package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/gridctl/gridctl/pkg/dockerclient"
	"github.com/gridctl/gridctl/pkg/runtime"
	_ "github.com/gridctl/gridctl/pkg/runtime/docker" // Register DockerRuntime factory
	"github.com/gridctl/gridctl/pkg/state"

	"github.com/spf13/cobra"
)

const logsFollowPollInterval = 250 * time.Millisecond

var (
	logsFollow bool
	logsTail   int
	logsServer string
	logsStack  string
)

var logsCmd = &cobra.Command{
	Use:   "logs [stack]",
	Short: "Show gateway daemon or MCP server logs",
	Long: `Prints logs for a stack.

By default this tails the gateway daemon log (~/.gridctl/logs/<stack>.log),
the same file 'gridctl apply' reports after a deploy. Use --server to stream
a containerized MCP server's logs from the container runtime instead.

The stack is auto-detected when exactly one is running.`,
	Example: `  gridctl logs                     Last 100 daemon log lines
  gridctl logs mystack -f          Follow the daemon log
  gridctl logs --server github     Container logs for the github server
  gridctl logs -n 20               Last 20 lines`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := logsStack
		if len(args) == 1 {
			name = args[0]
		}

		// SIGINT/SIGTERM cancel the context so --follow stops cleanly.
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		return runLogs(ctx, os.Stdout, name)
	},
}

func init() {
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "Follow log output")
	logsCmd.Flags().IntVarP(&logsTail, "tail", "n", 100, "Number of lines to show from the end of the log (0 or negative for all)")
	logsCmd.Flags().StringVar(&logsServer, "server", "", "Stream container logs for this MCP server instead of the daemon log")
	logsCmd.Flags().StringVarP(&logsStack, "stack", "s", "", "Stack name (auto-detected when only one stack is running)")
}

func runLogs(ctx context.Context, w io.Writer, name string) error {
	name, err := resolveLogsStack(name)
	if err != nil {
		return err
	}

	if logsServer != "" {
		return streamContainerLogs(ctx, w, name, logsServer, logsTail, logsFollow)
	}

	path := state.LogPath(name)
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("no daemon log for stack %q at %s (deploy it with 'gridctl apply <stack.yaml>')", name, path)
	}
	return tailDaemonLog(ctx, w, path, logsTail, logsFollow)
}

// resolveLogsStack picks the stack whose logs to show. An explicit name is
// accepted even for a stopped stack (its log file may still exist); with no
// name, exactly one running stack must be present.
func resolveLogsStack(name string) (string, error) {
	states, err := state.List()
	if err != nil {
		return "", fmt.Errorf("logs: could not read state: %w", err)
	}

	if name != "" {
		for _, s := range states {
			if s.StackName == name {
				return name, nil
			}
		}
		// No state file: still allow a leftover log file to be read.
		if _, statErr := os.Stat(state.LogPath(name)); statErr == nil {
			return name, nil
		}
		return "", fmt.Errorf("stack %q not found (try 'gridctl status')", name)
	}

	var running []string
	for _, s := range states {
		if state.IsRunning(&s) {
			running = append(running, s.StackName)
		}
	}
	switch len(running) {
	case 0:
		return "", errors.New("no running stacks; start one with 'gridctl apply <stack.yaml>' or 'gridctl serve'")
	case 1:
		return running[0], nil
	default:
		return "", fmt.Errorf("multiple stacks running (%s); pick one with 'gridctl logs <stack>'", strings.Join(running, ", "))
	}
}

// tailDaemonLog prints the last n lines of the log at path and, when follow
// is set, streams appended lines until ctx is cancelled.
func tailDaemonLog(ctx context.Context, w io.Writer, path string, n int, follow bool) error {
	f, err := os.Open(path) // #nosec G304 -- path is derived from the managed state directory
	if err != nil {
		return fmt.Errorf("opening log: %w", err)
	}
	defer f.Close()

	offset, err := printTail(w, f, n)
	if err != nil {
		return err
	}
	if !follow {
		return nil
	}

	return followLog(ctx, w, f, offset)
}

