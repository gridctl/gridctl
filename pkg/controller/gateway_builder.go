package controller

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gridctl/gridctl/internal/api"
	"github.com/gridctl/gridctl/pkg/a2a"
	"github.com/gridctl/gridctl/pkg/adapter"
	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/logging"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/provisioner"
	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/gridctl/gridctl/pkg/reload"
	"github.com/gridctl/gridctl/pkg/runtime"
	"github.com/gridctl/gridctl/pkg/state"
)

// WebFSFunc is a function that returns embedded web UI files.
// This decouples the controller from the build-tag-conditional embed logic.
type WebFSFunc func() (fs.FS, error)

// GatewayInstance holds all components of a running gateway.
type GatewayInstance struct {
	Gateway        *mcp.Gateway
	APIServer      *api.Server
	HTTPServer     *http.Server
	A2AGateway     *a2a.Gateway
	LogBuffer      *logging.LogBuffer
	Handler        slog.Handler
	RegistryServer *registry.Server // Internal registry MCP server (nil if empty)
}

// GatewayBuilder constructs and runs the MCP gateway from a stack config.
type GatewayBuilder struct {
	config    Config
	stack     *config.Stack
	stackPath string
	rt        *runtime.Orchestrator
	result    *runtime.UpResult
	version   string
	webFS     WebFSFunc

	// Pre-created log infrastructure (for foreground mode where orchestrator
	// events should also be captured before gateway starts).
	existingBuffer  *logging.LogBuffer
	existingHandler slog.Handler

	// registryDir overrides the default registry directory for testing.
	registryDir string
}

// NewGatewayBuilder creates a GatewayBuilder.
func NewGatewayBuilder(cfg Config, stack *config.Stack, stackPath string, rt *runtime.Orchestrator, result *runtime.UpResult) *GatewayBuilder {
	return &GatewayBuilder{
		config:    cfg,
		stack:     stack,
		stackPath: stackPath,
		rt:        rt,
		result:    result,
	}
}

// SetVersion sets the gateway version string.
func (b *GatewayBuilder) SetVersion(v string) {
	b.version = v
}

// SetWebFS sets the function for getting embedded web files.
func (b *GatewayBuilder) SetWebFS(fn WebFSFunc) {
	b.webFS = fn
}

// SetExistingLogInfra allows reusing a log buffer/handler created earlier
// (e.g., in foreground mode where orchestrator events should also be captured).
func (b *GatewayBuilder) SetExistingLogInfra(buffer *logging.LogBuffer, handler slog.Handler) {
	b.existingBuffer = buffer
	b.existingHandler = handler
}

// BuildAndRun constructs the gateway and runs it until shutdown.
// This is the main blocking call that replaces the old runGateway() function.
func (b *GatewayBuilder) BuildAndRun(ctx context.Context, verbose bool) error {
	inst, err := b.Build(verbose)
	if err != nil {
		return err
	}
	return b.Run(ctx, inst, verbose)
}

