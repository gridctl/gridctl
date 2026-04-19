package probe

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/logging"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/runtime"
)

// Default probe timeout. Matches the Phase 2 spec (10s) and is overridable via
// the config's ReadyTimeout field following the same semantics the gateway
// uses for HTTP readiness waits.
const defaultTimeout = 10 * time.Second

// Error codes surfaced to clients. Kept as constants so the frontend can map
// them to user-facing copy deterministically.
const (
	CodeProbeTimeout         = "probe_timeout"
	CodeInitializeFailed     = "initialize_failed"
	CodeToolsListFailed      = "tools_list_failed"
	CodeUnsupportedTransport = "unsupported_transport"
	CodeInvalidConfig        = "invalid_config"
	CodeRateLimited          = "rate_limited"
	CodeInternal             = "internal_error"
)

// Error is a structured probe failure. Unlike a plain error, it carries a
// stable code for the UI and an optional hint. Secrets in Message / Hint are
// scrubbed by the handler before the response is serialized.
type Error struct {
	Code    string
	Message string
	Hint    string
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// newErr is a small helper so callsites read well.
func newErr(code, msg, hint string) *Error {
	return &Error{Code: code, Message: msg, Hint: hint}
}

// Result is what a probe returns on success.
type Result struct {
	Tools  []mcp.Tool
	Cached bool
}

// Spawner abstracts the "start a workload, give me its endpoint, and let me
// stop+remove it" dance. The probe-path container/stdio spawning needs this
// indirection so handler tests can stub the transport layer without touching
// docker.
type Spawner interface {
	// SpawnContainer starts a container for the given server config in
	// ephemeral probe mode. On return, the caller is responsible for invoking
	// the returned release func exactly once — it stops and removes the
	// workload. Release must be safe to call even if SpawnContainer returns
	// with an error (implementations should return a no-op release in that
	// case).
	SpawnContainer(ctx context.Context, cfg config.MCPServer) (endpoint string, containerID string, release func(ctx context.Context), err error)
}

// Prober enumerates an MCP server's tools without registering it with the
// gateway. It owns the cache and spawner wiring; the HTTP handler is a thin
// shell around Probe().
type Prober struct {
	cache   *Cache
	spawner Spawner
	logger  *slog.Logger

	// clientFactory is overridable in tests so handler tests can inject a
	// stubbed transport without standing up real HTTP servers.
	clientFactory ClientFactory
}

// ClientFactory builds an MCP client for a given transport. The default
// implementation defers to the standard pkg/mcp constructors; tests override
// it to inject stubs.
type ClientFactory interface {
	NewHTTP(name, endpoint string) mcp.AgentClient
	NewProcess(name string, command []string, workDir string, env map[string]string) mcp.AgentClient
}

// NewProber wires up a prober. A nil cache becomes a new one with DefaultTTL.
// A nil spawner means container/stdio transports return an unsupported_transport
// error — handy for tests and for the stackless/daemon-less build.
func NewProber(cache *Cache, spawner Spawner) *Prober {
	if cache == nil {
		cache = NewCache(DefaultTTL)
	}
	return &Prober{
		cache:         cache,
		spawner:       spawner,
		logger:        logging.NewDiscardLogger(),
		clientFactory: defaultClientFactory{},
	}
}

// SetLogger installs a structured logger. Nil is ignored.
func (p *Prober) SetLogger(logger *slog.Logger) {
	if logger != nil {
		p.logger = logger
	}
}

// SetClientFactory overrides the MCP client constructors. Primarily for tests.
func (p *Prober) SetClientFactory(f ClientFactory) {
	if f != nil {
		p.clientFactory = f
	}
}

// Probe validates the config, short-circuits on cache hits, and otherwise
// spawns / connects to the server long enough to run initialize + tools/list,
// then tears down. It is safe to call concurrently; the caller is responsible
// for enforcing concurrency caps.
func (p *Prober) Probe(ctx context.Context, cfg config.MCPServer) (Result, *Error) {
	if err := validate(cfg); err != nil {
		return Result{}, err
	}

	if cfg.IsSSH() {
		return Result{}, newErr(CodeUnsupportedTransport,
			"Probe not supported for ssh servers in this release.",
			"Add tool names manually in the Advanced section — they will be enforced by the gateway once the stack is deployed.")
	}
	if cfg.IsOpenAPI() {
		return Result{}, newErr(CodeUnsupportedTransport,
			"Probe not supported for openapi servers.",
			"Use the Operations Filter (openapi.operations.include / exclude) to curate tools for OpenAPI-backed servers.")
	}

	key := Key(cfg)
	if entry, ok := p.cache.Get(key); ok {
		return Result{Tools: entry.Tools, Cached: true}, nil
	}

	timeout := resolveTimeout(cfg)
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result, err := p.runProbe(probeCtx, cfg)
	if err != nil {
		// A deadline exceeded from the probe context is surfaced as a
		// distinct, user-friendly code so the UI can render "Probe timed out"
		// rather than a raw transport error.
		if errors.Is(probeCtx.Err(), context.DeadlineExceeded) {
			return Result{}, newErr(CodeProbeTimeout,
				fmt.Sprintf("Probe timed out after %s. The server may need more time to start or may require additional configuration.", timeout),
				"Try increasing ready_timeout on the server, or verify that required environment variables are set.")
		}
		return Result{}, err
	}

	// Only successful probes land in the cache. A transient failure should not
	// poison subsequent reads.
	p.cache.Put(key, result.Tools)
	return Result{Tools: result.Tools, Cached: false}, nil
}

// runProbe dispatches to the right transport-specific probe path. All paths
// run under the caller's timeout context and are responsible for deterministic
// cleanup.
func (p *Prober) runProbe(ctx context.Context, cfg config.MCPServer) (Result, *Error) {
	switch {
	case cfg.IsExternal():
		return p.probeExternal(ctx, cfg)
	case cfg.IsLocalProcess():
		return p.probeLocalProcess(ctx, cfg)
	default:
		// Container-backed HTTP/SSE or stdio. Both require a live workload
		// spawned ephemerally for the duration of the probe.
		return p.probeContainer(ctx, cfg)
	}
}

func (p *Prober) probeExternal(ctx context.Context, cfg config.MCPServer) (Result, *Error) {
	client := p.clientFactory.NewHTTP(probeClientName(cfg), cfg.URL)
	return runClient(ctx, client)
}

func (p *Prober) probeLocalProcess(ctx context.Context, cfg config.MCPServer) (Result, *Error) {
	workDir := ""
	if cwd := filepath.Dir("."); cwd != "" {
		workDir = cwd
	}
	client := p.clientFactory.NewProcess(probeClientName(cfg), cfg.Command, workDir, cfg.Env)
	return runClient(ctx, client)
}

func (p *Prober) probeContainer(ctx context.Context, cfg config.MCPServer) (Result, *Error) {
	if strings.ToLower(cfg.Transport) == "stdio" {
		return Result{}, newErr(CodeUnsupportedTransport,
			"Probe is not yet supported for container stdio transport.",
			"Enter tool names manually in the Advanced section, or switch the container to http / sse transport for probe support.")
	}
	if p.spawner == nil {
		return Result{}, newErr(CodeUnsupportedTransport,
			"Container probe is not available in this build.",
			"Enter tool names manually in the Advanced section — they will be enforced by the gateway once the stack is deployed.")
	}

	endpoint, containerID, release, err := p.spawner.SpawnContainer(ctx, cfg)
	// release must be called even on spawn failure — per the Spawner contract
	// it is a no-op in that case, but callers never need to know which.
	if release != nil {
		defer func() {
			// Give teardown its own short budget independent of the probe ctx
			// so a timed-out probe still gets cleaned up.
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			release(cleanupCtx)
		}()
	}
	if err != nil {
		return Result{}, newErr(CodeInitializeFailed,
			fmt.Sprintf("Failed to start container: %v", err),
			"Check that the image is reachable and any required build-args / env vars are set.")
	}
	p.logger.Debug("probe container spawned", "container_id", containerID, "endpoint", endpoint)

	client := p.clientFactory.NewHTTP(probeClientName(cfg), endpoint)
	return runClient(ctx, client)
}

// runClient executes the Initialize + RefreshTools handshake and returns the
// tool list. It collapses transport-specific failures into structured probe
// errors with the stable codes the frontend understands. This path intentionally
// does not log raw error strings because they may contain env values.
func runClient(ctx context.Context, client mcp.AgentClient) (Result, *Error) {
	if err := client.Initialize(ctx); err != nil {
		closeClient(client)
		return Result{}, newErr(CodeInitializeFailed,
			fmt.Sprintf("Server failed to initialize: %v", err),
			"")
	}
	if err := client.RefreshTools(ctx); err != nil {
		closeClient(client)
		return Result{}, newErr(CodeToolsListFailed,
			fmt.Sprintf("Server failed to list tools: %v", err),
			"")
	}
	tools := client.Tools()
	closeClient(client)
	return Result{Tools: tools}, nil
}

// closeClient tries Close on clients that support it. Stdio/process clients
// implement io.Closer for this purpose; HTTP does not, and that's fine — there
// is no persistent connection to tear down for external URL probes.
func closeClient(client mcp.AgentClient) {
	type closer interface{ Close() error }
	if c, ok := client.(closer); ok {
		_ = c.Close()
	}
}

func validate(cfg config.MCPServer) *Error {
	if cfg.IsSSH() || cfg.IsOpenAPI() {
		// Transport is structurally valid but not supported — caller handles
		// the unsupported_transport response.
		return nil
	}
	if cfg.IsExternal() {
		if strings.TrimSpace(cfg.URL) == "" {
			return newErr(CodeInvalidConfig, "external servers require a url.", "Set the server's url field.")
		}
		return nil
	}
	if cfg.IsLocalProcess() {
		if len(cfg.Command) == 0 {
			return newErr(CodeInvalidConfig, "local process servers require a command.", "Set the server's command field.")
		}
		return nil
	}
	// Container-backed. Either image or source must be set; transport http/sse
	// needs a port, stdio doesn't.
	if cfg.Image == "" && cfg.Source == nil {
		return newErr(CodeInvalidConfig, "container servers require image or source.", "Set one of image or source.")
	}
	transport := strings.ToLower(cfg.Transport)
	if transport == "" {
		transport = "http"
	}
	switch transport {
	case "http", "sse":
		if cfg.Port == 0 {
			return newErr(CodeInvalidConfig, "http/sse container servers require a port.", "Set the server's port field.")
		}
	case "stdio":
		// No extra required fields.
	default:
		return newErr(CodeInvalidConfig, fmt.Sprintf("unknown transport: %q", cfg.Transport), "Valid transports: http, sse, stdio.")
	}
	return nil
}

func resolveTimeout(cfg config.MCPServer) time.Duration {
	if d := cfg.ResolvedReadyTimeout(); d > 0 {
		return d
	}
	return defaultTimeout
}

// probeClientName tags logs from a probe with a stable prefix. The container
// itself uses a different, runtime-assigned name — this is just for logging.
func probeClientName(cfg config.MCPServer) string {
	if cfg.Name != "" {
		return "probe:" + cfg.Name
	}
	return "probe:anonymous"
}

// defaultClientFactory wires the public mcp.NewClient / NewProcessClient
// constructors. Kept as a struct (not a free function) so tests can swap it.
type defaultClientFactory struct{}

func (defaultClientFactory) NewHTTP(name, endpoint string) mcp.AgentClient {
	return mcp.NewClient(name, endpoint)
}

func (defaultClientFactory) NewProcess(name string, command []string, workDir string, env map[string]string) mcp.AgentClient {
	return mcp.NewProcessClient(name, command, workDir, env)
}

// NoopSpawner is a convenience for tests and the stackless build — it always
// refuses, causing container/stdio probes to return unsupported_transport.
type NoopSpawner struct{}

// SpawnContainer always fails with a "not available" error.
func (NoopSpawner) SpawnContainer(ctx context.Context, cfg config.MCPServer) (string, string, func(context.Context), error) {
	return "", "", func(context.Context) {}, errors.New("container probe not available in this build")
}

// RuntimeSpawner wires the WorkloadRuntime into the prober so container /
// stdio probes can spawn real workloads. It is intentionally conservative —
// each probe gets its own uniquely-named workload so cleanup is unambiguous,
// and every code path runs Stop+Remove via the returned release function.
type RuntimeSpawner struct {
	rt     runtime.WorkloadRuntime
	logger *slog.Logger

	// now is overridable in tests to keep generated names deterministic.
	now func() time.Time
}

// NewRuntimeSpawner constructs a RuntimeSpawner. Passing a nil runtime yields
// a spawner whose Spawn always fails — equivalent to NoopSpawner.
func NewRuntimeSpawner(rt runtime.WorkloadRuntime) *RuntimeSpawner {
	return &RuntimeSpawner{
		rt:     rt,
		logger: logging.NewDiscardLogger(),
		now:    time.Now,
	}
}

// SetLogger installs a structured logger.
func (s *RuntimeSpawner) SetLogger(logger *slog.Logger) {
	if logger != nil {
		s.logger = logger
	}
}

// SpawnContainer implements the Spawner interface. Stdio transport is routed
// through an error today — full stdio probe requires attaching to a running
// container's stdio streams, which is a Phase 2.1 item.
func (s *RuntimeSpawner) SpawnContainer(ctx context.Context, cfg config.MCPServer) (string, string, func(context.Context), error) {
	noop := func(context.Context) {}
	if s.rt == nil {
		return "", "", noop, errors.New("container probe not available: no runtime wired")
	}

	transport := strings.ToLower(cfg.Transport)
	workloadName := fmt.Sprintf("gridctl-probe-%d", s.now().UnixNano())
	exposedPort := cfg.Port
	if exposedPort == 0 {
		return "", "", noop, errors.New("container probe requires a port")
	}

	cfgBytes := runtime.WorkloadConfig{
		Name:        workloadName,
		Stack:       "gridctl-probe",
		Type:        runtime.WorkloadTypeMCPServer,
		Image:       cfg.Image,
		Command:     cfg.Command,
		Env:         cfg.Env,
		ExposedPort: exposedPort,
		Transport:   transport,
		Labels: map[string]string{
			"gridctl.probe": "true",
		},
	}

	status, err := s.rt.Start(ctx, cfgBytes)
	if err != nil {
		return "", "", noop, fmt.Errorf("start container: %w", err)
	}

	containerID := string(status.ID)
	release := func(rctx context.Context) {
		// Stop is best-effort — Remove is the authoritative cleanup. Log but
		// don't propagate Stop errors because Remove will still fire.
		if err := s.rt.Stop(rctx, status.ID); err != nil {
			s.logger.Warn("probe teardown: stop failed", "container", containerID, "error", err)
		}
		if err := s.rt.Remove(rctx, status.ID); err != nil {
			s.logger.Warn("probe teardown: remove failed", "container", containerID, "error", err)
		}
	}

	endpoint := status.Endpoint
	if endpoint == "" {
		// Synthesize one from the host port the runtime assigned.
		endpoint = fmt.Sprintf("http://localhost:%d/mcp", status.HostPort)
	} else if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		endpoint = "http://" + endpoint + "/mcp"
	}

	return endpoint, containerID, release, nil
}
