package controller

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"

	"github.com/gridctl/gridctl/internal/api"
	"github.com/gridctl/gridctl/internal/probe"
	"github.com/gridctl/gridctl/pkg/agent"
	"github.com/gridctl/gridctl/pkg/agent/compose"
	"github.com/gridctl/gridctl/pkg/agent/dev/devserver"
	"github.com/gridctl/gridctl/pkg/agent/dev/watcher"
	agentgateway "github.com/gridctl/gridctl/pkg/agent/gateway"
	"github.com/gridctl/gridctl/pkg/agent/persist"
	agentruntime "github.com/gridctl/gridctl/pkg/agent/runtime"
	"github.com/gridctl/gridctl/pkg/agent/sandbox"
	"github.com/gridctl/gridctl/pkg/agent/skill"
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
	"github.com/gridctl/gridctl/pkg/telemetry"
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

	// telemetry holds the opt-in disk-persistence writers wired at Build
	// time. Nil when no server in the stack opts in.
	telemetry *telemetryWiring

	// agentDevWatcher is set when buildAPIServer wires the Agent IDE dev
	// server. Run() starts its goroutine against the daemon's lifecycle
	// context so the watcher exits cleanly on shutdown.
	agentDevWatcher *watcher.Watcher
}

// telemetryWiring bundles the three per-signal writers + the otlptrace
// exporter that feeds TracesFileClient. Lifecycle is owned by GatewayBuilder
// (Build/Run/waitForShutdown).
type telemetryWiring struct {
	logRouter      *telemetry.LogRouter
	metricsFlusher *telemetry.MetricsFlusher
	tracesClient   *telemetry.TracesFileClient
	tracesExporter *otlptrace.Exporter // started lazily inside Provider.RegisterExporter
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
	registryServer := registry.New(registryStore)
	inst.RegistryServer = registryServer

	// Phase 1c: Build the agent runtime aggregate. The gateway hangs off
	// SetAgentRuntime so HTTP handlers, the dispatcher, and the IDE all
	// pull from one place. RunStore + ApprovalRegistry + Sandbox are
	// available from the start so the dispatcher can register against
	// the registry server before the router exposes its tools; the
	// ChatModel plugs in later inside buildAPIServer once the vault has
	// resolved provider keys.
	agentSandbox := sandbox.New(0)
	agentRuntime := agentruntime.NewRuntime(
		persist.NewStore(persist.DefaultRunsDir()),
		compose.NewRegistry(),
		agentSandbox,
	)
	inst.Gateway.SetAgentRuntime(agentRuntime)

	// Wire the TS dispatcher onto the registry server. The bindings
	// provider closes over the gateway so per-call collaborators
	// (current AllowedTools snapshot, the runtime's ChatModel) reflect
	// the daemon's live state rather than a snapshot frozen at build
	// time. The ToolCaller is built once here because its only failure
	// mode is a nil gateway — a programmer error we want to surface at
	// build time, not as a silently-disabled tool() binding at first
	// call. SetTSDispatcher MUST run before the router adds the
	// registry server below — Router.RefreshTools queries the
	// registry's Tools(), which only surfaces TS skills when a
	// dispatcher is wired.
	toolCaller, err := agentgateway.NewToolCaller(inst.Gateway)
	if err != nil {
		return nil, fmt.Errorf("agent tool caller: %w", err)
	}
	dispatcher, err := sandbox.NewDispatcher(agentSandbox, makeDispatcherBindings(inst.Gateway, registryStore, registryServer, toolCaller))
	if err != nil {
		return nil, fmt.Errorf("agent dispatcher: %w", err)
	}
	registryServer.SetTSDispatcher(dispatcher)

	// Phase 2: Configure logging
	var logErr error
	inst.LogBuffer, inst.Handler, logErr = b.buildLogging(verbose)
	if logErr != nil {
		return nil, logErr
	}
	inst.Gateway.SetLogger(slog.New(inst.Handler))

	// Seed the in-memory log buffer from any pre-existing per-server
	// logs.jsonl files BEFORE registry init or any other component starts
	// emitting records. Otherwise live records can interleave with seeded
	// history and scramble ring ordering.
	b.seedLogsFromDisk(inst.LogBuffer, inst.Handler)

	// Initialize registry after logging is configured so warnings are captured
	if err := registryServer.Initialize(context.Background()); err != nil {
		slog.New(inst.Handler).Warn("registry initialization failed", "error", err)
	}

	// Load Go skill plugins after the store walker has populated
	// HandlerLanguage/HandlerPath, and BEFORE the registry server is
	// added to the router — Router.RefreshTools queries the registry's
	// Tools(), which only surfaces Go skills once a *skill.Registry is
	// wired via SetSkillRegistry. The loader reads each Go skill's
	// manifest.json first and enforces the go_version + go_mod_hash
	// guardrails before plugin.Open, so a stale plugin produces an
	// actionable warning rather than the opaque toolchain-mismatch
	// error the plugin package would otherwise return.
	goSkillRegistry := skill.NewRegistry()
	loadGoSkillPlugins(registryStore, goSkillRegistry, slog.New(inst.Handler))
	registryServer.SetSkillRegistry(goSkillRegistry)

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

// telemetrySeedLimit caps the number of pre-restart entries replayed into a
// ring buffer at startup. Leaves room for new live entries and bounds
// startup I/O.
const telemetrySeedLimit = 500

// seedLogsFromDisk replays any existing per-server logs.jsonl into the
// shared in-memory log buffer. Called early in Build (before registry init
// or any other goroutine emits a record) so seeded history precedes live
// records in the ring.
func (b *GatewayBuilder) seedLogsFromDisk(buf *logging.LogBuffer, handler slog.Handler) {
	if b.stack == nil || buf == nil {
		return
	}
	logger := slog.New(handler)
	for i := range b.stack.MCPServers {
		srv := &b.stack.MCPServers[i]
		if srv.Name == "" || !srv.PersistLogs(b.stack) {
			continue
		}
		path := state.TelemetryServerPath(b.stack.Name, srv.Name, "logs")
		if err := buf.SeedFromFile(path, telemetrySeedLimit); err != nil {
			logger.Warn("telemetry: seed logs failed", "server", srv.Name, "path", path, "error", err)
		}
	}
}

// seedTracesFromDisk replays any existing per-server traces.jsonl into the
// shared tracing buffer. Called immediately after tracingProvider.Init —
// before the trace file exporter is registered and before registry init —
// so seeded traces don't interleave with live spans.
func (b *GatewayBuilder) seedTracesFromDisk(handler slog.Handler) {
	if b.stack == nil || b.tracingProvider == nil || b.tracingProvider.Buffer == nil {
		return
	}
	logger := slog.New(handler)
	for i := range b.stack.MCPServers {
		srv := &b.stack.MCPServers[i]
		if srv.Name == "" || !srv.PersistTraces(b.stack) {
			continue
		}
		path := state.TelemetryServerPath(b.stack.Name, srv.Name, "traces")
		if err := b.tracingProvider.Buffer.SeedFromFile(path, telemetrySeedLimit); err != nil {
			logger.Warn("telemetry: seed traces failed", "server", srv.Name, "path", path, "error", err)
		}
	}
}

// seedMetricsFromDisk replays any existing per-server metrics.jsonl into the
// accumulator's per-server totals AND the flusher's previous-snapshot map.
// Called from buildAPIServer after the flusher is constructed but before any
// Build phase can drive live tool calls — so seeded counters precede live
// observations and the first post-restart flush computes a real diff against
// the seeded baseline.
func (b *GatewayBuilder) seedMetricsFromDisk(handler slog.Handler) {
	if b.stack == nil || b.telemetry == nil || b.telemetry.metricsFlusher == nil {
		return
	}
	logger := slog.New(handler)
	for i := range b.stack.MCPServers {
		srv := &b.stack.MCPServers[i]
		if srv.Name == "" || !srv.PersistMetrics(b.stack) {
			continue
		}
		path := state.TelemetryServerPath(b.stack.Name, srv.Name, "metrics")
		if err := b.telemetry.metricsFlusher.SeedFromFile(path, telemetrySeedLimit); err != nil {
			logger.Warn("telemetry: seed metrics failed", "server", srv.Name, "path", path, "error", err)
		}
	}
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

	// Start the Agent IDE watcher (when wired). Its lifetime is bound to
	// ctx so it exits cleanly on shutdown; errors are non-fatal so a
	// failed watcher does not crash the daemon.
	if b.agentDevWatcher != nil {
		go func(w *watcher.Watcher) {
			if err := w.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				slog.New(bufferHandler).Warn("agent IDE watcher exited", "error", err)
			}
		}(b.agentDevWatcher)
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

	// Start the telemetry metrics flusher (no-op when no server opts in).
	if b.telemetry != nil && b.telemetry.metricsFlusher != nil {
		b.telemetry.metricsFlusher.Start()
	}

	// Set up hot reload
	b.setupHotReload(ctx, inst, registrar, bufferHandler, verbose)

	if verbose {
		b.printEndpoints(inst)
	}

	// Wait for shutdown signal or server error
	return b.waitForShutdown(ctx, inst, bufferHandler, serverErr, verbose)
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

	// Wrap the existing chain with the telemetry log router. The router is
	// always installed; per-server file fan-out only kicks in when
	// AddServer is called for a given component. This keeps the install
	// cost zero for stacks that don't opt in.
	router := telemetry.NewLogRouter(redactHandler)
	if b.telemetry == nil {
		b.telemetry = &telemetryWiring{}
	}
	b.telemetry.logRouter = router
	router.SetSelfLogger(slog.New(redactHandler))

	return logBuffer, router, nil
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

	// Resolve the agent runtime aggregate the gateway holds (built in
	// Phase 1c above). Every API handler that needs runtime components
	// reads them from this aggregate rather than from per-field setters.
	rt, _ := gateway.AgentRuntime().(*agentruntime.Runtime)

	if provider := buildPlaygroundProvider(b.vaultStore); provider != nil {
		server.SetPlaygroundProvider(provider)
		// Wire the same provider into the runtime so the TS dispatcher's
		// llm() binding sees it. The dispatcher's bindings closure
		// re-reads ChatModel on every call, so plugging it in here
		// retroactively makes earlier-registered TS skills work.
		if rt != nil {
			rt.SetChatModel(provider)
		}
	}

	// Agent IDE dev server (Phase F). When a project root is configured
	// (or the default registry root exists), construct the watcher +
	// devserver and wire them onto the runtime aggregate so the API
	// server's /api/agent/dev/* handlers stop 503'ing. SetDevServer must
	// run before SetAgentRuntime so the read accessor sees a non-nil
	// devServer the first time a request lands.
	if rt != nil {
		b.wireAgentDevServer(rt)
	}

	// Agent runtime persistence (Phase E). The runtime aggregate already
	// owns the JSONL run store and approval registry — wire it into the
	// API server so /api/agent/runs/* handlers read from one source of
	// truth and can never get out of step with the dispatcher's view.
	if rt != nil {
		server.SetAgentRuntime(rt)
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

	// Telemetry persistence: wire the metrics flusher. Adding per-server
	// outputs happens in wireTelemetryPersistence after the tracing
	// provider is initialized below — keeps all opt-in writers grouped.
	if b.telemetry == nil {
		b.telemetry = &telemetryWiring{}
	}
	b.telemetry.metricsFlusher = telemetry.NewMetricsFlusher(accumulator, 0)
	if handler != nil {
		b.telemetry.metricsFlusher.SetLogger(slog.New(handler))
	}

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

	// Seed the trace buffer from disk before live spans land in it. Done
	// here (not later in Build) because registry init and other Build
	// stages can begin emitting spans the moment the provider is live.
	b.seedTracesFromDisk(handler)

	// Telemetry persistence: register the trace file client as an extra
	// span exporter alongside the in-memory buffer and the optional OTLP
	// exporter. The exporter is started during otlptrace.NewUnstarted ->
	// exporter.Start; failure is logged and the in-memory tracing path
	// continues unaffected.
	if tracingCfg.Enabled && b.telemetry != nil {
		client := telemetry.NewTracesFileClient()
		if handler != nil {
			client.SetLogger(slog.New(handler))
		}
		exporter, err := otlptrace.New(context.Background(), client)
		if err != nil {
			slog.New(handler).Warn("telemetry trace exporter init failed; per-server traces.jsonl disabled",
				"error", err)
		} else {
			b.telemetry.tracesClient = client
			b.telemetry.tracesExporter = exporter
			tracingProvider.RegisterExporter(exporter)
		}
	}

	// Apply the current stack's per-server persistence settings.
	b.applyTelemetryConfig(server, handler)

	// Seed the metrics accumulator from disk after writers are registered
	// (so per-server directories exist) and before the flusher goroutine
	// starts in Run. The seed updates both the accumulator's per-server
	// totals and the flusher's prev map atomically — so the first post-
	// restart flush emits a real diff against the seeded baseline rather
	// than a fresh reset.
	b.seedMetricsFromDisk(handler)

	return server, nil
}

// wireAgentDevServer resolves the dev-root, constructs the watcher +
// devserver, and installs them on the runtime aggregate. Each step is
// best-effort: a missing root, an unreadable directory, or a fsnotify
// failure logs a warning and leaves rt.DevServer() nil so the IDE
// handlers fall back to 503 with the existing message. The watcher
// goroutine is started later in Run() so it binds to the daemon's
// lifecycle context.
func (b *GatewayBuilder) wireAgentDevServer(rt *agentruntime.Runtime) {
	root := b.resolveAgentDevRoot()
	if root == "" {
		return
	}

	logger := slog.Default()

	w, err := watcher.New(root)
	if err != nil {
		logger.Warn("agent IDE dev server disabled", "root", root, "error", err)
		return
	}

	srv, err := devserver.NewServer(root, w)
	if err != nil {
		logger.Warn("agent IDE dev server disabled", "root", root, "error", err)
		return
	}

	rt.SetDevServer(srv)
	b.agentDevWatcher = w
	logger.Info("agent IDE wired", "root", root)
}

// resolveAgentDevRoot returns the effective dev-root for the Agent IDE,
// preferring the explicit flag and falling back to ~/.gridctl/registry/skills
// when that directory exists. Returns "" when no root resolves — callers
// treat that as "leave the IDE backend unwired".
func (b *GatewayBuilder) resolveAgentDevRoot() string {
	if b.config.AgentDevRoot != "" {
		return b.config.AgentDevRoot
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	candidate := filepath.Join(home, ".gridctl", "registry", "skills")
	info, err := os.Stat(candidate)
	if err != nil || !info.IsDir() {
		return ""
	}
	return candidate
}

// applyTelemetryConfig walks the stack's MCP servers and registers per-
// server file writers for every signal a server opts into. Idempotent:
// re-running with a changed stack adds new writers and removes ones that
// flipped to off. Used both at initial Build time and from the hot-reload
// callback so a YAML change takes effect without restarting the daemon.
func (b *GatewayBuilder) applyTelemetryConfig(apiServer *api.Server, handler slog.Handler) {
	_ = apiServer // reserved for Phase 3 (inventory hookup); keeps callers stable
	if b.telemetry == nil || b.stack == nil {
		return
	}

	logger := slog.New(handler)
	stack := b.stack

	// Compute desired set per signal.
	wantLogs := map[string]bool{}
	wantMetrics := map[string]bool{}
	wantTraces := map[string]bool{}
	for i := range stack.MCPServers {
		srv := &stack.MCPServers[i]
		if srv.Name == "" {
			continue
		}
		if srv.PersistLogs(stack) {
			wantLogs[srv.Name] = true
		}
		if srv.PersistMetrics(stack) {
			wantMetrics[srv.Name] = true
		}
		if srv.PersistTraces(stack) {
			wantTraces[srv.Name] = true
		}
	}

	if len(wantLogs)+len(wantMetrics)+len(wantTraces) == 0 {
		// Nothing to persist; ensure any previously-registered writers
		// are torn down (handles hot-reload "off").
		if b.telemetry.logRouter != nil {
			for _, n := range b.telemetry.logRouter.ConfiguredServers() {
				b.telemetry.logRouter.RemoveServer(n)
			}
		}
		if b.telemetry.metricsFlusher != nil {
			for _, n := range b.telemetry.metricsFlusher.ConfiguredServers() {
				b.telemetry.metricsFlusher.RemoveServer(n)
			}
		}
		if b.telemetry.tracesClient != nil {
			for _, n := range b.telemetry.tracesClient.ConfiguredServers() {
				b.telemetry.tracesClient.RemoveServer(n)
			}
		}
		return
	}

	opts := telemetryRotationOpts(stack)

	// Logs.
	if router := b.telemetry.logRouter; router != nil {
		current := stringSet(router.ConfiguredServers())
		for name := range wantLogs {
			if err := state.EnsureTelemetryServerDir(stack.Name, name); err != nil {
				logger.Warn("telemetry: cannot ensure dir", "server", name, "error", err)
				continue
			}
			path := state.TelemetryServerPath(stack.Name, name, "logs")
			if err := router.AddServer(name, path, opts); err != nil {
				logger.Warn("telemetry: log writer install failed", "server", name, "path", path, "error", err)
			}
		}
		for name := range current {
			if !wantLogs[name] {
				router.RemoveServer(name)
			}
		}
	}

	// Metrics.
	if flusher := b.telemetry.metricsFlusher; flusher != nil {
		current := stringSet(flusher.ConfiguredServers())
		for name := range wantMetrics {
			if err := state.EnsureTelemetryServerDir(stack.Name, name); err != nil {
				logger.Warn("telemetry: cannot ensure dir", "server", name, "error", err)
				continue
			}
			path := state.TelemetryServerPath(stack.Name, name, "metrics")
			if err := flusher.AddServer(name, path, opts); err != nil {
				logger.Warn("telemetry: metrics writer install failed", "server", name, "path", path, "error", err)
			}
		}
		for name := range current {
			if !wantMetrics[name] {
				flusher.RemoveServer(name)
			}
		}
	}

	// Traces.
	if tc := b.telemetry.tracesClient; tc != nil {
		current := stringSet(tc.ConfiguredServers())
		for name := range wantTraces {
			if err := state.EnsureTelemetryServerDir(stack.Name, name); err != nil {
				logger.Warn("telemetry: cannot ensure dir", "server", name, "error", err)
				continue
			}
			path := state.TelemetryServerPath(stack.Name, name, "traces")
			if err := tc.AddServer(name, path, opts); err != nil {
				logger.Warn("telemetry: traces writer install failed", "server", name, "path", path, "error", err)
			}
		}
		for name := range current {
			if !wantTraces[name] {
				tc.RemoveServer(name)
			}
		}
	}
}

// telemetryRotationOpts pulls retention from stack config or falls back to
// the lumberjack defaults. Phase 1's SetDefaults already fills retention
// when telemetry is set, so the zero-value fallbacks are belt-and-braces.
func telemetryRotationOpts(stack *config.Stack) telemetry.LogOpts {
	if stack == nil || stack.Telemetry == nil || stack.Telemetry.Retention == nil {
		return telemetry.LogOpts{}
	}
	r := stack.Telemetry.Retention
	return telemetry.LogOpts{
		MaxSizeMB:  r.MaxSizeMB,
		MaxBackups: r.MaxBackups,
		MaxAgeDays: r.MaxAgeDays,
	}
}

func stringSet(in []string) map[string]bool {
	out := make(map[string]bool, len(in))
	for _, s := range in {
		out[s] = true
	}
	return out
}

// tokenizerName returns the configured tokenizer mode, defaulting to "embedded".
func (b *GatewayBuilder) tokenizerName() string {
	if b.stack.Gateway != nil && b.stack.Gateway.Tokenizer != "" {
		return b.stack.Gateway.Tokenizer
	}
	return "embedded"
}

// makeDispatcherBindings returns a sandbox.BindingsProvider that builds
// per-call collaborators from the live gateway state. Closing over gw
// (rather than snapshotting Bindings at build time) means later changes
// — a freshly-set ChatModel after vault auth completes, an autoscaled
// MCP server adding new tools — show up in subsequent dispatches
// without a rewire. The ToolCaller is constructed once at wire time
// (it depends only on the gateway pointer, which is stable) so a
// per-call construction failure is impossible by design.
//
// SkillBody and SkillName are resolved per-call from the registry
// store so the JS sandbox's `skill.body` / `skill.name` globals match
// what a Go skill would receive via ctx.SkillBody() / ctx.SkillName()
// for the same skill — the parity invariant the hybrid pattern
// depends on. A store lookup miss reads as empty body, not a hard
// error: bindings should degrade quietly rather than fault the
// invocation when the registry walker has dropped the skill (a race
// during hot-reload, or a programmatic registration that bypassed the
// on-disk store).
//
// Approver is left nil so the sandbox's auto-approve stub stays in
// effect for the dispatcher path. Orchestrator-driven runs construct a
// real compose.Gate (which needs a per-run recorder) and supply it via
// their own bindings.
func makeDispatcherBindings(gw *mcp.Gateway, store *registry.Store, skillCaller sandbox.SkillCaller, toolCaller agent.ToolCaller) sandbox.BindingsProvider {
	return func(_ context.Context, skillName string) sandbox.Bindings {
		b := sandbox.Bindings{
			AllowedTools: gw.Router().AggregatedTools(),
			SkillCaller:  skillCaller,
			ToolCaller:   toolCaller,
			SkillName:    skillName,
		}
		if store != nil && skillName != "" {
			if sk, err := store.GetSkill(skillName); err == nil && sk != nil {
				b.SkillBody = sk.Body
			}
		}
		if rt, ok := gw.AgentRuntime().(*agentruntime.Runtime); ok && rt != nil {
			b.ChatModel = rt.ChatModel()
		}
		return b
	}
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
	// After a successful reload, refresh per-server telemetry writers so a
	// YAML-toggled persist setting takes effect without restart. The
	// callback fires under reload.Handler.mu — keep it allocation-light.
	reloadHandler.SetOnConfigApplied(func(newCfg *config.Stack) {
		b.stack = newCfg
		b.applyTelemetryConfig(inst.APIServer, handler)
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

// waitForShutdown blocks until ctx is canceled (signal-driven) or the server
// errors, then cleans up. Listening on ctx.Done() rather than a local signal
// channel ensures all ctx-bound goroutines in the gateway see the same
// cancellation and exit cleanly.
func (b *GatewayBuilder) waitForShutdown(ctx context.Context, inst *GatewayInstance, handler slog.Handler, serverErr <-chan error, verbose bool) error {
	select {
	case <-ctx.Done():
		logger := slog.New(handler)
		logger.Info("received shutdown signal")

		if verbose {
			fmt.Println("\nShutting down...")
		}

		// Close API server resources: broadcasts SSE close event while
		// HTTP connections are still alive, then closes gateway clients.
		inst.APIServer.Close()

		// Graceful HTTP shutdown with timeout. Parent is Background, not
		// ctx — ctx is already canceled at this point, so a child of it
		// would expire immediately.
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer shutdownCancel()

		if err := inst.HTTPServer.Shutdown(shutdownCtx); err != nil {
			logger.Error("HTTP server shutdown error", "error", err)
		}

		if b.telemetry != nil && b.telemetry.metricsFlusher != nil {
			b.telemetry.metricsFlusher.Stop()
		}

		if b.tracingProvider != nil {
			if err := b.tracingProvider.Shutdown(shutdownCtx); err != nil {
				logger.Error("tracing shutdown error", "error", err)
			}
		}

		if b.telemetry != nil && b.telemetry.logRouter != nil {
			b.telemetry.logRouter.Close()
		}
	case err := <-serverErr:
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}