// Build constructs all gateway components without starting the HTTP server.
func (b *GatewayBuilder) Build(verbose bool) (*GatewayInstance, error) {
	inst := &GatewayInstance{}

	// Phase 1: Create MCP Gateway
	inst.Gateway = mcp.NewGateway()
	inst.Gateway.SetDockerClient(b.rt.DockerClient())
	inst.Gateway.SetVersion(b.version)

	// Phase 1b: Create registry server (internal MCP server)
	regDir := filepath.Join(state.BaseDir(), "registry")
	if b.registryDir != "" {
		regDir = b.registryDir
	}
	registryStore := registry.NewStore(regDir)
	registryServer := registry.New(registryStore, inst.Gateway)
	inst.RegistryServer = registryServer

	// Phase 2: Configure logging
	inst.LogBuffer, inst.Handler = b.buildLogging(verbose)
	inst.Gateway.SetLogger(slog.New(inst.Handler))

	// Initialize registry after logging is configured so warnings are captured
	if err := registryServer.Initialize(context.Background()); err != nil {
		slog.New(inst.Handler).Warn("registry initialization failed", "error", err)
	}
	if registryServer.HasContent() {
		inst.Gateway.Router().AddClient(registryServer)
		inst.Gateway.Router().RefreshTools()
	}

	// Phase 3: Create A2A gateway if needed
	inst.A2AGateway = b.buildA2AGateway(inst.Handler)

	// Phase 4: Get embedded web files
	var webFS fs.FS
	if b.webFS != nil {
		var err error
		webFS, err = b.webFS()
		if err != nil && verbose {
			fmt.Printf("Warning: no embedded web UI: %v\n", err)
		}
	}

	// Phase 5: Create API server
	inst.APIServer = b.buildAPIServer(inst.Gateway, inst.A2AGateway, inst.LogBuffer, webFS, inst.RegistryServer)

	// Phase 6: Create HTTP server
	inst.HTTPServer = &http.Server{
		Addr:              fmt.Sprintf(":%d", b.config.Port),
		Handler:           inst.APIServer.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	return inst, nil
}

// Run starts the HTTP server, registers MCP servers, and blocks until shutdown.
func (b *GatewayBuilder) Run(ctx context.Context, inst *GatewayInstance, verbose bool) error {
	gateway := inst.Gateway
	bufferHandler := inst.Handler

	// Start periodic session cleanup
	gateway.StartCleanup(ctx)
	defer gateway.Close()

	// Start HTTP server
	serverErr := make(chan error, 1)
	go func() {
		if err := inst.HTTPServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	// Give the server a moment to fail if port is in use
	select {
	case err := <-serverErr:
		_ = state.Delete(b.stack.Name)
		return fmt.Errorf("failed to start server on port %d: %w", b.config.Port, err)
	case <-time.After(100 * time.Millisecond):
		// Server started successfully
	}

	// Register MCP servers (after HTTP server is running for health checks)
	registrar := NewServerRegistrar(gateway, b.config.NoExpand)
	registrar.SetLogger(slog.New(bufferHandler))
	registrar.RegisterAll(ctx, b.result, b.stack, b.stackPath)

	// Start periodic health monitoring
	gateway.StartHealthMonitor(ctx, mcp.DefaultHealthCheckInterval)

	// Register agents with access permissions
	b.registerAgents(gateway, verbose)

	// Register A2A agents
	b.registerA2AAgents(ctx, inst.A2AGateway, bufferHandler, verbose)

	// Register agent-to-agent adapters (A2A agents as MCP tool providers)
	if err := b.registerAgentAdapters(ctx, gateway, verbose); err != nil {
		return fmt.Errorf("registering agent adapters: %w", err)
	}

	// Set up hot reload
	b.setupHotReload(ctx, inst, registrar, bufferHandler, verbose)

	if verbose {
		b.printEndpoints(inst)
	}

	// Wait for shutdown signal or server error
	return b.waitForShutdown(inst, bufferHandler, serverErr, verbose)
}

// buildLogging creates or reuses the log buffer and handler.
// The returned handler chain is: RedactingHandler → BufferHandler → inner (JSON/Text).
func (b *GatewayBuilder) buildLogging(verbose bool) (*logging.LogBuffer, slog.Handler) {
	if b.existingBuffer != nil && b.existingHandler != nil {
		return b.existingBuffer, b.existingHandler
	}

	logBuffer := logging.NewLogBuffer(1000)

	logLevel := slog.LevelInfo
	if b.config.Verbose {
		logLevel = slog.LevelDebug
	}

	var innerHandler slog.Handler
	if verbose {
		innerHandler = slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})
	} else if b.config.DaemonChild {
		innerHandler = slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})
	}

	bufferHandler := logging.NewBufferHandler(logBuffer, innerHandler)
	return logBuffer, logging.NewRedactingHandler(bufferHandler)
}

// buildA2AGateway creates an A2A gateway if the stack has A2A-enabled agents.
func (b *GatewayBuilder) buildA2AGateway(handler slog.Handler) *a2a.Gateway {
	hasA2A := len(b.stack.A2AAgents) > 0
	if !hasA2A {
		for _, agent := range b.stack.Agents {
			if agent.IsA2AEnabled() {
				hasA2A = true
				break
			}
		}
	}
	if !hasA2A {
		return nil
	}

	baseURL := fmt.Sprintf("http://localhost:%d", b.config.Port)
	return a2a.NewGateway(baseURL, slog.New(handler))
}

