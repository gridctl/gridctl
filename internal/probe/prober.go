package probe

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/logging"
	"github.com/gridctl/gridctl/pkg/mcp"
)

// Default probe timeout. Overridable via the config's ReadyTimeout field
// following the same semantics the gateway uses for HTTP readiness waits.
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

func newErr(code, msg, hint string) *Error {
	return &Error{Code: code, Message: msg, Hint: hint}
}

// Result is what a probe returns on success.
type Result struct {
	Tools  []mcp.Tool
	Cached bool
}

// unsupportedHint is the canonical guidance shown when the probe cannot run.
// It points users at the post-deploy tool editor on the topology sidebar,
// which is the primary curation surface for container / local-process /
// stdio / SSH / OpenAPI servers.
const unsupportedHint = "Enter tool names manually in the wizard's Advanced section, or curate them from the topology sidebar after deploy."

// Prober enumerates an MCP server's tools without registering it with the
// gateway. Scope: external URL transport only.
//
// For every other transport (container HTTP/SSE, container stdio, local
// process, SSH, OpenAPI) the probe returns CodeUnsupportedTransport with a
// hint pointing users at the post-deploy tool editor. Running a server
// ephemerally before deploy is a niche workflow compared to the common
// deploy-then-curate flow that the topology editor supports.
type Prober struct {
	cache  *Cache
	logger *slog.Logger

	// clientFactory is overridable in tests so handler tests can inject a
	// stubbed transport without standing up real HTTP servers.
	clientFactory ClientFactory
}

// ClientFactory builds an MCP client for a given transport. The default
// implementation defers to the standard pkg/mcp constructors; tests override
// it to inject stubs.
type ClientFactory interface {
	NewHTTP(name, endpoint string) mcp.AgentClient
}

// NewProber wires up a prober. A nil cache becomes a new one with DefaultTTL.
func NewProber(cache *Cache) *Prober {
	if cache == nil {
		cache = NewCache(DefaultTTL)
	}
	return &Prober{
		cache:         cache,
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

// SetClientFactory overrides the MCP client constructor. Primarily for tests.
func (p *Prober) SetClientFactory(f ClientFactory) {
	if f != nil {
		p.clientFactory = f
	}
}

// Probe validates the config, short-circuits on cache hits, and otherwise
// connects to the external URL long enough to run initialize + tools/list.
// It is safe to call concurrently; the caller is responsible for enforcing
// concurrency caps.
func (p *Prober) Probe(ctx context.Context, cfg config.MCPServer) (Result, *Error) {
	if unsupported := unsupportedReason(cfg); unsupported != nil {
		return Result{}, unsupported
	}
	if err := validate(cfg); err != nil {
		return Result{}, err
	}

	key := Key(cfg)
	if entry, ok := p.cache.Get(key); ok {
		return Result{Tools: entry.Tools, Cached: true}, nil
	}

	timeout := resolveTimeout(cfg)
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result, err := p.probeExternal(probeCtx, cfg)
	if err != nil {
		// A deadline exceeded from the probe context is surfaced as a
		// distinct, user-friendly code so the UI can render "Probe timed out"
		// rather than a raw transport error.
		if errors.Is(probeCtx.Err(), context.DeadlineExceeded) {
			return Result{}, newErr(CodeProbeTimeout,
				fmt.Sprintf("Probe timed out after %s. The server may need more time to respond or may require additional configuration.", timeout),
				"Try increasing ready_timeout on the server, or verify the URL is reachable.")
		}
		return Result{}, err
	}

	// Only successful probes land in the cache. A transient failure should
	// not poison subsequent reads.
	p.cache.Put(key, result.Tools)
	return Result{Tools: result.Tools, Cached: false}, nil
}

// unsupportedReason returns a structured error for any transport the current
// probe implementation does not handle. External URL is the only supported
// path; every other transport routes to the topology editor post-deploy.
func unsupportedReason(cfg config.MCPServer) *Error {
	switch {
	case cfg.IsSSH():
		return newErr(CodeUnsupportedTransport,
			"Probe not supported for ssh servers.",
			unsupportedHint)
	case cfg.IsOpenAPI():
		return newErr(CodeUnsupportedTransport,
			"Probe not supported for openapi servers.",
			"Use openapi.operations.include / exclude to curate tools, or the topology sidebar editor after deploy.")
	case cfg.IsLocalProcess():
		return newErr(CodeUnsupportedTransport,
			"Probe not supported for local-process servers.",
			unsupportedHint)
	case cfg.IsExternal():
		return nil
	default:
		// Container-backed (image or source). Not supported for probe in this
		// release — the common case is deploy-then-curate from the topology.
		return newErr(CodeUnsupportedTransport,
			"Probe not supported for container servers.",
			unsupportedHint)
	}
}

func (p *Prober) probeExternal(ctx context.Context, cfg config.MCPServer) (Result, *Error) {
	client := p.clientFactory.NewHTTP(probeClientName(cfg), cfg.URL)
	return runClient(ctx, client)
}

// runClient executes the Initialize + RefreshTools handshake and returns the
// tool list. It collapses transport-specific failures into structured probe
// errors with the stable codes the frontend understands. This path
// intentionally does not log raw error strings because they may contain env
// values.
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

// closeClient tries Close on clients that support it. The external URL
// (HTTP) client does not hold a persistent connection, so this is usually a
// no-op — but the type check future-proofs against stubbed clients in tests
// that do implement Close.
func closeClient(client mcp.AgentClient) {
	type closer interface{ Close() error }
	if c, ok := client.(closer); ok {
		_ = c.Close()
	}
}

func validate(cfg config.MCPServer) *Error {
	// At this point unsupportedReason has already routed everything except
	// external URL to unsupported_transport. Only external-URL-specific
	// validation remains.
	if strings.TrimSpace(cfg.URL) == "" {
		return newErr(CodeInvalidConfig, "external servers require a url.", "Set the server's url field.")
	}
	return nil
}

func resolveTimeout(cfg config.MCPServer) time.Duration {
	if d := cfg.ResolvedReadyTimeout(); d > 0 {
		return d
	}
	return defaultTimeout
}

// probeClientName tags logs from a probe with a stable prefix.
func probeClientName(cfg config.MCPServer) string {
	if cfg.Name != "" {
		return "probe:" + cfg.Name
	}
	return "probe:anonymous"
}

// defaultClientFactory wires the public mcp.NewClient constructor. Kept as a
// struct (not a free function) so tests can swap it.
type defaultClientFactory struct{}

func (defaultClientFactory) NewHTTP(name, endpoint string) mcp.AgentClient {
	return mcp.NewClient(name, endpoint)
}
