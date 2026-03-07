// Package controller implements the stack lifecycle management for gridctl.
// It extracts orchestration logic from cmd/gridctl/deploy.go into testable,
// interface-backed components.
package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/logging"
	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/runtime"
	"github.com/gridctl/gridctl/pkg/state"
	"github.com/gridctl/gridctl/pkg/vault"
)

// vaultSetAdapter wraps vault.Store to satisfy config.VaultSetLookup.
type vaultSetAdapter struct {
	store *vault.Store
}

func newVaultSetAdapter(store *vault.Store) *vaultSetAdapter {
	return &vaultSetAdapter{store: store}
}

func (a *vaultSetAdapter) Get(key string) (string, bool) {
	return a.store.Get(key)
}

func (a *vaultSetAdapter) GetSetSecrets(setName string) []config.VaultSecret {
	secrets := a.store.GetSetSecrets(setName)
	result := make([]config.VaultSecret, len(secrets))
	for i, s := range secrets {
		result[i] = config.VaultSecret{Key: s.Key, Value: s.Value}
	}
	return result
}

// Config holds all deploy configuration, replacing package-level variables.
type Config struct {
	StackPath   string
	Port        int
	BasePort    int
	Verbose     bool
	Quiet       bool
	NoCache     bool
	NoExpand    bool
	Foreground  bool
	Watch       bool
	DaemonChild bool
	CodeMode    bool   // Enable code mode via CLI flag
	Runtime     string // Explicit container runtime ("docker" or "podman")
}

// StackController orchestrates the full deploy lifecycle.
type StackController struct {
	config     Config
	version    string
	webFS      WebFSFunc
	vaultStore *vault.Store
}

// New creates a StackController.
func New(cfg Config) *StackController {
	return &StackController{config: cfg}
}

// SetVersion sets the version string for the gateway.
func (sc *StackController) SetVersion(v string) {
	sc.version = v
}

// SetWebFS sets the function for getting embedded web files.
func (sc *StackController) SetWebFS(fn WebFSFunc) {
	sc.webFS = fn
}

// Deploy orchestrates the full stack lifecycle.
func (sc *StackController) Deploy(ctx context.Context) error {
	cfg := sc.config

	// Resolve absolute path for daemon child
	absPath, err := filepath.Abs(cfg.StackPath)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}
	cfg.StackPath = absPath
	sc.config = cfg

	// Load vault
	vaultStore := vault.NewStore(state.VaultDir())
	if err := vaultStore.Load(); err != nil {
		return fmt.Errorf("loading vault: %w", err)
	}

	sc.vaultStore = vaultStore

	// Load stack with vault resolution and set injection
	stack, err := config.LoadStack(cfg.StackPath, config.WithVault(vaultStore), config.WithVaultSets(newVaultSetAdapter(vaultStore)))
	if err != nil {
		return fmt.Errorf("failed to load stack: %w", err)
	}

	// Check state lock and existing daemon
	if err := sc.checkState(stack); err != nil {
		return err
	}

	// Daemon child path: run gateway directly
	if cfg.DaemonChild {
		return sc.runDaemonChild(ctx, stack)
	}

	// Create output printer
	printer := sc.createPrinter(stack)

	// Detect runtime and start containers
	rt, err := sc.createRuntime(ctx, stack, printer)
	if err != nil {
		return err
	}
	defer rt.Close()

	// Set up logging for orchestrator
	logBuffer, bufferHandler := sc.setupOrchestratorLogging(rt)

	// Run workloads
	result, err := rt.Up(ctx, stack, runtime.UpOptions{
		NoCache:     cfg.NoCache,
		BasePort:    cfg.BasePort,
		GatewayPort: cfg.Port,
	})
	if err != nil {
		return fmt.Errorf("failed to start stack: %w", err)
	}

	// Foreground mode: run gateway directly
	if cfg.Foreground {
		builder := sc.newGatewayBuilder(stack, rt, result)
		builder.SetExistingLogInfra(logBuffer, bufferHandler)
		return builder.BuildAndRun(ctx, !cfg.Quiet)
	}

	// Daemon mode: fork child process
	return sc.runDaemonMode(ctx, stack, result, printer)
}

