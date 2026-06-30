package controller

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"

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

	// modelAttribution holds the client and server model mappings used to
	// price tool calls. Stored behind an atomic pointer so the hot-reload
	// hook can swap both mappings together without racing in-flight
	// observations; the observer's resolver closure reads through it on
	// every call.
	modelAttribution atomic.Pointer[modelAttribution]
}

// modelAttribution is the resolved cost-attribution state derived from a
// stack: clients maps normalized client IDs to their declared models
// (stack.yaml client_models), servers maps server names to their effective
// models (per-server model: with gateway default_model folded in). The
// resolver consults clients first — the model is a property of the calling
// client's session; the server tier is the coarser fallback.
// declaredServers and defaultModel carry the raw (un-folded) declarations so
// the API can show provenance: which servers set their own model and what
// the gateway default is.
type modelAttribution struct {
	clients         map[string]string
	servers         map[string]string
	declaredServers map[string]string
	defaultModel    string
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

// resolveSchemaPinning returns the effective gateway schema-pinning settings,
// applying the documented defaults (enabled, "warn") when the stack omits the
// security block or individual fields. Enabled is a *bool so an omitted
// `enabled:` inherits the default-on behavior rather than YAML's zero value.
func resolveSchemaPinning(stack *config.Stack) (enabled bool, action string) {
	enabled, action = true, "warn"
	if stack == nil || stack.Gateway == nil || stack.Gateway.Security == nil {
		return enabled, action
	}
	sp := stack.Gateway.Security.SchemaPinning
	if sp == nil {
		return enabled, action
	}
	if sp.Enabled != nil {
		enabled = *sp.Enabled
	}
	if sp.Action == "block" {
		action = "block"
	}
	return enabled, action
}

// installSchemaPinning shares a single pin store between the API server (for
// read-only inspection at /api/pins) and the gateway's TOFU verifier (for drift
// detection and the warn/block policy), so the UI reflects exactly what the
// gateway enforces. It is a no-op when no store is provided or when pinning is
// disabled for the stack, leaving both halves unset.
func installSchemaPinning(gateway *mcp.Gateway, server *api.Server, stack *config.Stack, ps *pins.PinStore) {
	if ps == nil {
		return
	}
	enabled, action := resolveSchemaPinning(stack)
	if !enabled {
		return
	}
	server.SetPinStore(ps)
	gateway.SetSchemaVerifier(pins.NewGatewayAdapter(ps), action)
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

	// Phase 1a4: Install the per-client access policy (nil when no clients:
	// block is configured, preserving legacy "everyone sees everything").
	inst.Gateway.SetClientAccessPolicy(mcp.NewClientAccessPolicy(clientAccessSpec(b.stack)))

	// Phase 1b: Create registry server (internal MCP server)
	regDir := filepath.Join(state.BaseDir(), "registry")
	if b.registryDir != "" {
		regDir = b.registryDir
	}
	registryStore := registry.NewStore(regDir)
	registryServer := registry.New(registryStore)
	inst.RegistryServer = registryServer

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

	// Seed global prompt (skill) usage from the reserved namespace, persisted
	// off the stack-global metrics toggle (the registry is not a stack server).
	if b.stack.Telemetry != nil && b.stack.Telemetry.Persist.Metrics {
		ppath := state.TelemetryServerPath(b.stack.Name, telemetry.PromptUsageNamespace, "metrics")
		if err := b.telemetry.metricsFlusher.SeedPromptUsageFromFile(ppath, telemetrySeedLimit); err != nil {
			logger.Warn("telemetry: seed prompt usage failed", "path", ppath, "error", err)
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

	installSchemaPinning(gateway, server, b.stack, b.pinStore)

	// Wire token usage metrics
	counter, err := b.buildTokenCounter()
	if err != nil {
		return nil, err
	}
	accumulator := metrics.NewAccumulator(10000)
	observer := metrics.NewObserver(counter, accumulator)
	b.wireModelAttribution(observer, server)
	gateway.SetToolCallObserver(observer)
	gateway.SetPromptGetObserver(observer)
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

// applyTelemetryConfig walks the stack's MCP servers and registers per-
// clientAccessSpec translates the stack's optional `clients:` block into the
// config-agnostic spec the gateway consumes. Returns nil when no block is
// configured, which the gateway treats as "every client sees every tool".
func clientAccessSpec(stack *config.Stack) *mcp.ClientAccessSpec {
	if stack == nil || stack.Clients == nil {
		return nil
	}
	spec := &mcp.ClientAccessSpec{
		Default:  stack.Clients.Default,
		Profiles: make(map[string]mcp.ClientProfileSpec, len(stack.Clients.Profiles)),
	}
	for name, profile := range stack.Clients.Profiles {
		spec.Profiles[name] = mcp.ClientProfileSpec{
			Aliases: profile.Aliases,
			Servers: profile.Servers,
			Tools:   profile.Tools,
		}
	}
	return spec
}

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

	// Prompt (skill) usage persists globally off the stack-global metrics
	// toggle, independent of per-server opt-in: the skills registry is not a
	// stack.MCPServers entry, so it has no per-server PersistMetrics switch.
	// Run this before the early-return below so a stack that flips metrics off
	// still tears the writer down on hot-reload.
	if flusher := b.telemetry.metricsFlusher; flusher != nil {
		if stack.Telemetry != nil && stack.Telemetry.Persist.Metrics {
			if err := state.EnsureTelemetryServerDir(stack.Name, telemetry.PromptUsageNamespace); err != nil {
				logger.Warn("telemetry: cannot ensure prompt-usage dir", "error", err)
			} else {
				path := state.TelemetryServerPath(stack.Name, telemetry.PromptUsageNamespace, "metrics")
				if err := flusher.SetPromptUsageWriter(path, telemetryRotationOpts(stack)); err != nil {
					logger.Warn("telemetry: prompt-usage writer install failed", "path", path, "error", err)
				}
			}
		} else {
			flusher.RemovePromptUsageWriter()
		}
	}

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

// wireModelAttribution installs the cost-attribution model resolver on the
// observer and exposes the underlying mappings to the API server (for the
// optimize model stats, the /api/status cost_attribution flag, and the
// client_models exposure). The resolver is always installed: empty mappings
// resolve every call to "", which keeps the observer's cost path inert
// exactly as if no resolver were set, while letting a hot reload activate
// attribution later without an unsynchronized SetModelResolver swap racing
// in-flight observations.
//
// Resolution precedence within the resolver: the calling client's declared
// model (client_models) wins over the server's effective model. An empty
// clientID — anonymous sessions and the legacy ObserveToolCall path — skips
// the client tier and lands on the server tier, preserving pre-client
// behavior exactly.
func (b *GatewayBuilder) wireModelAttribution(observer *metrics.Observer, apiServer *api.Server) {
	b.refreshModelAttribution(b.stack)
	observer.SetModelResolver(func(serverName, clientID string) string {
		attribution := b.modelAttribution.Load()
		if clientID != "" {
			if model := attribution.clients[clientID]; model != "" {
				return model
			}
		}
		return attribution.servers[serverName]
	})
	apiServer.SetModelAttribution(func() map[string]string {
		return b.modelAttribution.Load().servers
	})
	apiServer.SetClientModelAttribution(func() map[string]string {
		return b.modelAttribution.Load().clients
	})
	apiServer.SetDeclaredServerModels(func() map[string]string {
		return b.modelAttribution.Load().declaredServers
	})
	apiServer.SetDefaultModel(func() string {
		return b.modelAttribution.Load().defaultModel
	})
}

// refreshModelAttribution re-resolves the client and server model mappings
// from the given stack. Called at build time and from the hot-reload hook so
// `client_models:`, `model:`, and `default_model:` edits take effect on the
// next observed call.
func (b *GatewayBuilder) refreshModelAttribution(cfg *config.Stack) {
	b.modelAttribution.Store(&modelAttribution{
		clients:         cfg.ClientModelAttribution(),
		servers:         cfg.ModelAttribution(),
		declaredServers: declaredServerModels(cfg),
		defaultModel:    gatewayDefaultModel(cfg),
	})
}

// declaredServerModels collects the raw per-server model: declarations
// (no gateway default folded in). Returns nil when nothing is declared.
func declaredServerModels(cfg *config.Stack) map[string]string {
	if cfg == nil {
		return nil
	}
	var out map[string]string
	for _, server := range cfg.MCPServers {
		if server.Model == "" {
			continue
		}
		if out == nil {
			out = make(map[string]string, len(cfg.MCPServers))
		}
		out[server.Name] = server.Model
	}
	return out
}

// gatewayDefaultModel returns gateway.default_model, or "" when unset.
func gatewayDefaultModel(cfg *config.Stack) string {
	if cfg == nil || cfg.Gateway == nil {
		return ""
	}
	return cfg.Gateway.DefaultModel
}

// buildTracingConfig extracts tracing config from gateway config with defaults.
func buildTracingConfig(gw *config.GatewayConfig) *tracing.Config {
	cfg := tracing.DefaultConfig()
	if gw == nil || gw.Tracing == nil {
		return cfg
	}
	t := gw.Tracing
	if t.Enabled != nil {
		cfg.Enabled = *t.Enabled
	}
	if t.Sampling > 0 {
		cfg.Sampling = t.Sampling
	}
	if t.Retention != "" {
		cfg.Retention = t.Retention
	}
	cfg.Export = t.Export
	cfg.Endpoint = t.Endpoint
	if t.MaxTraces > 0 {
		cfg.MaxTraces = t.MaxTraces
	}
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
		// Re-resolve the per-client access policy from the reloaded config so a
		// `clients:` change takes effect on the next tools/list and tools/call.
		inst.Gateway.SetClientAccessPolicy(mcp.NewClientAccessPolicy(clientAccessSpec(newCfg)))
		// Re-resolve cost attribution so `client_models:`, `model:`, and
		// `default_model:` edits price subsequent calls without a restart.
		b.refreshModelAttribution(newCfg)
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
			refreshRegistry(watchCtx, inst, slog.New(handler))
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

	// Watch the registry skills directory so skills added to disk out-of-band
	// (hand-authored, or written by `gridctl skill add/update/remove` while the
	// daemon runs) are reflected in the running gateway — activation, the UI
	// listing, and the MCP prompt set — without a restart. This is independent
	// of --watch, which only governs the stack.yaml watcher.
	if inst.RegistryServer != nil {
		regLogger := slog.New(handler)
		skillsDir := filepath.Join(inst.RegistryServer.Store().Dir(), "skills")
		// Create the directory synchronously so the watcher arms on the tight
		// skills subtree rather than a busier ancestor like ~/.gridctl. The
		// watcher itself is write-free and tolerates a missing directory, so a
		// failure here is non-fatal.
		if err := os.MkdirAll(skillsDir, 0o755); err != nil {
			regLogger.Warn("could not create registry skills directory for watching", "path", skillsDir, "error", err)
		}
		regWatcher := reload.NewDirWatcher(skillsDir, func() error {
			refreshRegistry(ctx, inst, regLogger)
			return nil
		})
		regWatcher.SetLogger(regLogger)
		go func() {
			if err := regWatcher.Watch(ctx); err != nil && err != context.Canceled {
				regLogger.Error("registry watcher error", "error", err)
			}
		}()
	}

	if b.config.Watch {
		startWatcher(b.stackPath)

		if verbose {
			fmt.Printf("\nFile watcher enabled for: %s\n", b.stackPath)
		}
	}
}

// refreshRegistry reloads the registry store from disk and re-syncs the
// gateway router with the result. It is shared by the stack-reload callback
// and the registry directory watcher so the two paths cannot drift: the
// store is reloaded, the registry client is added or removed from the router
// depending on whether any skills remain, and the router's tool set is
// refreshed. A failed reload is logged and tolerated rather than fatal.
func refreshRegistry(ctx context.Context, inst *GatewayInstance, logger *slog.Logger) {
	if inst.RegistryServer == nil {
		return
	}
	if err := inst.RegistryServer.RefreshTools(ctx); err != nil {
		logger.Warn("registry refresh failed", "error", err)
	}
	if inst.RegistryServer.HasContent() {
		inst.Gateway.Router().AddClient(inst.RegistryServer)
	} else {
		inst.Gateway.Router().RemoveClient("registry")
	}
	inst.Gateway.Router().RefreshTools()
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
