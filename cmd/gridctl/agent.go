package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gridctl/gridctl/pkg/agent/dev/devserver"
	"github.com/gridctl/gridctl/pkg/agent/dev/scaffold"
	"github.com/gridctl/gridctl/pkg/agent/dev/watcher"

	"github.com/spf13/cobra"
)

// agentCmd is the parent for skill-authoring-time operations:
// editor canvas (`agent dev`), scaffolding (`agent init`),
// validation (`agent validate`), and the explicit Go skill compile
// step (`agent build`). Phase F ships dev + init; build / validate
// land in Phase G alongside the rest of the CLI surface.
var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Skill authoring tools — IDE, scaffolds, build, validate",
	Long: `The agent command groups skill-authoring operations.

  agent init        scaffold a runnable hello-world TS skill in cwd
  agent dev         start the IDE dev server (live canvas + trace overlay)

The IDE is read-only with respect to source — code is canon. The
canvas is a derived view of the AST on disk; click any node to jump
to that file:line in $EDITOR.`,
}

var (
	agentDevPort    int
	agentDevRoot    string
	agentDevFormat  string
	agentInitName   string
	agentInitForce  bool
	agentInitDir    string
	agentInitFormat string
)

var agentDevCmd = &cobra.Command{
	Use:   "dev",
	Short: "Start the agent IDE dev server",
	Long: `Boots an HTTP server that serves the agent IDE at localhost:<port>.

The server walks the project root, parses every typed-skill source it
finds, and streams file-watcher events to the IDE so saves reflect on
the canvas in <300ms (TS) or after explicit 'agent build' (Go).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		root := agentDevRoot
		if root == "" {
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("agent dev: getwd: %w", err)
			}
			root = cwd
		}
		root, err := filepath.Abs(root)
		if err != nil {
			return fmt.Errorf("agent dev: abs %s: %w", root, err)
		}
		w, err := watcher.New(root)
		if err != nil {
			return fmt.Errorf("agent dev: watcher: %w", err)
		}
		srv, err := devserver.NewServer(root, w)
		if err != nil {
			return fmt.Errorf("agent dev: server: %w", err)
		}

		ctx, cancel := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()

		// Run the watcher concurrently with the HTTP server.
		watchErrCh := make(chan error, 1)
		go func() { watchErrCh <- w.Run(ctx) }()

		addr := fmt.Sprintf("127.0.0.1:%d", agentDevPort)
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("agent dev: listen %s: %w", addr, err)
		}

		httpSrv := &http.Server{
			Handler:           srv.Handler(),
			ReadHeaderTimeout: 10 * time.Second,
		}

		serveErrCh := make(chan error, 1)
		go func() { serveErrCh <- httpSrv.Serve(listener) }()

		startup := struct {
			Status string `json:"status"`
			Addr   string `json:"addr"`
			Root   string `json:"root"`
		}{Status: "ready", Addr: "http://" + listener.Addr().String(), Root: root}

		if strings.EqualFold(agentDevFormat, "json") {
			_ = json.NewEncoder(cmd.OutOrStdout()).Encode(startup)
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "agent IDE ready at %s (root=%s)\n", startup.Addr, root)
		}

		select {
		case <-ctx.Done():
		case err := <-serveErrCh:
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				return err
			}
		case err := <-watchErrCh:
			if err != nil && !errors.Is(err, context.Canceled) {
				return err
			}
		}

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = httpSrv.Shutdown(shutdownCtx)
		return nil
	},
}

var agentInitCmd = &cobra.Command{
	Use:   "init [DIR]",
	Short: "Scaffold a runnable hello-world TS skill",
	Long: `Drops a starter SKILL.md, skill.ts, and agent.json into DIR.

DIR defaults to the current directory. Existing files are skipped
(re-running 'agent init' is idempotent unless --force is set).`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := agentInitDir
		if len(args) == 1 {
			dir = args[0]
		}
		if dir == "" {
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("agent init: getwd: %w", err)
			}
			dir = cwd
		}
		dir, err := filepath.Abs(dir)
		if err != nil {
			return fmt.Errorf("agent init: abs %s: %w", dir, err)
		}
		res, err := scaffold.Scaffold(dir, scaffold.Options{
			SkillName: agentInitName,
			Force:     agentInitForce,
		})
		if err != nil {
			return fmt.Errorf("agent init: %w", err)
		}
		if strings.EqualFold(agentInitFormat, "json") {
			_ = json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
				"dir":     dir,
				"created": res.Created,
				"skipped": res.Skipped,
			})
			return nil
		}
		for _, f := range res.Created {
			fmt.Fprintf(cmd.OutOrStdout(), "created %s\n", filepath.Join(dir, f))
		}
		for _, f := range res.Skipped {
			fmt.Fprintf(cmd.OutOrStdout(), "skipped %s (already exists)\n", filepath.Join(dir, f))
		}
		fmt.Fprintf(cmd.OutOrStdout(), "\nrun: gridctl agent dev --root %q\n", dir)
		return nil
	},
}

func init() {
	agentDevCmd.Flags().IntVar(&agentDevPort, "port", 8181, "Port to bind the dev server on")
	agentDevCmd.Flags().StringVar(&agentDevRoot, "root", "", "Project root to watch (defaults to cwd)")
	agentDevCmd.Flags().StringVar(&agentDevFormat, "format", "text", "Output format: text or json")

	agentInitCmd.Flags().StringVar(&agentInitName, "name", "hello-ts", "Skill name to scaffold")
	agentInitCmd.Flags().StringVar(&agentInitDir, "dir", "", "Directory to scaffold into (defaults to cwd or first arg)")
	agentInitCmd.Flags().BoolVar(&agentInitForce, "force", false, "Overwrite existing files")
	agentInitCmd.Flags().StringVar(&agentInitFormat, "format", "text", "Output format: text or json")

	agentCmd.AddCommand(agentDevCmd)
	agentCmd.AddCommand(agentInitCmd)
	rootCmd.AddCommand(agentCmd)
}