// createRuntime detects the container runtime and creates an orchestrator.
// If the stack doesn't need a container runtime, falls back to the default factory.
func (sc *StackController) createRuntime(ctx context.Context, stack *config.Stack, printer *output.Printer) (*runtime.Orchestrator, error) {
	if !stack.NeedsContainerRuntime() {
		// No container workloads — use default runtime (won't fail if Docker unavailable)
		rt, err := runtime.New()
		if err != nil {
			return nil, fmt.Errorf("failed to create runtime: %w", err)
		}
		return rt, nil
	}

	// Detect runtime
	info, err := runtime.DetectRuntime(ctx, runtime.DetectOptions{
		Explicit: sc.config.Runtime,
	})
	if err != nil {
		return nil, err
	}

	// Print runtime info
	if printer != nil {
		printer.Info("Runtime", "type", info.DisplayName())
	} else if !sc.config.Quiet {
		fmt.Printf("Runtime: %s\n", info.DisplayName())
	}

	// Warn about rootless Podman
	if info.IsRootless() {
		msg := "rootless Podman detected — networking may not work as expected; use rootful mode for full compatibility"
		if printer != nil {
			printer.Warn(msg)
		} else {
			fmt.Printf("Warning: %s\n", msg)
		}
	}

	// Log SELinux volume label behavior
	if info.SELinux && info.Type == runtime.RuntimePodman {
		msg := "SELinux detected — :Z labels will be auto-applied to volume mounts"
		if printer != nil {
			printer.Info(msg)
		} else if !sc.config.Quiet {
			fmt.Printf("%s\n", msg)
		}
	}

	rt, err := runtime.NewWithInfo(info)
	if err != nil {
		return nil, fmt.Errorf("failed to create runtime: %w", err)
	}

	return rt, nil
}

// checkState acquires a lock, cleans stale state, and checks if already running.
func (sc *StackController) checkState(stack *config.Stack) error {
	var existingState *state.DaemonState
	err := state.WithLock(stack.Name, 5*time.Second, func() error {
		cleaned, cleanErr := state.CheckAndClean(stack.Name)
		if cleanErr != nil {
			return fmt.Errorf("checking state: %w", cleanErr)
		}
		if cleaned {
			fmt.Printf("Cleaned up stale state for '%s'\n", stack.Name)
		}
		existingState, _ = state.Load(stack.Name)
		return nil
	})
	if err != nil {
		return err
	}
	if existingState != nil && state.IsRunning(existingState) {
		return fmt.Errorf("stack '%s' is already running on port %d (PID: %d)\nUse 'gridctl destroy %s' to stop it first",
			stack.Name, existingState.Port, existingState.PID, sc.config.StackPath)
	}
	return nil
}

// runDaemonChild runs the gateway as a daemon child process.
func (sc *StackController) runDaemonChild(ctx context.Context, stack *config.Stack) error {
	rt, err := sc.createRuntime(ctx, stack, nil)
	if err != nil {
		return err
	}
	defer rt.Close()

	result, err := getRunningContainers(ctx, rt, stack)
	if err != nil {
		return fmt.Errorf("failed to get container info: %w", err)
	}

	st := &state.DaemonState{
		StackName: stack.Name,
		StackFile: sc.config.StackPath,
		PID:       os.Getpid(),
		Port:      sc.config.Port,
		StartedAt: time.Now(),
	}
	if err := state.Save(st); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	builder := sc.newGatewayBuilder(stack, rt, result)
	return builder.BuildAndRun(ctx, false)
}

// createPrinter creates the output printer unless quiet mode is enabled.
func (sc *StackController) createPrinter(stack *config.Stack) *output.Printer {
	if sc.config.Quiet {
		return nil
	}

	printer := output.New()
	printer.Banner(sc.version)
	printer.Info("Parsing & checking stack", "file", sc.config.StackPath)

	if sc.config.Verbose {
		fmt.Println("\nFull stack (JSON):")
		data, _ := json.MarshalIndent(stack, "", "  ")
		fmt.Println(logging.RedactString(string(data)))
	}

	return printer
}