// buildAPIServer creates and configures the API server.
func (b *GatewayBuilder) buildAPIServer(gateway *mcp.Gateway, a2aGateway *a2a.Gateway, logBuffer *logging.LogBuffer, webFS fs.FS, registryServer *registry.Server) *api.Server {
	server := api.NewServer(gateway, webFS)
	server.SetDockerClient(b.rt.DockerClient())
	server.SetStackName(b.stack.Name)
	server.SetLogBuffer(logBuffer)
	server.SetProvisionerRegistry(provisioner.NewRegistry(), "gridctl")

	if b.stack.Gateway != nil && len(b.stack.Gateway.AllowedOrigins) > 0 {
		server.SetAllowedOrigins(b.stack.Gateway.AllowedOrigins)
	} else {
		server.SetAllowedOrigins([]string{"*"})
	}

	if b.stack.Gateway != nil && b.stack.Gateway.Auth != nil {
		server.SetAuth(b.stack.Gateway.Auth.Type, b.stack.Gateway.Auth.Token, b.stack.Gateway.Auth.Header)
	}

	if a2aGateway != nil {
		server.SetA2AGateway(a2aGateway)
	}

	if registryServer != nil {
		server.SetRegistryServer(registryServer)
	}

	return server
}

// registerAgents registers agents with their access permissions.
func (b *GatewayBuilder) registerAgents(gateway *mcp.Gateway, verbose bool) {
	if len(b.result.Agents) == 0 {
		return
	}

	if verbose {
		fmt.Println("\nRegistering agents with gateway...")
	}
	for _, agent := range b.result.Agents {
		gateway.RegisterAgent(agent.Name, agent.Uses)
		if verbose {
			serverNames := config.ServerNames(agent.Uses)
			fmt.Printf("  Registered agent '%s' with access to: %v\n", agent.Name, serverNames)
		}
	}
}

