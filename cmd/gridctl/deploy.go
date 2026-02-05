package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/gridctl/gridctl/internal/api"
	"github.com/gridctl/gridctl/pkg/a2a"
	"github.com/gridctl/gridctl/pkg/adapter"
	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/logging"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/reload"
	"github.com/gridctl/gridctl/pkg/runtime"
	_ "github.com/gridctl/gridctl/pkg/runtime/docker" // Register DockerRuntime factory
	"github.com/gridctl/gridctl/pkg/state"

	"github.com/spf13/cobra"
)

var (
	deployVerbose     bool
	deployQuiet       bool
	deployNoCache     bool
	deployPort        int
	deployBasePort    int
	deployForeground  bool
	deployDaemonChild bool
	deployNoExpand    bool
	deployWatch       bool
)

var deployCmd = &cobra.Command{
	Use:   "deploy <stack.yaml>",
	Short: "Start MCP servers defined in a stack file",
	Long: `Reads a stack YAML file and starts all defined MCP servers and resources.

Creates a Docker network, pulls/builds images as needed, and starts containers.
The MCP gateway runs as a background daemon by default.

Use --foreground (-f) to run in foreground with verbose output.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDeploy(args[0])
	},
}

func init() {
	deployCmd.Flags().BoolVarP(&deployVerbose, "verbose", "v", false, "Print full stack as JSON")
	deployCmd.Flags().BoolVarP(&deployQuiet, "quiet", "q", false, "Suppress progress output (show only final result)")
	deployCmd.Flags().BoolVar(&deployNoCache, "no-cache", false, "Force rebuild of source-based images")
	deployCmd.Flags().IntVarP(&deployPort, "port", "p", 8180, "Port for MCP gateway")
	deployCmd.Flags().IntVar(&deployBasePort, "base-port", 9000, "Base port for MCP server host port allocation")
	deployCmd.Flags().BoolVarP(&deployForeground, "foreground", "f", false, "Run in foreground (don't daemonize)")
	deployCmd.Flags().BoolVar(&deployDaemonChild, "daemon-child", false, "Internal flag for daemon process")
	_ = deployCmd.Flags().MarkHidden("daemon-child")
	deployCmd.Flags().BoolVar(&deployNoExpand, "no-expand", false, "Disable environment variable expansion in OpenAPI spec files")
	deployCmd.Flags().BoolVarP(&deployWatch, "watch", "w", false, "Watch stack file for changes and hot reload")
}

func runDeploy(stackPath string) error {
	// Convert to absolute path for daemon child
	absPath, err := filepath.Abs(stackPath)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}
	stackPath = absPath

	// Load stack
	stack, err := config.LoadStack(stackPath)
	if err != nil {
		return fmt.Errorf("failed to load stack: %w", err)
	}

	// Clean up stale state (process died without cleanup) and check if already running
	var existingState *state.DaemonState
	err = state.WithLock(stack.Name, 5*time.Second, func() error {
		// Auto-clean stale state files
		cleaned, cleanErr := state.CheckAndClean(stack.Name)
		if cleanErr != nil {
			return fmt.Errorf("checking state: %w", cleanErr)
		}
		if cleaned {
			fmt.Printf("Cleaned up stale state for '%s'\n", stack.Name)
		}

		// Check if already running (after cleanup)
		existingState, _ = state.Load(stack.Name)
		return nil
	})
	if err != nil {
		return err
	}
	if existingState != nil && state.IsRunning(existingState) {
		return fmt.Errorf("stack '%s' is already running on port %d (PID: %d)\nUse 'gridctl destroy %s' to stop it first",
			stack.Name, existingState.Port, existingState.PID, stackPath)
	}

	// If we're the daemon child, run the gateway
	if deployDaemonChild {
		return runDeployDaemonChild(stackPath, stack)
	}

	// Create output printer (verbose by default unless --quiet)
	var printer *output.Printer
	if !deployQuiet {
		printer = output.New()
		printer.Banner(version)
		printer.Info("Parsing & checking stack", "file", stackPath)
	}

	if deployVerbose {
		fmt.Println("\nFull stack (JSON):")
		data, _ := json.MarshalIndent(stack, "", "  ")
		fmt.Println(string(data))
	}

	// Start containers
	rt, err := runtime.New()
	if err != nil {
		return fmt.Errorf("failed to create runtime: %w", err)
	}
	defer rt.Close()

	// In foreground mode, create the log buffer early so orchestrator events
	// are captured and visible in the UI log viewer
	var logBuffer *logging.LogBuffer
	var bufferHandler *logging.BufferHandler
	if deployForeground && !deployQuiet {
		logBuffer = logging.NewLogBuffer(1000)
		logLevel := slog.LevelInfo
		if deployVerbose {
			logLevel = slog.LevelDebug
		}
		innerHandler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})
		bufferHandler = logging.NewBufferHandler(logBuffer, innerHandler)
		rt.SetLogger(slog.New(bufferHandler).With("component", "orchestrator"))
	} else if !deployQuiet {
		// Non-foreground mode: stderr-only logger for orchestrator
		logLevel := slog.LevelInfo
		if deployVerbose {
			logLevel = slog.LevelDebug
		}
		logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))
		rt.SetLogger(logger)
	}

	ctx := context.Background()
	opts := runtime.UpOptions{
		NoCache:     deployNoCache,
		BasePort:    deployBasePort,
		GatewayPort: deployPort,
	}
	result, err := rt.Up(ctx, stack, opts)
	if err != nil {
		return fmt.Errorf("failed to start stack: %w", err)
	}

	// Convert to legacy result format for runGateway
	legacyResult := result.ToLegacyResult()

	// If foreground mode, run gateway directly (pass pre-created buffer if available)
	if deployForeground {
		return runGateway(ctx, rt, stack, stackPath, legacyResult, deployPort, !deployQuiet, printer, logBuffer, bufferHandler)
	}

	// Daemon mode: fork child process
	pid, err := forkDeployDaemon(stackPath, deployPort, deployBasePort, deployNoExpand, deployWatch)
	if err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	// Wait for daemon to be fully ready (MCP servers initialized)
	// Use longer timeout for servers that need OAuth flows (e.g., mcp-remote)
	if err := waitForReady(deployPort, 60*time.Second); err != nil {
		return fmt.Errorf("daemon failed to become ready: %w\nCheck logs at %s", err, state.LogPath(stack.Name))
	}

	// Verify daemon started
	st, err := state.Load(stack.Name)
	if err != nil {
		return fmt.Errorf("daemon may have failed to start - check logs at %s", state.LogPath(stack.Name))
	}

	// Print summary table for daemon mode
	if printer != nil {
		summaries := buildWorkloadSummaries(stack, result)
		printer.Summary(summaries)
		printer.Info("Gateway running", "url", fmt.Sprintf("http://localhost:%d", st.Port))
		printer.Print("\nUse 'gridctl destroy %s' to stop\n", stackPath)
	} else {
		fmt.Printf("Stack '%s' started successfully\n", stack.Name)
		fmt.Printf("  Gateway: http://localhost:%d\n", st.Port)
		fmt.Printf("  PID: %d\n", pid)
		fmt.Printf("  Logs: %s\n", state.LogPath(stack.Name))
		fmt.Printf("\nUse 'gridctl destroy %s' to stop\n", stackPath)
	}

	return nil
}

// buildWorkloadSummaries creates summary data for the status table.
func buildWorkloadSummaries(stack *config.Stack, result *runtime.UpResult) []output.WorkloadSummary {
	var summaries []output.WorkloadSummary

	// Build transport lookup from stack config
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

	// MCP Servers
	for _, server := range result.MCPServers {
		transport := serverTransports[server.Name]
		summaries = append(summaries, output.WorkloadSummary{
			Name:      server.Name,
			Type:      "mcp-server",
			Transport: transport,
			State:     "running",
		})
	}

	// Agents
	for _, agent := range result.Agents {
		summaries = append(summaries, output.WorkloadSummary{
			Name:      agent.Name,
			Type:      "agent",
			Transport: "container",
			State:     "running",
		})
	}

	// Resources
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

// runDeployDaemonChild runs the gateway as a daemon child process
func runDeployDaemonChild(stackPath string, stack *config.Stack) error {
	// Create runtime
	rt, err := runtime.New()
	if err != nil {
		return fmt.Errorf("failed to create runtime: %w", err)
	}
	defer rt.Close()

	// Containers should already be running, but we need the result
	// Re-query container info
	ctx := context.Background()
	result, err := getRunningContainers(ctx, rt, stack)
	if err != nil {
		return fmt.Errorf("failed to get container info: %w", err)
	}

	// Write state file before starting server
	st := &state.DaemonState{
		StackName: stack.Name,
		StackFile: stackPath,
		PID:       os.Getpid(),
		Port:      deployPort,
		StartedAt: time.Now(),
	}
	if err := state.Save(st); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	// Run gateway (blocks until shutdown)
	return runGateway(ctx, rt, stack, stackPath, result, deployPort, false, nil, nil, nil)
}

// getRunningContainers retrieves info about already-running containers and external servers
func getRunningContainers(ctx context.Context, rt *runtime.Runtime, stack *config.Stack) (*runtime.LegacyUpResult, error) {
	// Get container statuses using new WorkloadStatus API
	statuses, err := rt.Status(ctx, stack.Name)
	if err != nil {
		return nil, err
	}

	// Build result from statuses
	result := &runtime.LegacyUpResult{}

	// Track which container-based MCP servers we found
	foundServers := make(map[string]bool)

	for _, status := range statuses {
		// Extract workload name from labels
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
			// Find the MCP server config to get port info
			var containerPort int
			for _, s := range stack.MCPServers {
				if s.Name == workloadName {
					containerPort = s.Port
					break
				}
			}

			// Get host port from container
			hostPort, _ := runtime.GetContainerHostPort(ctx, rt.DockerClient(), string(status.ID), containerPort)

			result.MCPServers = append(result.MCPServers, runtime.MCPServerInfo{
				Name:          workloadName,
				ContainerID:   string(status.ID),
				ContainerName: status.Name,
				ContainerPort: containerPort,
				HostPort:      hostPort,
			})
			foundServers[workloadName] = true
		} else if status.Type == runtime.WorkloadTypeAgent {
			// Find the agent config to get uses info
			var uses []config.ToolSelector
			for _, a := range stack.Agents {
				if a.Name == workloadName {
					uses = a.Uses
					break
				}
			}

			result.Agents = append(result.Agents, runtime.AgentInfo{
				Name:          workloadName,
				ContainerID:   string(status.ID),
				ContainerName: status.Name,
				Uses:          uses,
			})
		}
	}

	// Add external MCP servers from config (they don't have containers)
	for _, server := range stack.MCPServers {
		if server.IsExternal() && !foundServers[server.Name] {
			result.MCPServers = append(result.MCPServers, runtime.MCPServerInfo{
				Name:     server.Name,
				External: true,
				URL:      server.URL,
			})
		}
	}

	// Add local process MCP servers from config (they don't have containers)
	for _, server := range stack.MCPServers {
		if server.IsLocalProcess() && !foundServers[server.Name] {
			result.MCPServers = append(result.MCPServers, runtime.MCPServerInfo{
				Name:         server.Name,
				LocalProcess: true,
				Command:      server.Command,
			})
		}
	}

	// Add SSH MCP servers from config (they don't have containers)
	for _, server := range stack.MCPServers {
		if server.IsSSH() && !foundServers[server.Name] {
			result.MCPServers = append(result.MCPServers, runtime.MCPServerInfo{
				Name:            server.Name,
				SSH:             true,
				Command:         server.Command,
				SSHHost:         server.SSH.Host,
				SSHUser:         server.SSH.User,
				SSHPort:         server.SSH.Port,
				SSHIdentityFile: server.SSH.IdentityFile,
			})
		}
	}

	// Add OpenAPI MCP servers from config (they don't have containers)
	for _, server := range stack.MCPServers {
		if server.IsOpenAPI() && !foundServers[server.Name] {
			result.MCPServers = append(result.MCPServers, runtime.MCPServerInfo{
				Name:          server.Name,
				OpenAPI:       true,
				OpenAPIConfig: server.OpenAPI,
			})
		}
	}

	return result, nil
}

// runGateway runs the MCP gateway (blocking).
// existingBuffer and existingHandler allow reusing a log buffer created earlier
// (e.g., for foreground mode where orchestrator events should also be captured).
// Pass nil for both to create a fresh buffer.
func runGateway(ctx context.Context, rt *runtime.Runtime, stack *config.Stack, stackPath string, result *runtime.LegacyUpResult, port int, verbose bool, printer *output.Printer, existingBuffer *logging.LogBuffer, existingHandler *logging.BufferHandler) error {
	// Create MCP gateway
	gateway := mcp.NewGateway()
	gateway.SetDockerClient(rt.DockerClient())
	gateway.SetVersion(version)

	// Reuse existing log buffer if provided, otherwise create fresh
	logBuffer := existingBuffer
	var bufferHandler *logging.BufferHandler
	if logBuffer != nil && existingHandler != nil {
		bufferHandler = existingHandler
	} else {
		logBuffer = logging.NewLogBuffer(1000)

		logLevel := slog.LevelInfo
		if deployVerbose {
			logLevel = slog.LevelDebug
		}

		// Always log to stderr in daemon mode (stderr is redirected to log file)
		// In foreground verbose mode, use JSON format; in daemon mode use text format
		var innerHandler slog.Handler
		if verbose {
			innerHandler = slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})
		} else if deployDaemonChild {
			innerHandler = slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})
		}
		bufferHandler = logging.NewBufferHandler(logBuffer, innerHandler)
	}
	gateway.SetLogger(slog.New(bufferHandler))

	// Create A2A gateway early if needed (for server setup)
	var a2aGateway *a2a.Gateway
	hasA2A := len(stack.A2AAgents) > 0
	if !hasA2A {
		for _, agent := range stack.Agents {
			if agent.IsA2AEnabled() {
				hasA2A = true
				break
			}
		}
	}
	if hasA2A {
		baseURL := fmt.Sprintf("http://localhost:%d", port)
		a2aGateway = a2a.NewGateway(baseURL)
	}

	// Get embedded web files
	webFS, err := WebFS()
	if err != nil && verbose {
		fmt.Printf("Warning: no embedded web UI: %v\n", err)
	}

	// Start API server FIRST so health checks can succeed
	// MCP servers will be registered asynchronously after the server is running
	server := api.NewServer(gateway, webFS)
	server.SetDockerClient(rt.DockerClient())
	server.SetStackName(stack.Name)
	server.SetLogBuffer(logBuffer)
	if a2aGateway != nil {
		server.SetA2AGateway(a2aGateway)
	}
	addr := fmt.Sprintf(":%d", port)

	// Handle shutdown gracefully
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	// Start server
	serverErr := make(chan error, 1)
	go func() {
		if err := http.ListenAndServe(addr, server.Handler()); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	// Give the server a moment to fail if port is in use
	select {
	case err := <-serverErr:
		// Clean up state file on startup failure
		_ = state.Delete(stack.Name)
		return fmt.Errorf("failed to start server on port %d: %w", port, err)
	case <-time.After(100 * time.Millisecond):
		// Server started successfully
	}

	// Now register MCP servers (after HTTP server is running)
	// This allows the health check to succeed even if MCP servers take time to connect
	registerMCPServers(ctx, gateway, stack, stackPath, result, verbose)

	// Register agents with their access permissions
	if len(result.Agents) > 0 {
		if verbose {
			fmt.Println("\nRegistering agents with gateway...")
		}
		for _, agent := range result.Agents {
			gateway.RegisterAgent(agent.Name, agent.Uses)
			if verbose {
				// Format uses for display
				serverNames := config.ServerNames(agent.Uses)
				fmt.Printf("  Registered agent '%s' with access to: %v\n", agent.Name, serverNames)
			}
		}
	}

	// Register A2A agents
	if a2aGateway != nil {
		if verbose {
			fmt.Println("\nRegistering A2A agents...")
		}

		// Register local A2A agents (agents with a2a config)
		for _, agent := range stack.Agents {
			if agent.IsA2AEnabled() {
				version := "1.0.0"
				if agent.A2A.Version != "" {
					version = agent.A2A.Version
				}

				// Convert config skills to a2a skills
				skills := make([]a2a.Skill, len(agent.A2A.Skills))
				for i, s := range agent.A2A.Skills {
					skills[i] = a2a.Skill{
						ID:          s.ID,
						Name:        s.Name,
						Description: s.Description,
						Tags:        s.Tags,
					}
				}

				card := a2a.AgentCard{
					Name:         agent.Name,
					Description:  agent.Description,
					Version:      version,
					Skills:       skills,
					Capabilities: a2a.AgentCapabilities{},
				}

				a2aGateway.RegisterLocalAgent(agent.Name, card, nil)
				if verbose {
					fmt.Printf("  Registered local A2A agent '%s' with %d skills\n", agent.Name, len(skills))
				}
			}
		}

		// Register external A2A agents
		for _, a2aAgent := range stack.A2AAgents {
			authType := ""
			authToken := ""
			authHeader := ""

			if a2aAgent.Auth != nil {
				authType = a2aAgent.Auth.Type
				if a2aAgent.Auth.TokenEnv != "" {
					authToken = os.Getenv(a2aAgent.Auth.TokenEnv)
				}
				authHeader = a2aAgent.Auth.HeaderName
			}

			if err := a2aGateway.RegisterRemoteAgent(ctx, a2aAgent.Name, a2aAgent.URL, authType, authToken, authHeader); err != nil {
				if verbose {
					fmt.Printf("  Warning: failed to register A2A agent %s: %v\n", a2aAgent.Name, err)
				}
			}
		}
	}

	// Register A2A agents as MCP tool providers (for agent-to-agent skill equipping)
	if err := registerAgentAdapters(ctx, gateway, stack, port, verbose); err != nil {
		return fmt.Errorf("registering agent adapters: %w", err)
	}

	// Set up hot reload handler
	reloadHandler := reload.NewHandler(stackPath, stack, gateway, rt, port, deployBasePort, port)
	reloadHandler.SetLogger(slog.New(bufferHandler))
	reloadHandler.SetNoExpand(deployNoExpand)
	reloadHandler.SetRegisterServerFunc(func(ctx context.Context, server config.MCPServer, hostPort int) error {
		return registerSingleMCPServer(ctx, gateway, stack, stackPath, server, hostPort, verbose)
	})
	server.SetReloadHandler(reloadHandler)

	// Start file watcher if --watch flag is set
	var watchCtx context.Context
	var watchCancel context.CancelFunc
	if deployWatch {
		watchCtx, watchCancel = context.WithCancel(ctx)
		defer watchCancel()

		watcher := reload.NewWatcher(stackPath, func() error {
			result, err := reloadHandler.Reload(watchCtx)
			if err != nil {
				return err
			}
			if !result.Success {
				return fmt.Errorf("%s", result.Message)
			}
			return nil
		})
		watcher.SetLogger(slog.New(bufferHandler))

		go func() {
			if err := watcher.Watch(watchCtx); err != nil && err != context.Canceled {
				slog.New(bufferHandler).Error("file watcher error", "error", err)
			}
		}()

		if verbose {
			fmt.Printf("\nFile watcher enabled for: %s\n", stackPath)
		}
	}

	if verbose {
		fmt.Printf("\nMCP Gateway running:\n")
		fmt.Printf("  POST /mcp         - JSON-RPC endpoint\n")
		fmt.Printf("  GET  /sse         - SSE endpoint (for Claude Desktop)\n")
		fmt.Printf("  POST /message     - SSE message endpoint\n")
		if a2aGateway != nil {
			fmt.Printf("\nA2A Protocol endpoints:\n")
			fmt.Printf("  GET  /.well-known/agent.json - Agent discovery\n")
			fmt.Printf("  GET  /a2a/{agent}            - Agent card\n")
			fmt.Printf("  POST /a2a/{agent}            - JSON-RPC endpoint\n")
		}
		fmt.Printf("\nWeb UI available at http://localhost%s/\n", addr)
		fmt.Printf("API endpoints:\n")
		fmt.Printf("  GET  /api/status      - Gateway status (includes unified agents)\n")
		fmt.Printf("  GET  /api/mcp-servers - List MCP servers\n")
		fmt.Printf("  GET  /api/tools       - List tools\n")
		fmt.Printf("  POST /api/reload      - Trigger configuration reload\n")
		fmt.Printf("  GET  /health          - Liveness check (daemon is alive)\n")
		fmt.Printf("  GET  /ready           - Readiness check (all MCP servers initialized)\n")
		fmt.Println("\nPress Ctrl+C to stop...")
	}

	// Wait for shutdown signal or server error
	select {
	case <-done:
		if verbose {
			fmt.Println("\nShutting down...")
		}
		// Clean up state file on graceful shutdown
		_ = state.Delete(stack.Name)
	case err := <-serverErr:
		_ = state.Delete(stack.Name)
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}

// registerMCPServers registers all MCP servers with the gateway.
// This is called after the HTTP server is running so health checks can succeed.
func registerMCPServers(ctx context.Context, gateway *mcp.Gateway, stack *config.Stack, stackPath string, result *runtime.LegacyUpResult, verbose bool) {
	// Build a map from MCP server name to config for transport lookup
	serverConfigs := make(map[string]config.MCPServer)
	for _, s := range stack.MCPServers {
		serverConfigs[s.Name] = s
	}

	// Register MCP servers with the gateway
	if verbose {
		fmt.Println("\nRegistering MCP servers with gateway...")
	}
	for _, server := range result.MCPServers {
		serverCfg := serverConfigs[server.Name]

		// Determine transport type
		var transport mcp.Transport
		switch serverCfg.Transport {
		case "sse":
			transport = mcp.TransportSSE
		case "stdio":
			transport = mcp.TransportStdio
		default:
			transport = mcp.TransportHTTP
		}

		var cfg mcp.MCPServerConfig
		if server.External {
			// External server - use URL directly
			cfg = mcp.MCPServerConfig{
				Name:      server.Name,
				Transport: transport,
				Endpoint:  server.URL,
				External:  true,
				Tools:     serverCfg.Tools,
			}
		} else if server.LocalProcess {
			// Local process server - use command
			cfg = mcp.MCPServerConfig{
				Name:         server.Name,
				LocalProcess: true,
				Command:      server.Command,
				WorkDir:      filepath.Dir(stackPath), // Use stack directory
				Env:          serverCfg.Env,
				Tools:        serverCfg.Tools,
			}
		} else if server.SSH {
			// SSH server - use SSH command wrapper
			cfg = mcp.MCPServerConfig{
				Name:            server.Name,
				SSH:             true,
				Command:         server.Command,
				SSHHost:         server.SSHHost,
				SSHUser:         server.SSHUser,
				SSHPort:         server.SSHPort,
				SSHIdentityFile: server.SSHIdentityFile,
				Env:             serverCfg.Env,
				Tools:           serverCfg.Tools,
			}
		} else if server.OpenAPI {
			// OpenAPI server - create adapter from spec
			openAPICfg := server.OpenAPIConfig
			cfg = mcp.MCPServerConfig{
				Name:    server.Name,
				OpenAPI: true,
				OpenAPIConfig: &mcp.OpenAPIClientConfig{
					Spec:     openAPICfg.Spec,
					BaseURL:  openAPICfg.BaseURL,
					NoExpand: deployNoExpand,
				},
				Tools: serverCfg.Tools,
			}
			// Handle authentication
			if openAPICfg.Auth != nil {
				cfg.OpenAPIConfig.AuthType = openAPICfg.Auth.Type
				if openAPICfg.Auth.Type == "bearer" && openAPICfg.Auth.TokenEnv != "" {
					cfg.OpenAPIConfig.AuthToken = os.Getenv(openAPICfg.Auth.TokenEnv)
				} else if openAPICfg.Auth.Type == "header" {
					cfg.OpenAPIConfig.AuthHeader = openAPICfg.Auth.Header
					if openAPICfg.Auth.ValueEnv != "" {
						cfg.OpenAPIConfig.AuthValue = os.Getenv(openAPICfg.Auth.ValueEnv)
					}
				}
			}
			// Handle operations filter
			if openAPICfg.Operations != nil {
				cfg.OpenAPIConfig.Include = openAPICfg.Operations.Include
				cfg.OpenAPIConfig.Exclude = openAPICfg.Operations.Exclude
			}
		} else if transport == mcp.TransportStdio {
			// Container stdio
			cfg = mcp.MCPServerConfig{
				Name:        server.Name,
				Transport:   transport,
				ContainerID: server.ContainerID,
				Tools:       serverCfg.Tools,
			}
		} else {
			// Container HTTP/SSE
			cfg = mcp.MCPServerConfig{
				Name:      server.Name,
				Transport: transport,
				Endpoint:  fmt.Sprintf("http://localhost:%d/mcp", server.HostPort),
				Tools:     serverCfg.Tools,
			}
		}

		if err := gateway.RegisterMCPServer(ctx, cfg); err != nil {
			if verbose {
				fmt.Printf("  Warning: failed to register MCP server %s: %v\n", server.Name, err)
			}
		}
	}
}

// registerSingleMCPServer registers a single MCP server with the gateway.
// Used by the reload handler to register newly added servers.
func registerSingleMCPServer(ctx context.Context, gateway *mcp.Gateway, stack *config.Stack, stackPath string, server config.MCPServer, hostPort int, verbose bool) error {
	// Determine transport type
	var transport mcp.Transport
	switch server.Transport {
	case "sse":
		transport = mcp.TransportSSE
	case "stdio":
		transport = mcp.TransportStdio
	default:
		transport = mcp.TransportHTTP
	}

	var cfg mcp.MCPServerConfig
	if server.IsExternal() {
		cfg = mcp.MCPServerConfig{
			Name:      server.Name,
			Transport: transport,
			Endpoint:  server.URL,
			External:  true,
			Tools:     server.Tools,
		}
	} else if server.IsLocalProcess() {
		cfg = mcp.MCPServerConfig{
			Name:         server.Name,
			LocalProcess: true,
			Command:      server.Command,
			WorkDir:      filepath.Dir(stackPath),
			Env:          server.Env,
			Tools:        server.Tools,
		}
	} else if server.IsSSH() {
		cfg = mcp.MCPServerConfig{
			Name:            server.Name,
			SSH:             true,
			Command:         server.Command,
			SSHHost:         server.SSH.Host,
			SSHUser:         server.SSH.User,
			SSHPort:         server.SSH.Port,
			SSHIdentityFile: server.SSH.IdentityFile,
			Env:             server.Env,
			Tools:           server.Tools,
		}
	} else if server.IsOpenAPI() {
		openAPICfg := server.OpenAPI
		cfg = mcp.MCPServerConfig{
			Name:    server.Name,
			OpenAPI: true,
			OpenAPIConfig: &mcp.OpenAPIClientConfig{
				Spec:     openAPICfg.Spec,
				BaseURL:  openAPICfg.BaseURL,
				NoExpand: deployNoExpand,
			},
			Tools: server.Tools,
		}
		if openAPICfg.Auth != nil {
			cfg.OpenAPIConfig.AuthType = openAPICfg.Auth.Type
			if openAPICfg.Auth.Type == "bearer" && openAPICfg.Auth.TokenEnv != "" {
				cfg.OpenAPIConfig.AuthToken = os.Getenv(openAPICfg.Auth.TokenEnv)
			} else if openAPICfg.Auth.Type == "header" {
				cfg.OpenAPIConfig.AuthHeader = openAPICfg.Auth.Header
				if openAPICfg.Auth.ValueEnv != "" {
					cfg.OpenAPIConfig.AuthValue = os.Getenv(openAPICfg.Auth.ValueEnv)
				}
			}
		}
		if openAPICfg.Operations != nil {
			cfg.OpenAPIConfig.Include = openAPICfg.Operations.Include
			cfg.OpenAPIConfig.Exclude = openAPICfg.Operations.Exclude
		}
	} else if transport == mcp.TransportStdio {
		// For stdio, we need to find the container ID
		// This requires querying Docker - for now we return an error as stdio
		// containers need special handling that requires the container ID
		return fmt.Errorf("stdio transport containers must be started via full reload")
	} else {
		// Container HTTP/SSE
		cfg = mcp.MCPServerConfig{
			Name:      server.Name,
			Transport: transport,
			Endpoint:  fmt.Sprintf("http://localhost:%d/mcp", hostPort),
			Tools:     server.Tools,
		}
	}

	return gateway.RegisterMCPServer(ctx, cfg)
}

// forkDeployDaemon starts the daemon child process
func forkDeployDaemon(stackPath string, port int, basePort int, noExpand bool, watch bool) (int, error) {
	// Get current executable
	exe, err := os.Executable()
	if err != nil {
		return 0, fmt.Errorf("getting executable: %w", err)
	}

	// Ensure log directory exists
	if err := state.EnsureLogDir(); err != nil {
		return 0, fmt.Errorf("creating log directory: %w", err)
	}

	// Get stack name for log file
	stack, err := config.LoadStack(stackPath)
	if err != nil {
		return 0, fmt.Errorf("loading stack: %w", err)
	}

	// Open log file
	logFile, err := os.OpenFile(state.LogPath(stack.Name), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return 0, fmt.Errorf("opening log file: %w", err)
	}

	// Build command with --daemon-child flag
	args := []string{"deploy", stackPath,
		"--daemon-child",
		"--port", strconv.Itoa(port),
		"--base-port", strconv.Itoa(basePort)}
	if noExpand {
		args = append(args, "--no-expand")
	}
	if watch {
		args = append(args, "--watch")
	}
	cmd := exec.Command(exe, args...)

	// Detach from terminal
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true, // Create new session
	}

	// Redirect stdio to log file
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil

	// Inherit environment
	cmd.Env = os.Environ()

	// Start child process
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return 0, fmt.Errorf("starting daemon: %w", err)
	}

	// Don't wait - let it run in background
	return cmd.Process.Pid, nil
}

// registerAgentAdapters creates A2A client adapters for agents that are used by other agents.
// This allows Agent A to "equip" Agent B as a skill provider through the unified MCP interface.
func registerAgentAdapters(_ context.Context, mcpGateway *mcp.Gateway, stack *config.Stack, port int, verbose bool) error {
	// Build map of A2A-enabled agents with their configs
	a2aAgentConfigs := make(map[string]*config.Agent)
	for i := range stack.Agents {
		agent := &stack.Agents[i]
		if agent.IsA2AEnabled() {
			a2aAgentConfigs[agent.Name] = agent
		}
	}

	// Find agents that are "used" by other agents (not MCP servers)
	usedAgents := make(map[string]bool)
	for _, agent := range stack.Agents {
		for _, selector := range agent.Uses {
			if _, isA2A := a2aAgentConfigs[selector.Server]; isA2A {
				usedAgents[selector.Server] = true
			}
		}
	}

	if len(usedAgents) == 0 {
		return nil
	}

	if verbose {
		fmt.Println("\nRegistering agent-to-agent adapters...")
	}

	// Create adapters for each used agent
	for agentName := range usedAgents {
		agentCfg := a2aAgentConfigs[agentName]

		// Local A2A agents are accessible at the gateway's /a2a/{name} endpoint
		endpoint := fmt.Sprintf("http://localhost:%d", port)

		// Create the adapter
		a2aAdapter := adapter.NewA2AClientAdapter(agentName, endpoint)

		// For local agents, initialize directly from config (no HTTP needed)
		// This avoids the chicken-and-egg problem where the HTTP server
		// hasn't started yet when we try to fetch the agent card.
		version := "1.0.0"
		if agentCfg.A2A.Version != "" {
			version = agentCfg.A2A.Version
		}

		// Convert config skills to a2a skills
		skills := make([]a2a.Skill, len(agentCfg.A2A.Skills))
		for i, s := range agentCfg.A2A.Skills {
			skills[i] = a2a.Skill{
				ID:          s.ID,
				Name:        s.Name,
				Description: s.Description,
				Tags:        s.Tags,
			}
		}

		a2aAdapter.InitializeFromSkills(version, skills)

		// Add to MCP gateway router
		mcpGateway.Router().AddClient(a2aAdapter)

		if verbose {
			fmt.Printf("  Registered agent '%s' as skill provider with %d tools\n", agentName, len(a2aAdapter.Tools()))
		}
	}

	// Refresh aggregated tools in the router
	mcpGateway.Router().RefreshTools()

	return nil
}

// waitForReady polls the ready endpoint until it returns 200 or timeout.
// The /ready endpoint only succeeds when all MCP servers are initialized,
// unlike /health which succeeds immediately when the HTTP server starts.
func waitForReady(port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 500 * time.Millisecond}
	url := fmt.Sprintf("http://localhost:%d/ready", port)

	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			statusOK := resp.StatusCode == http.StatusOK
			resp.Body.Close() // Always close body, even on non-200 status
			if statusOK {
				return nil
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("readiness check timed out after %v", timeout)
}
