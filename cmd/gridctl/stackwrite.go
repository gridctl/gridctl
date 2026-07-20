package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gridctl/gridctl/internal/stackedit"
	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/provisioner"
	"github.com/gridctl/gridctl/pkg/state"

	"gopkg.in/yaml.v3"
)

// Shared plumbing for CLI commands that append servers to stack.yaml
// (import, add).

// resolveStackFileTarget locates the stack file to append to: the explicit
// flag value, then the single running stack's recorded file, then
// ./stack.yaml.
func resolveStackFileTarget(fileFlag string) (string, []byte, error) {
	tryRead := func(path string) (string, []byte, error) {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", nil, fmt.Errorf("stack file %s: %w", path, err)
		}
		return path, data, nil
	}
	if fileFlag != "" {
		return tryRead(fileFlag)
	}
	if states, err := state.List(); err == nil {
		var running []state.DaemonState
		for _, s := range states {
			if state.IsRunning(&s) && s.StackFile != "" {
				running = append(running, s)
			}
		}
		if len(running) == 1 {
			return tryRead(running[0].StackFile)
		}
		if len(running) > 1 {
			return "", nil, fmt.Errorf("multiple running stacks; pass --file to pick the stack file")
		}
	}
	if _, err := os.Stat("stack.yaml"); err == nil {
		return tryRead("stack.yaml")
	}
	return "", nil, fmt.Errorf("no stack file found: pass --file, run inside a directory with stack.yaml, or deploy a stack first")
}

// warnRunningStack confirms before writing to a stack a running daemon may
// hot-apply. Skipped under assumeYes; refuses in non-interactive runs
// without it.
func warnRunningStack(printer *output.Printer, stackPath string, assumeYes bool) error {
	if assumeYes {
		return nil
	}
	abs, err := filepath.Abs(stackPath)
	if err != nil {
		return nil
	}
	states, err := state.List()
	if err != nil {
		return nil
	}
	for _, s := range states {
		if !state.IsRunning(&s) || s.StackFile != abs {
			continue
		}
		printer.Warn(fmt.Sprintf("Stack %q is running; a watched daemon applies new servers as soon as the file is saved", s.StackName))
		if !output.IsTerminal(os.Stdin) {
			return fmt.Errorf("refusing to modify a running stack non-interactively; pass --yes to proceed")
		}
		return nil
	}
	return nil
}

// writeServersToStack performs the locked read-verify-write cycle: backup,
// optional overwrite removals, append, validate the post-append stack, and
// atomically replace the file. Nothing is written when validation fails.
func writeServersToStack(stackPath string, servers []config.MCPServer, overwrites []string) (string, error) {
	mu := stackedit.PathLock(stackPath)
	mu.Lock()
	defer mu.Unlock()

	original, err := os.ReadFile(stackPath)
	if err != nil {
		return "", fmt.Errorf("read stack file: %w", err)
	}
	originalHash := sha256.Sum256(original)

	updated := original
	for _, name := range overwrites {
		if updated, err = stackedit.RemoveResourceByName(updated, "mcp-servers", name); err != nil {
			return "", err
		}
	}

	snippets := make([][]byte, 0, len(servers))
	for _, server := range servers {
		snippet, err := yaml.Marshal(server)
		if err != nil {
			return "", fmt.Errorf("marshal server %s: %w", server.Name, err)
		}
		snippets = append(snippets, snippet)
	}
	if updated, err = stackedit.AppendResources(updated, "mcp-servers", snippets...); err != nil {
		return "", err
	}

	// Article IX gate: the post-append stack must validate before a byte
	// lands on disk.
	var stack config.Stack
	if err := yaml.Unmarshal(updated, &stack); err != nil {
		return "", fmt.Errorf("post-append stack does not parse: %w", err)
	}
	config.ExpandStackVarsWithEnv(&stack)
	stack.SetDefaults()
	if result := config.ValidateWithIssues(&stack); !result.Valid {
		var lines []string
		for _, issue := range result.Issues {
			lines = append(lines, fmt.Sprintf("%s: %s", issue.Field, issue.Message))
		}
		return "", fmt.Errorf("post-append stack fails validation; nothing written:\n  %s", strings.Join(lines, "\n  "))
	}

	current, err := os.ReadFile(stackPath)
	if err != nil {
		return "", fmt.Errorf("re-read stack file: %w", err)
	}
	if sha256.Sum256(current) != originalHash {
		return "", fmt.Errorf("stack file changed on disk during the write; re-run to work from the current contents")
	}

	backupPath, err := provisioner.CreateBackup(stackPath)
	if err != nil {
		return "", fmt.Errorf("backing up stack file: %w", err)
	}
	if err := stackedit.AtomicWrite(stackPath, updated); err != nil {
		return "", err
	}
	return backupPath, nil
}

// stackServerNames extracts existing server names without full config
// loading, so collision checks work even when the stack references vars
// that are unset in this shell.
func stackServerNames(source []byte) (map[string]bool, error) {
	var doc struct {
		Servers []struct {
			Name string `yaml:"name"`
		} `yaml:"mcp-servers"`
	}
	if err := yaml.Unmarshal(source, &doc); err != nil {
		return nil, err
	}
	names := make(map[string]bool, len(doc.Servers))
	for _, s := range doc.Servers {
		if s.Name != "" {
			names[s.Name] = true
		}
	}
	return names, nil
}
