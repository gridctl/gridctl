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
	"github.com/gridctl/gridctl/internal/probe"
	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/logging"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/metrics"
	"github.com/gridctl/gridctl/pkg/pins"
	"github.com/gridctl/gridctl/pkg/provisioner"
	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/gridctl/gridctl/pkg/reload"
	"github.com/gridctl/gridctl/pkg/runtime"
	"github.com/gridctl/gridctl/pkg/skills"
	"github.com/gridctl/gridctl/pkg/state"
	"github.com/gridctl/gridctl/pkg/token"
	"github.com/gridctl/gridctl/pkg/tracing"
	"github.com/gridctl/gridctl/pkg/vault"
)

// WebFSFunc is a function that returns embedded web UI files.
// This decouples the controller from the build-tag-conditional embed logic.
type WebFSFunc func() (fs.FS, error)

// GatewayInstance holds all components of a running gateway.
type GatewayInstance struct {
	Gateway        *mcp.Gateway
	APIServer      *api.Server
	HTTPServer     *http.Server
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

	// vaultStore for API server injection and log redaction.
	vaultStore *vault.Store

	// pinStore for API server injection (schema pin management).
	pinStore *pins.PinStore

	// tracingProvider is retained so Shutdown() can be called on gateway exit.
	tracingProvider *tracing.Provider
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

// SetVaultStore sets the vault store for API server injection and log redaction.
func (b *GatewayBuilder) SetVaultStore(v *vault.Store) {
	b.vaultStore = v
}

// SetPinStore sets the pin store for API server injection.
func (b *GatewayBuilder) SetPinStore(ps *pins.PinStore) {
	b.pinStore = ps
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

	// Phase 1a: Enable code mode if configured
	codeModeEnabled := b.config.CodeMode
	if !codeModeEnabled && b.stack.Gateway != nil && b.stack.Gateway.CodeMode == "on" {
		codeModeEnabled = true
	}
	if codeModeEnabled {
		timeout := 30 * time.Second
		if b.stack.Gateway != nil && b.stack.Gateway.CodeModeTimeout > 0 {
			timeout = time.Duration(b.stack.Gateway.CodeModeTimeout) * time.Second
		}
		inst.Gateway.SetCodeMode(timeout)
	}

	// Phase 1a2: Set default output format if configured
	if b.stack.Gateway != nil && b.stack.Gateway.OutputFormat != "" {
		inst.Gateway.SetDefaultOutputFormat(b.stack.Gateway.OutputFormat)
	}

	// Phase 1a3: Set max tool result bytes if configured
	if b.stack.Gateway != nil && b.stack.Gateway.MaxToolResultBytes != 0 {
		inst.Gateway.SetMaxToolResultBytes(b.stack.Gateway.MaxToolResultBytes)
	}

	// Phase 1b: Create registry server (internal MCP server)
	regDir := filepath.Join(state.BaseDir(), "registry")
	if b.registryDir != "" {
		regDir = b.registryDir
	}
	registryStore := registry.NewStore(regDir)
	registryServer := registry.New(registryStore,
		registry.WithToolCaller(inst.Gateway, nil))
	inst.RegistryServer = registryServer

	// Phase 2: Configure logging
	var logErr error
	inst.LogBuffer, inst.Handler, logErr = b.buildLogging(verbose)
	if logErr != nil {
		return nil, logErr
	}
	inst.Gateway.SetLogger(slog.New(inst.Handler))
	registryServer.SetLogger(slog.New(inst.Handler))

	// Initialize registry after logging is configured so warnings are captured
	if err := registryServer.Initialize(context.Background()); err != nil {
		slog.New(inst.Handler).Warn("registry initialization failed", "error", err)
	}
	if registryServer.HasContent() {
		inst.Gateway.Router().AddClient(registryServer)
		inst.Gateway.Router().RefreshTools()
	}

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
	var apiErr error
	inst.APIServer, apiErr = b.buildAPIServer(inst.Gateway, inst.LogBuffer, webFS, inst.RegistryServer, inst.Handler)
	if apiErr != nil {
		return nil, apiErr
	}

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
	if b.rt != nil {
		registrar.SetRuntime(b.rt.Runtime())
	}
	registrar.SetBasePort(b.config.BasePort)
	registrar.RegisterAll(ctx, b.result, b.stack, b.stackPath)

	// Start periodic health monitoring and autoscaler tick loop.
	gateway.StartHealthMonitor(ctx, mcp.DefaultHealthCheckInterval)
	gateway.StartAutoscaler(ctx, mcp.DefaultAutoscalerInterval)

	// Start background skill update check (non-blocking)
	skills.CheckUpdatesBackground(
		filepath.Join(state.BaseDir(), "registry"),
		slog.New(bufferHandler),
	)

	// Set up hot reload
	b.setupHotReload(ctx, inst, registrar, bufferHandler, verbose)

	if verbose {
		b.printEndpoints(inst)
	}

	// Wait for shutdown signal or server error
	return b.waitForShutdown(inst, bufferHandler, serverErr, verbose)
}

// buildLogging creates or reuses the log buffer and handler.
// The returned handler chain is: RedactingHandler → BufferHandler → inner (JSON/Text [+ file]).
func (b *GatewayBuilder) buildLogging(verbose bool) (*logging.LogBuffer, slog.Handler, error) {
	if b.existingBuffer != nil && b.existingHandler != nil {
		return b.existingBuffer, b.existingHandler, nil
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

	// Wire file output: CLI flag takes precedence over stack.yaml logging.file.
	logFilePath := b.config.LogFile
	if logFilePath == "" && b.stack.Logging != nil {
		logFilePath = b.stack.Logging.File
	}
	if logFilePath != "" {
		fileOpts := logging.FileOpts{}
		if b.stack.Logging != nil {
			fileOpts.MaxSizeMB = b.stack.Logging.MaxSizeMB
			fileOpts.MaxAgeDays = b.stack.Logging.MaxAgeDays
			fileOpts.MaxBackups = b.stack.Logging.MaxBackups
		}
		fileHandler, err := logging.NewFileHandler(logFilePath, fileOpts)
		if err != nil {
			return nil, nil, err
		}
		if innerHandler != nil {
			innerHandler = logging.NewMultiHandler(innerHandler, fileHandler)
		} else {
			innerHandler = fileHandler
		}
	}

	bufferHandler := logging.NewBufferHandler(logBuffer, innerHandler)
	redactHandler := logging.NewRedactingHandler(bufferHandler)

	// Register vault values for redaction
	if b.vaultStore != nil {
		redactHandler.RegisterRedactValues(b.vaultStore.Values())
	}

	// Log startup entry when writing to a file
	if logFilePath != "" {
		maxSizeMB := 100
		if b.stack.Logging != nil && b.stack.Logging.MaxSizeMB > 0 {
			maxSizeMB = b.stack.Logging.MaxSizeMB
		}
		slog.New(redactHandler).Info("log file opened",
			"path", logFilePath,
			"rotation", fmt.Sprintf("%dMB", maxSizeMB))
	}

	return logBuffer, redactHandler, nil
}

// buildAPIServer creates and configures the API server.
func (b *GatewayBuilder) buildAPIServer(gateway *mcp.Gateway, logBuffer *logging.LogBuffer, webFS fs.FS, registryServer *registry.Server, handler slog.Handler) (*api.Server, error) {
	server := api.NewServer(gateway, webFS)
	server.SetDockerClient(b.rt.DockerClient())
	server.SetStackName(b.stack.Name)
	server.SetStackFile(b.config.StackPath)
	server.SetLogBuffer(logBuffer)
	server.SetProvisionerRegistry(provisioner.NewRegistry(), "gridctl")
	server.SetGatewayAddr(fmt.Sprintf("http://localhost:%d", b.config.Port))

	if b.stack.Gateway != nil && len(b.stack.Gateway.AllowedOrigins) > 0 {
		server.SetAllowedOrigins(b.stack.Gateway.AllowedOrigins)
	} else {
		server.SetAllowedOrigins([]string{"*"})
	}

	if b.stack.Gateway != nil && b.stack.Gateway.Auth != nil {
		server.SetAuth(b.stack.Gateway.Auth.Type, b.stack.Gateway.Auth.Token, b.stack.Gateway.Auth.Header)
	}

	if registryServer != nil {
		server.SetRegistryServer(registryServer)
	}

	if b.vaultStore != nil {
		server.SetVaultStore(b.vaultStore)
	}

	if b.pinStore != nil {
		server.SetPinStore(b.pinStore)
	}

	// Wire token usage metrics
	counter, err := b.buildTokenCounter()
	if err != nil {
		return nil, err
	}
	accumulator := metrics.NewAccumulator(10000)
	observer := metrics.NewObserver(counter, accumulator)
	gateway.SetToolCallObserver(observer)
	gateway.SetTokenCounter(counter)
	gateway.SetFormatSavingsRecorder(accumulator)
	server.SetMetricsAccumulator(accumulator)
	server.SetTokenizerName(b.tokenizerName())

	// Wire the wizard's "Discover tools" probe. Scope: external URL
	// servers only — container / stdio / local-process / SSH / OpenAPI are
	// curated post-deploy from the topology sidebar.
	probeCache := probe.NewCache(probe.DefaultTTL)
	prober := probe.NewProber(probeCache)
	if handler != nil {
		prober.SetLogger(slog.New(handler).With("subsystem", "probe"))
	}
	server.SetProber(prober)

	// Wire distributed tracing
	tracingCfg := buildTracingConfig(b.stack.Gateway)
	tracingProvider := tracing.NewProvider(tracingCfg)
	if handler != nil {
		tracingProvider.SetLogger(slog.New(handler))
	}
	if err := tracingProvider.Init(context.Background()); err != nil {
		slog.New(handler).Warn("tracing init failed", "error", err)
	}
	b.tracingProvider = tracingProvider
	server.SetTraceBuffer(tracingProvider.Buffer)

	return server, nil
}

// tokenizerName returns the configured tokenizer mode, defaulting to "embedded".
func (b *GatewayBuilder) tokenizerName() string {
	if b.stack.Gateway != nil && b.stack.Gateway.Tokenizer != "" {
		return b.stack.Gateway.Tokenizer
	}
	return "embedded"
}

// buildTokenCounter creates the token counter based on the stack gateway config.
// "embedded" (default): cl100k_base BPE vocabulary, pure Go, no network.
// "api": Anthropic count_tokens endpoint — Anthropic-specific, requires a key.
func (b *GatewayBuilder) buildTokenCounter() (token.Counter, error) {
	switch b.tokenizerName() {
	case "api":
		apiKey := ""
		if b.stack.Gateway != nil {
			apiKey = b.stack.Gateway.TokenizerAPIKey
		}
		if apiKey == "" {
			apiKey = os.Getenv("ANTHROPIC_API_KEY")
		}
		if apiKey == "" {
			return nil, fmt.Errorf("gateway.tokenizer is \"api\" but no API key is configured: set ANTHROPIC_API_KEY or add tokenizer_api_key to stack.yaml")
		}
		return token.NewAPICounter(apiKey)
	case "embedded", "":
		c, err := token.NewTiktokenCounter()
		if err != nil {
			return nil, fmt.Errorf("failed to initialize embedded tokenizer: %w", err)
		}
		return c, nil
	default:
		// Unknown values fall back to embedded rather than failing.
		c, err := token.NewTiktokenCounter()
		if err != nil {
			return nil, fmt.Errorf("failed to initialize embedded tokenizer: %w", err)
		}
		return c, nil
	}
}

// buildTracingConfig extracts tracing config from gateway config with defaults.
func buildTracingConfig(gw *config.GatewayConfig) *tracing.Config {
	cfg := tracing.DefaultConfig()
	if gw == nil || gw.Tracing == nil {
		return cfg
	}
	t := gw.Tracing
	cfg.Enabled = t.Enabled
	if t.Sampling > 0 {
		cfg.Sampling = t.Sampling
	}
	if t.Retention != "" {
		cfg.Retention = t.Retention
	}
	cfg.Export = t.Export
	cfg.Endpoint = t.Endpoint
	return cfg
}

// setupHotReload configures file watching and reload for the stack.
func (b *GatewayBuilder) setupHotReload(ctx context.Context, inst *GatewayInstance, registrar *ServerRegistrar, handler slog.Handler, verbose bool) {
	var vaultLookup config.VaultLookup
	var vaultSetLookup config.VaultSetLookup
	if b.vaultStore != nil {
		vaultLookup = b.vaultStore
		vaultSetLookup = newVaultSetAdapter(b.vaultStore)
	}
	reloadHandler := reload.NewHandler(b.stackPath, b.stack, inst.Gateway, b.rt, b.config.Port, b.config.BasePort, vaultLookup, vaultSetLookup)
	reloadHandler.SetLogger(slog.New(handler))
	reloadHandler.SetNoExpand(b.config.NoExpand)
	// stackPath is threaded through the callback by the reload handler rather
	// than captured from b.stackPath: in stackless mode b.stackPath starts
	// empty and is only populated once POST /api/stack/initialize runs, which
	// updates reloadHandler.stackPath. The handler already holds its mutex
	// when invoking this callback, so reading h.stackPath there is safe and
	// avoids a reentrant-lock deadlock a getter-based approach would cause.
	reloadHandler.SetRegisterServerFunc(func(ctx context.Context, server config.MCPServer, replicas []reload.ReplicaRuntime, stackPath string) error {
		runtimes := make([]ReplicaRuntime, 0, len(replicas))
		for _, rep := range replicas {
			runtimes = append(runtimes, ReplicaRuntime{HostPort: rep.HostPort, ContainerID: rep.ContainerID})
		}
		return registrar.RegisterOne(ctx, server, runtimes, stackPath)
	})
	inst.APIServer.SetReloadHandler(reloadHandler)

	// startWatcher starts a file watcher for the given stack path.
	// It is called immediately when --watch is active, and exposed via SetStartWatcher
	// so POST /api/stack/initialize can activate watching after cold-loading.
	startWatcher := func(stackPath string) {
		watchCtx, _ := context.WithCancel(ctx) //nolint:govet,gosec // cancel called on process exit via ctx

		watcher := reload.NewWatcher(stackPath, func() error {
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
	}

	// Expose the watcher starter so initialize can activate it on demand.
	inst.APIServer.SetStartWatcher(startWatcher)

	if b.config.Watch {
		startWatcher(b.stackPath)

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

		if b.tracingProvider != nil {
			if err := b.tracingProvider.Shutdown(shutdownCtx); err != nil {
				logger.Error("tracing shutdown error", "error", err)
			}
		}

		_ = state.Delete(b.stack.Name)
	case err := <-serverErr:
		_ = state.Delete(b.stack.Name)
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}