// printTail writes the last n lines of r to w (every line when n <= 0,
// matching the docker logs convention for --tail) and returns the byte
// offset reached (the end of the file at read time). Lines are read with
// a bufio.Reader so arbitrarily long lines never abort the command.
func printTail(w io.Writer, r io.ReadSeeker, n int) (int64, error) {
	capacity := n
	if capacity < 0 {
		capacity = 0
	}
	lines := make([]string, 0, capacity)
	reader := bufio.NewReader(r)
	for {
		line, err := reader.ReadString('\n')
		if line != "" {
			if n > 0 && len(lines) == n {
				lines = lines[1:]
			}
			lines = append(lines, strings.TrimSuffix(line, "\n"))
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, fmt.Errorf("reading log: %w", err)
		}
	}
	for _, line := range lines {
		fmt.Fprintln(w, line)
	}
	return r.Seek(0, io.SeekEnd)
}

// followLog polls the open log file for appended data until ctx is done.
// Truncation (e.g. log rotation) resets the read offset to the new end.
func followLog(ctx context.Context, w io.Writer, f *os.File, offset int64) error {
	ticker := time.NewTicker(logsFollowPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			info, err := f.Stat()
			if err != nil {
				return fmt.Errorf("watching log: %w", err)
			}
			size := info.Size()
			if size < offset {
				offset = size // truncated or rotated in place
				continue
			}
			if size == offset {
				continue
			}
			if _, err := f.Seek(offset, io.SeekStart); err != nil {
				return fmt.Errorf("watching log: %w", err)
			}
			// Advance by what was actually copied: the file may have grown
			// past the Stat() size mid-copy, and re-reading those bytes on
			// the next tick would duplicate output.
			copied, err := io.Copy(w, f)
			if err != nil {
				return fmt.Errorf("watching log: %w", err)
			}
			offset += copied
		}
	}
}

// dockerClientProvider is the optional capability a runtime exposes when it
// can stream container logs through a Docker-compatible API.
type dockerClientProvider interface {
	Client() dockerclient.DockerClient
}

// streamContainerLogs streams a containerized MCP server's logs via the
// container runtime. Local-process and external servers have no container
// to read from and get a clear error instead.
func streamContainerLogs(ctx context.Context, w io.Writer, stack, server string, tail int, follow bool) error {
	rt, err := runtime.New()
	if err != nil {
		return fmt.Errorf("container runtime unavailable: %w", err)
	}
	defer rt.Close()

	statuses, err := rt.Status(ctx, stack)
	if err != nil {
		return fmt.Errorf("listing containers for stack %q: %w", stack, err)
	}

	var id string
	var names []string
	for _, s := range statuses {
		workload := s.Labels[runtime.LabelMCPServer]
		if workload == "" {
			workload = s.Name
		}
		names = append(names, workload)
		if workload == server {
			id = string(s.ID)
		}
	}
	if id == "" {
		msg := fmt.Sprintf("server %q has no container in stack %q (local-process and external servers have no container logs)", server, stack)
		if len(names) > 0 {
			msg += fmt.Sprintf("; containers: %s", strings.Join(names, ", "))
		}
		return errors.New(msg)
	}

	provider, ok := rt.Runtime().(dockerClientProvider)
	if !ok {
		return errors.New("container logs are not supported by the active runtime")
	}

	tailArg := "all"
	if tail > 0 {
		tailArg = strconv.Itoa(tail)
	}
	rc, err := provider.Client().ContainerLogs(ctx, id, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       tailArg,
		Follow:     follow,
	})
	if err != nil {
		return fmt.Errorf("streaming container logs: %w", err)
	}
	defer rc.Close()

	// Container streams are multiplexed; demux both to w.
	if _, err := stdcopy.StdCopy(w, w, rc); err != nil && ctx.Err() == nil {
		return fmt.Errorf("reading container logs: %w", err)
	}
	return nil
}