// setupOrchestratorLogging configures logging for the runtime orchestrator.
func (sc *StackController) setupOrchestratorLogging(rt *runtime.Orchestrator) (*logging.LogBuffer, slog.Handler) {
	cfg := sc.config

	// Helper to register vault values on a redacting handler
	registerVault := func(h *logging.RedactingHandler) {
		if sc.vaultStore != nil {
			h.RegisterRedactValues(sc.vaultStore.Values())
		}
	}

	if cfg.Foreground && !cfg.Quiet {
		logBuffer := logging.NewLogBuffer(1000)
		logLevel := slog.LevelInfo
		if cfg.Verbose {
			logLevel = slog.LevelDebug
		}
		innerHandler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})
		bufferHandler := logging.NewBufferHandler(logBuffer, innerHandler)
		redactHandler := logging.NewRedactingHandler(bufferHandler)
		registerVault(redactHandler)
		rt.SetLogger(slog.New(redactHandler).With("component", "orchestrator"))
		return logBuffer, redactHandler
	}

	if !cfg.Quiet {
		logLevel := slog.LevelInfo
		if cfg.Verbose {
			logLevel = slog.LevelDebug
		}
		textHandler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})
		redactHandler := logging.NewRedactingHandler(textHandler)
		registerVault(redactHandler)
		rt.SetLogger(slog.New(redactHandler))
	}

	return nil, nil
}

// runDaemonMode forks a child process and waits for readiness.
func (sc *StackController) runDaemonMode(ctx context.Context, stack *config.Stack, result *runtime.UpResult, printer *output.Printer) error {
	daemon := NewDaemonManager(sc.config)
	pid, err := daemon.Fork(stack)
	if err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	if err := daemon.WaitForReady(sc.config.Port, 60*time.Second); err != nil {
		return fmt.Errorf("daemon failed to become ready: %w\nCheck logs at %s", err, state.LogPath(stack.Name))
	}

	st, err := state.Load(stack.Name)
	if err != nil {
		return fmt.Errorf("daemon may have failed to start - check logs at %s", state.LogPath(stack.Name))
	}

	// Print summary
	if printer != nil {
		summaries := BuildWorkloadSummaries(stack, result)
		printer.Summary(summaries)
		printer.Info("Gateway running", "url", fmt.Sprintf("http://localhost:%d", st.Port))
		printer.Print("\nUse 'gridctl destroy %s' to stop\n", sc.config.StackPath)
	} else {
		fmt.Printf("Stack '%s' started successfully\n", stack.Name)
		fmt.Printf("  Gateway: http://localhost:%d\n", st.Port)
		fmt.Printf("  PID: %d\n", pid)
		fmt.Printf("  Logs: %s\n", state.LogPath(stack.Name))
		fmt.Printf("\nUse 'gridctl destroy %s' to stop\n", sc.config.StackPath)
	}

	return nil
}

// newGatewayBuilder creates a configured GatewayBuilder.
func (sc *StackController) newGatewayBuilder(stack *config.Stack, rt *runtime.Orchestrator, result *runtime.UpResult) *GatewayBuilder {
	builder := NewGatewayBuilder(sc.config, stack, sc.config.StackPath, rt, result)
	builder.SetVersion(sc.version)
	builder.SetWebFS(sc.webFS)
	builder.SetVaultStore(sc.vaultStore)
	return builder
}