// registerA2AAgents registers local and external A2A agents.
func (b *GatewayBuilder) registerA2AAgents(ctx context.Context, a2aGateway *a2a.Gateway, handler slog.Handler, verbose bool) {
	if a2aGateway == nil {
		return
	}

	if verbose {
		fmt.Println("\nRegistering A2A agents...")
	}

	// Register local A2A agents (agents with a2a config)
	for _, agent := range b.stack.Agents {
		if agent.IsA2AEnabled() {
			ver := "1.0.0"
			if agent.A2A.Version != "" {
				ver = agent.A2A.Version
			}

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
				Version:      ver,
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
	for _, a2aAgent := range b.stack.A2AAgents {
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

	// Start periodic A2A task cleanup
	a2aGateway.StartCleanup(ctx)
}

// registerAgentAdapters creates A2A client adapters for agents used by other agents.
func (b *GatewayBuilder) registerAgentAdapters(_ context.Context, mcpGateway *mcp.Gateway, verbose bool) error {
	// Build map of A2A-enabled agents
	a2aAgentConfigs := make(map[string]*config.Agent)
	for i := range b.stack.Agents {
		agent := &b.stack.Agents[i]
		if agent.IsA2AEnabled() {
			a2aAgentConfigs[agent.Name] = agent
		}
	}

	// Find agents "used" by other agents
	usedAgents := make(map[string]bool)
	for _, agent := range b.stack.Agents {
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

	for agentName := range usedAgents {
		agentCfg := a2aAgentConfigs[agentName]
		endpoint := fmt.Sprintf("http://localhost:%d", b.config.Port)
		a2aAdapter := adapter.NewA2AClientAdapter(agentName, endpoint)

		ver := "1.0.0"
		if agentCfg.A2A.Version != "" {
			ver = agentCfg.A2A.Version
		}

		skills := make([]a2a.Skill, len(agentCfg.A2A.Skills))
		for i, s := range agentCfg.A2A.Skills {
			skills[i] = a2a.Skill{
				ID:          s.ID,
				Name:        s.Name,
				Description: s.Description,
				Tags:        s.Tags,
			}
		}

		a2aAdapter.InitializeFromSkills(ver, skills)
		mcpGateway.Router().AddClient(a2aAdapter)

		if verbose {
			fmt.Printf("  Registered agent '%s' as skill provider with %d tools\n", agentName, len(a2aAdapter.Tools()))
		}
	}

	mcpGateway.Router().RefreshTools()
	return nil
}

// setupHotReload configures file watching and reload for the stack.
func (b *GatewayBuilder) setupHotReload(ctx context.Context, inst *GatewayInstance, registrar *ServerRegistrar, handler slog.Handler, verbose bool) {
	reloadHandler := reload.NewHandler(b.stackPath, b.stack, inst.Gateway, b.rt, b.config.Port, b.config.BasePort, b.config.Port)
	reloadHandler.SetLogger(slog.New(handler))
	reloadHandler.SetNoExpand(b.config.NoExpand)
	reloadHandler.SetRegisterServerFunc(func(ctx context.Context, server config.MCPServer, hostPort int) error {
		return registrar.RegisterOne(ctx, server, hostPort, b.stackPath)
	})
	inst.APIServer.SetReloadHandler(reloadHandler)

	if b.config.Watch {
		watchCtx, watchCancel := context.WithCancel(ctx)
		defer watchCancel()

		watcher := reload.NewWatcher(b.stackPath, func() error {
			result, err := reloadHandler.Reload(watchCtx)
			if err != nil {
				return err
			}
			if !result.Success {
				return fmt.Errorf("%s", result.Message)
			}
			// Refresh registry if it exists
			if inst.RegistryServer != nil {
				if refreshErr := inst.RegistryServer.RefreshTools(watchCtx); refreshErr != nil {
					slog.New(handler).Warn("registry refresh failed", "error", refreshErr)
				}
				if inst.RegistryServer.HasContent() {
					inst.Gateway.Router().AddClient(inst.RegistryServer)
				} else {
					inst.Gateway.Router().RemoveClient("registry")
				}
				inst.Gateway.Router().RefreshTools()
			}
			return nil
		})
		watcher.SetLogger(slog.New(handler))

		go func() {
			if err := watcher.Watch(watchCtx); err != nil && err != context.Canceled {
				slog.New(handler).Error("file watcher error", "error", err)
			}
		}()

		if verbose {
			fmt.Printf("\nFile watcher enabled for: %s\n", b.stackPath)
		}
	}
}

// printEndpoints prints the gateway endpoint information.
func (b *GatewayBuilder) printEndpoints(inst *GatewayInstance) {
	addr := fmt.Sprintf(":%d", b.config.Port)

	fmt.Printf("\nMCP Gateway running:\n")
	fmt.Printf("  POST /mcp         - JSON-RPC endpoint\n")
	fmt.Printf("  GET  /sse         - SSE endpoint (for Claude Desktop)\n")
	fmt.Printf("  POST /message     - SSE message endpoint\n")
	if inst.A2AGateway != nil {
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

// waitForShutdown blocks until a shutdown signal or server error, then cleans up.
func (b *GatewayBuilder) waitForShutdown(inst *GatewayInstance, handler slog.Handler, serverErr <-chan error, verbose bool) error {
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	select {
	case sig := <-done:
		logger := slog.New(handler)
		logger.Info("received signal, shutting down", "signal", sig)

		if verbose {
			fmt.Println("\nShutting down...")
		}

		// Close API server resources: broadcasts SSE close event while
		// HTTP connections are still alive, then closes gateway clients.
		inst.APIServer.Close()

		// Graceful HTTP shutdown with timeout
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer shutdownCancel()

		if err := inst.HTTPServer.Shutdown(shutdownCtx); err != nil {
			logger.Error("HTTP server shutdown error", "error", err)
		}

		_ = state.Delete(b.stack.Name)
	case err := <-serverErr:
		_ = state.Delete(b.stack.Name)
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}