// BuildWorkloadSummaries creates summary data for the status table.
func BuildWorkloadSummaries(stack *config.Stack, result *runtime.UpResult) []output.WorkloadSummary {
	var summaries []output.WorkloadSummary

	serverTransports := make(map[string]string)
	for _, s := range stack.MCPServers {
		transport := s.Transport
		if transport == "" {
			transport = "http"
		}
		if s.IsExternal() {
			transport = "external"
		} else if s.IsLocalProcess() {
			transport = "local"
		} else if s.IsSSH() {
			transport = "ssh"
		} else if s.IsOpenAPI() {
			transport = "openapi"
		}
		serverTransports[s.Name] = transport
	}

	for _, server := range result.MCPServers {
		transport := serverTransports[server.Name]
		summaries = append(summaries, output.WorkloadSummary{
			Name:      server.Name,
			Type:      "mcp-server",
			Transport: transport,
			State:     "running",
		})
	}

	for _, agent := range result.Agents {
		summaries = append(summaries, output.WorkloadSummary{
			Name:      agent.Name,
			Type:      "agent",
			Transport: "container",
			State:     "running",
		})
	}

	for _, res := range stack.Resources {
		summaries = append(summaries, output.WorkloadSummary{
			Name:      res.Name,
			Type:      "resource",
			Transport: "container",
			State:     "running",
		})
	}

	return summaries
}

// getRunningContainers retrieves info about already-running containers and external servers.
func getRunningContainers(ctx context.Context, rt *runtime.Orchestrator, stack *config.Stack) (*runtime.UpResult, error) {
	result := &runtime.UpResult{}
	foundServers := make(map[string]bool)

	// Only query Docker for container statuses when the stack has container workloads
	var statuses []runtime.WorkloadStatus
	if stack.NeedsContainerRuntime() {
		var err error
		statuses, err = rt.Status(ctx, stack.Name)
		if err != nil {
			return nil, err
		}
	}

	for _, status := range statuses {
		var workloadName string
		if status.Labels != nil {
			if name, ok := status.Labels[runtime.LabelMCPServer]; ok {
				workloadName = name
			} else if name, ok := status.Labels[runtime.LabelResource]; ok {
				workloadName = name
			} else if name, ok := status.Labels[runtime.LabelAgent]; ok {
				workloadName = name
			}
		}

		if status.Type == runtime.WorkloadTypeMCPServer {
			var containerPort int
			for _, s := range stack.MCPServers {
				if s.Name == workloadName {
					containerPort = s.Port
					break
				}
			}

			hostPort, _ := runtime.GetContainerHostPort(ctx, rt.DockerClient(), string(status.ID), containerPort)

			result.MCPServers = append(result.MCPServers, runtime.MCPServerResult{
				Name:       workloadName,
				WorkloadID: status.ID,
				HostPort:   hostPort,
			})
			foundServers[workloadName] = true
		} else if status.Type == runtime.WorkloadTypeAgent {
			var uses []config.ToolSelector
			for _, a := range stack.Agents {
				if a.Name == workloadName {
					uses = a.Uses
					break
				}
			}

			result.Agents = append(result.Agents, runtime.AgentResult{
				Name:       workloadName,
				WorkloadID: status.ID,
				Uses:       uses,
			})
		}
	}

	// Add non-container MCP servers from config
	for _, server := range stack.MCPServers {
		if foundServers[server.Name] {
			continue
		}
		switch {
		case server.IsExternal():
			result.MCPServers = append(result.MCPServers, runtime.MCPServerResult{
				Name: server.Name, External: true, URL: server.URL,
			})
		case server.IsLocalProcess():
			result.MCPServers = append(result.MCPServers, runtime.MCPServerResult{
				Name: server.Name, LocalProcess: true, Command: server.Command,
			})
		case server.IsSSH():
			result.MCPServers = append(result.MCPServers, runtime.MCPServerResult{
				Name: server.Name, SSH: true, Command: server.Command,
				SSHHost: server.SSH.Host, SSHUser: server.SSH.User,
				SSHPort: server.SSH.Port, SSHIdentityFile: server.SSH.IdentityFile,
			})
		case server.IsOpenAPI():
			result.MCPServers = append(result.MCPServers, runtime.MCPServerResult{
				Name: server.Name, OpenAPI: true, OpenAPIConfig: server.OpenAPI,
			})
		}
	}

	return result, nil
}
