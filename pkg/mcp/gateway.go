package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"encoding/json"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/docker/docker/api/types/container"
	"github.com/gridctl/gridctl/pkg/dockerclient"
	"github.com/gridctl/gridctl/pkg/format"
	"github.com/gridctl/gridctl/pkg/logging"
	"github.com/gridctl/gridctl/pkg/token"
)

// ErrReadyTimeout indicates that an HTTP/SSE MCP server did not become reachable
// within the configured readiness window. Callers can use errors.Is to distinguish
// this from context cancellation or other registration errors.
var ErrReadyTimeout = errors.New("ready timeout")

// MCPServerConfig contains configuration for connecting to an MCP server.
type MCPServerConfig struct {
	Name              string
	Transport         Transport
	Endpoint          string               // For HTTP/SSE transport
	ContainerID       string               // For Docker Stdio transport
	External          bool                 // True for external URL servers (no container)
	LocalProcess      bool                 // True for local process servers (no container)
	SSH               bool                 // True for SSH servers (remote process over SSH)
	OpenAPI           bool                 // True for OpenAPI-based servers
	Command           []string             // For local process or SSH transport
	WorkDir           string               // For local process transport
	Env               map[string]string    // For local process or SSH transport
	SSHHost           string               // SSH hostname (for SSH servers)
	SSHUser           string               // SSH username (for SSH servers)
	SSHPort           int                  // SSH port (for SSH servers, 0 = default 22)
	SSHIdentityFile   string               // SSH identity file path (for SSH servers)
	SSHKnownHostsFile string               // SSH known_hosts file path; enables StrictHostKeyChecking=yes
	SSHJumpHost       string               // SSH jump/bastion host ([user@]host[:port])
	OpenAPIConfig     *OpenAPIClientConfig // OpenAPI configuration (for OpenAPI servers)
	Auth              *ServerAuthConfig    // Downstream auth for external URL servers (nil = none)
	HeaderSource      HeaderSource         // Live auth header source (OAuth broker); overrides Auth's static mapping
	Tools             []string             // Tool whitelist (empty = all tools)
	OutputFormat      string               // Output format: "json", "toon", "csv", "text"
	PinSchemas        *bool                // Override gateway schema pinning (nil = inherit gateway default)

	// ReadyTimeout overrides the HTTP/SSE readiness wait. Zero uses DefaultReadyTimeout.
	// Applies only to HTTP and SSE transports; stdio and other paths ignore it.
	ReadyTimeout time.Duration

	// PingTimeout overrides the per-ping deadline used by the health monitor.
	// Zero uses DefaultPingTimeout. Useful for slow upstreams (e.g. HTTP servers
	// with many tools) where the 5s default can flake under autoscale spawn load.
	PingTimeout time.Duration

	// CleanupOnReadyFailure runs when waitForHTTPServer returns ErrReadyTimeout.
	// Callers that manage the underlying container populate this with a closure
	// that stops and removes it, so a retry starts from a clean slate. nil means
	// "no cleanup" (e.g. external servers, tests).
	CleanupOnReadyFailure func(ctx context.Context) error
}

// OpenAPIClientConfig contains configuration for an OpenAPI-backed MCP client.
type OpenAPIClientConfig struct {
	Spec       string   // URL or local file path to OpenAPI spec
	BaseURL    string   // Override server URL from spec
	AuthType   string   // "bearer", "header", "query", "oauth2", or "basic"
	AuthToken  string   // Resolved bearer token (from env)
	AuthHeader string   // Header name for header-based auth
	AuthValue  string   // Resolved header value (from env)
	Include    []string // Operation IDs to include
	Exclude    []string // Operation IDs to exclude
	NoExpand   bool     // If true, skip environment variable expansion in spec file

	// Query param auth fields
	AuthQueryParam string // Query parameter name for type: query
	AuthQueryValue string // Resolved query parameter value (from env)

	// OAuth2 client credentials fields
	OAuth2ClientID     string   // Resolved OAuth2 client ID (from env)
	OAuth2ClientSecret string   // Resolved OAuth2 client secret (from env)
	OAuth2TokenURL     string   // OAuth2 token endpoint URL
	OAuth2Scopes       []string // OAuth2 scopes to request

	// Basic auth fields
	BasicUsername string // Resolved username (from env)
	BasicPassword string // Resolved password (from env)

	// TLS/mTLS fields
	TLSCertFile           string // Client certificate file path
	TLSKeyFile            string // Client private key file path
	TLSCAFile             string // Custom CA certificate file path
	TLSInsecureSkipVerify bool   // Skip server certificate verification
}

// HealthStatus tracks the health state of a downstream MCP server.
type HealthStatus struct {
	Healthy     bool      // Whether the server is responding to pings
	LastCheck   time.Time // When the last health check ran
	LastHealthy time.Time // When the server was last seen healthy
	Error       string    // Error message if unhealthy (empty when healthy)
}

// DefaultHealthCheckInterval is the default interval between health checks.
const DefaultHealthCheckInterval = 30 * time.Second

// Gateway aggregates multiple MCP servers into a single endpoint.
type Gateway struct {
	router    *Router
	sessions  *SessionManager
	dockerCli dockerclient.DockerClient
	logger    *slog.Logger
	cancel    context.CancelFunc

	mu          sync.RWMutex
	serverInfo  ServerInfo
	serverMeta  map[string]MCPServerConfig // name -> config for status reporting
	codeMode    *CodeMode                  // nil when code mode is off
	codeModeStr string                     // "off", "on" — for status reporting

	healthMu      sync.RWMutex
	health        map[string]*HealthStatus         // name -> rollup health (public API)
	replicaHealth map[string]map[int]*HealthStatus // name -> replica_id -> health

	regFailMu            sync.RWMutex
	registrationFailures map[string]string // name -> error message for servers that failed to register

	authStateMu sync.RWMutex
	authState   map[string]ServerAuthState // name -> downstream authorization state

	toolCallObserver  ToolCallObserver  // optional observer for tool call metrics
	promptGetObserver PromptGetObserver // optional observer for prompt-get (skill usage) metrics

	defaultOutputFormat   string                // gateway-level default output format
	tokenCounter          token.Counter         // token counter for format savings calculation
	formatSavingsRecorder FormatSavingsRecorder // optional recorder for format savings

	maxToolResultBytes int // maximum tool result size before truncation (0 = default 64KB)

	toolCountWarned bool // whether the tool count hint has been logged

	schemaVerifier SchemaVerifier // optional TOFU schema verifier (pins.GatewayAdapter)
	pinAction      string         // "warn" | "block" on drift (default "warn")
	blockedMu      sync.RWMutex
	blockedServers map[string]bool // servers blocked due to unacknowledged schema drift

	autoMu      sync.RWMutex
	autoscalers map[string]*Autoscaler // name -> scaler for autoscaled replica sets

	// clientPolicy is the per-client tool access filter resolved from the
	// stack.yaml `clients:` block. nil means no block was configured and every
	// client sees every tool (legacy behavior). Guarded by mu; replaced
	// wholesale on apply and hot-reload.
	clientPolicy *ClientAccessPolicy

	// callGates are veto-capable pre-call policy checks run in slice order
	// on every tools/call, after the client-scope check and before routing.
	// See the CallGate doc in types.go for the fixed-order-slice design
	// decision. Canonical order: rate limits before budgets, so a
	// rate-limited caller gets the cheaper check's message. Guarded by mu;
	// replaced wholesale on apply and hot-reload.
	callGates []CallGate

	// costSettler receives each priced call's cost after observation so
	// budget windows settle synchronously. Guarded by mu.
	costSettler CostSettler

	// groupPolicy is the compiled `groups:` block: the exposure-layer
	// curation axis. nil means no groups are configured and only the
	// default /mcp surface exists. Guarded by mu; replaced wholesale on
	// apply and hot-reload.
	groupPolicy *GroupPolicy
}

// NewGateway creates a new MCP gateway.
func NewGateway() *Gateway {
	return &Gateway{
		router:   NewRouter(),
		sessions: NewSessionManager(),
		logger:   logging.NewDiscardLogger(),
		serverInfo: ServerInfo{
			Name:    "gridctl-gateway",
			Version: "dev",
		},
		serverMeta:           make(map[string]MCPServerConfig),
		health:               make(map[string]*HealthStatus),
		replicaHealth:        make(map[string]map[int]*HealthStatus),
		blockedServers:       make(map[string]bool),
		autoscalers:          make(map[string]*Autoscaler),
		registrationFailures: make(map[string]string),
		authState:            make(map[string]ServerAuthState),
	}
}

// SetLogger sets the logger for gateway operations.
// If nil is passed, logging is disabled (default).
func (g *Gateway) SetLogger(logger *slog.Logger) {
	if logger != nil {
		g.logger = logger
	}
}

// SetDockerClient sets the Docker client for stdio transport.
func (g *Gateway) SetDockerClient(cli dockerclient.DockerClient) {
	g.dockerCli = cli
}

// SetToolCallObserver sets an observer that is notified after every tool call.
// Used to collect token usage metrics without coupling the gateway to a metrics package.
func (g *Gateway) SetToolCallObserver(obs ToolCallObserver) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.toolCallObserver = obs
}

// SetPromptGetObserver sets an observer that is notified after every
// successful prompts/get. Used to collect per-skill usage metrics without
// coupling the gateway to a metrics package.
func (g *Gateway) SetPromptGetObserver(obs PromptGetObserver) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.promptGetObserver = obs
}

// SetVersion sets the gateway version string.
func (g *Gateway) SetVersion(version string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.serverInfo.Version = version
}

// SetName overrides the announced serverInfo name. Empty input is a no-op so
// an unset stack field can never blank the identity.
func (g *Gateway) SetName(name string) {
	if name == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.serverInfo.Name = name
}

// SetCodeMode enables code mode with the given timeout.
// When code mode is active, tools/list returns meta-tools instead of individual tools.
func (g *Gateway) SetCodeMode(timeout time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()
	cm := NewCodeMode(timeout)
	cm.SetLogger(g.logger)
	g.codeMode = cm
	g.codeModeStr = "on"
}

// SetClientAccessPolicy installs the per-client tool access filter. Passing nil
// disables scoping (every client sees every tool). The gateway re-resolves
// scope from the live policy on every tools/list and tools/call, so a hot
// reload that swaps the policy takes effect on the next request — including for
// already-established sessions.
func (g *Gateway) SetClientAccessPolicy(policy *ClientAccessPolicy) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.clientPolicy = policy
}

// SetCallGates installs the pre-call policy gates, replacing any previous
// set. Passing nil (or an empty slice) removes all gates, restoring
// unrestricted dispatch. Like SetClientAccessPolicy, the slice is read fresh
// on every call, so a hot-reload swap takes effect on the next request.
func (g *Gateway) SetCallGates(gates []CallGate) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.callGates = gates
}

// SetCostSettler installs the post-call cost settlement hook. Passing nil
// removes it.
func (g *Gateway) SetCostSettler(s CostSettler) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.costSettler = s
}

// SetGroupPolicy installs the compiled tool-group policy. Passing nil
// removes all groups (their endpoints 404 and bound sessions are denied).
// Like the client access policy, the live policy is read fresh on every
// request, so a hot-reload swap takes effect immediately.
func (g *Gateway) SetGroupPolicy(policy *GroupPolicy) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.groupPolicy = policy
}

// CurrentGroupPolicy returns the current group policy under a read lock.
func (g *Gateway) CurrentGroupPolicy() *GroupPolicy {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.groupPolicy
}

// HasGroup reports whether a group with the given name is configured. The
// transport uses it to 404 unknown group endpoints before session creation.
func (g *Gateway) HasGroup(name string) bool {
	return g.CurrentGroupPolicy().Has(name)
}

// GroupsStatus resolves every configured group against the live aggregated
// tool surface, for GET /api/groups and `gridctl groups`.
func (g *Gateway) GroupsStatus() []GroupStatus {
	return g.CurrentGroupPolicy().Status(g.router.AggregatedTools())
}

// clientAccessPolicy returns the current policy under a read lock.
func (g *Gateway) clientAccessPolicy() *ClientAccessPolicy {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.clientPolicy
}

// scopeToolsForContext narrows tools to those the connecting client (resolved
// from ctx) is allowed to see. It is the single chokepoint every tool-exposure
// path funnels through so the code-mode universe cannot bypass the filter.
func (g *Gateway) scopeToolsForContext(ctx context.Context, tools []Tool) []Tool {
	policy := g.clientAccessPolicy()
	if policy == nil {
		return tools
	}
	return policy.Filter(ClientAccessIDFromContext(ctx), tools)
}

// clientAllowsToolCall reports whether the connecting client (resolved from
// ctx) may invoke the given prefixed tool. Always true when no policy is set.
func (g *Gateway) clientAllowsToolCall(ctx context.Context, prefixedName string) bool {
	policy := g.clientAccessPolicy()
	if policy == nil {
		return true
	}
	return policy.Allows(ClientAccessIDFromContext(ctx), prefixedName)
}

// ClientAccessConfigured reports whether a `clients:` access block is in effect.
func (g *Gateway) ClientAccessConfigured() bool {
	return g.clientAccessPolicy() != nil
}

// ClientScope returns the backend-computed effective scope for the given access
// identifier: the servers and prefixed tools it can reach after intersecting
// its allow-list with the live tool surface. Used by the stack/clients API
// so the frontend renders the real per-client subgraph.
func (g *Gateway) ClientScope(accessID string) ClientScopeResult {
	return g.clientAccessPolicy().scopeResult(NormalizeClientID(accessID), g.router.CatalogTools())
}

// ClientScopePreview computes the effective scope a client WOULD have under a
// hypothetical server/tool allow-list, intersected with the live tool surface,
// without mutating any installed policy (read-only). It backs the per-client
// "what changes" preview the Stack Access Lens shows before a commit.
//
// servers and tools are tri-state allow-lists matching the PUT /scope contract:
// a nil tools slice preserves the client's currently-configured tool allow-list
// (so a server-only draft never appears to clobber an operator-authored tool
// list), while a non-nil tools slice replaces it. A listed client's scope
// depends only on its own profile, so simulating one profile in isolation is
// faithful to what enforcement would compute after the write.
func (g *Gateway) ClientScopePreview(accessID string, servers, tools []string) ClientScopeResult {
	key := NormalizeClientID(accessID)
	if tools == nil {
		if _, saved, listed := g.clientAccessPolicy().profileAllowLists(key); listed {
			tools = saved
		}
	}
	spec := &ClientAccessSpec{
		Profiles: map[string]ClientProfileSpec{
			key: {Servers: servers, Tools: tools},
		},
	}
	return NewClientAccessPolicy(spec).scopeResult(key, g.router.CatalogTools())
}

// CatalogToolNames returns every prefixed tool name on the live surface,
// unscoped. The scope preview uses its length as the "of N tools" denominator.
func (g *Gateway) CatalogToolNames() []string {
	tools := g.router.CatalogTools()
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Name)
	}
	return names
}

// SetDefaultOutputFormat sets the gateway-level default output format.
func (g *Gateway) SetDefaultOutputFormat(format string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.defaultOutputFormat = format
}

// SetMaxToolResultBytes sets the maximum tool result size in bytes before truncation.
// When set to 0, the default of 65536 (64KB) is used.
func (g *Gateway) SetMaxToolResultBytes(n int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.maxToolResultBytes = n
}

// SetTokenCounter sets the token counter used for format savings calculation.
func (g *Gateway) SetTokenCounter(counter token.Counter) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.tokenCounter = counter
}

// SetFormatSavingsRecorder sets the recorder for format savings metrics.
func (g *Gateway) SetFormatSavingsRecorder(recorder FormatSavingsRecorder) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.formatSavingsRecorder = recorder
}

// SetSchemaVerifier wires in a SchemaVerifier for TOFU schema pinning.
// action must be "warn" (default) or "block".
func (g *Gateway) SetSchemaVerifier(sv SchemaVerifier, action string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.schemaVerifier = sv
	if action == "block" {
		g.pinAction = "block"
	} else {
		g.pinAction = "warn"
	}
}

// SchemaVerifier returns the wired TOFU schema verifier, or nil when schema
// pinning is not installed. Exposed so callers and tests can confirm whether
// the gateway is enforcing pinning.
func (g *Gateway) SchemaVerifier() SchemaVerifier {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.schemaVerifier
}

// ResetServerPins clears the stored pin record for serverName, if the wired
// SchemaVerifier also implements PinResetter. Called before re-registering a
// modified server during hot reload so the next VerifyOrPin re-pins from scratch
// rather than flagging config-driven tool changes as drift.
func (g *Gateway) ResetServerPins(serverName string) error {
	g.mu.RLock()
	sv := g.schemaVerifier
	g.mu.RUnlock()
	if sv == nil {
		return nil
	}
	if r, ok := sv.(PinResetter); ok {
		return r.ResetServerPins(serverName)
	}
	return nil
}

// UnblockServer clears the block on a server that was blocked due to schema drift.
// Called by the approve flow after the user accepts the updated tool definitions.
func (g *Gateway) UnblockServer(serverName string) {
	g.blockedMu.Lock()
	defer g.blockedMu.Unlock()
	delete(g.blockedServers, serverName)
}

// pinningEnabledForServer reports whether schema pinning should run for serverName.
// Returns false if schemaVerifier is nil, or if the server's PinSchemas field is explicitly false.
func (g *Gateway) pinningEnabledForServer(serverName string) bool {
	if g.schemaVerifier == nil {
		return false
	}
	g.mu.RLock()
	cfg, ok := g.serverMeta[serverName]
	g.mu.RUnlock()
	if ok && cfg.PinSchemas != nil {
		return *cfg.PinSchemas
	}
	return true
}

// handlePinDrift applies the configured drift policy to a list of schema changes.
// In warn mode it logs a structured warning. In block mode it also marks the server blocked.
func (g *Gateway) handlePinDrift(serverName string, drifts []SchemaDrift) {
	if len(drifts) == 0 {
		return
	}
	g.logger.Warn("schema drift detected",
		"server", serverName,
		"modified", len(drifts))
	for _, d := range drifts {
		g.logger.Warn("tool modified",
			"server", serverName,
			"tool", d.Name,
			"old_description", d.OldDescription,
			"new_description", d.NewDescription)
		for _, f := range d.Findings {
			if f.Severity != "warn" && f.Severity != "critical" {
				continue
			}
			g.logger.Warn("poisoning heuristic flagged drifted tool",
				"server", serverName,
				"tool", d.Name,
				"code", f.Code,
				"severity", f.Severity,
				"field", f.Field,
				"finding", f.Message)
		}
	}
	if g.pinAction == "block" {
		g.blockedMu.Lock()
		g.blockedServers[serverName] = true
		g.blockedMu.Unlock()
		g.logger.Warn("server blocked pending schema approval",
			"server", serverName,
			"hint", "run 'gridctl pins approve "+serverName+"' to resume")
	} else {
		g.logger.Warn("run 'gridctl pins approve "+serverName+"' to accept these changes or investigate the server",
			"server", serverName)
	}
}

// resolveOutputFormat returns the output format for the given server.
// Resolution order: server format > gateway default > "json".
func (g *Gateway) resolveOutputFormat(serverName string) string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if meta, ok := g.serverMeta[serverName]; ok && meta.OutputFormat != "" {
		return meta.OutputFormat
	}
	if g.defaultOutputFormat != "" {
		return g.defaultOutputFormat
	}
	return "json"
}

// CodeModeStatus returns the code mode status string ("off" or "on").
func (g *Gateway) CodeModeStatus() string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.codeModeStr == "" {
		return "off"
	}
	return g.codeModeStr
}

// Router returns the tool router.
func (g *Gateway) Router() *Router {
	return g.router
}

// Sessions returns the session manager.
func (g *Gateway) Sessions() *SessionManager {
	return g.sessions
}

// SessionCount returns the number of active sessions.
func (g *Gateway) SessionCount() int {
	return g.sessions.Count()
}

// StartCleanup starts periodic session cleanup. Call Close() to stop.
func (g *Gateway) StartCleanup(ctx context.Context) {
	ctx, g.cancel = context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				removed := g.sessions.Cleanup(30 * time.Minute)
				if removed > 0 {
					g.logger.Info("cleaned up stale sessions", "removed", removed)
				}
			}
		}
	}()
}

// StartHealthMonitor starts periodic health checking for all registered MCP servers.
// It runs alongside StartCleanup and stops when the gateway context is cancelled.
func (g *Gateway) StartHealthMonitor(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				g.checkHealth(ctx)
			}
		}
	}()
}

// checkHealth pings every replica of every registered MCP server and updates
// per-replica health state plus a per-server rollup. Replicas that implement
// Reconnectable are restarted on failure, gated by an exponential backoff so
// a crashing replica does not spin.
func (g *Gateway) checkHealth(ctx context.Context) {
	for _, set := range g.router.ReplicaSets() {
		name := set.Name()

		// Only check sets that have server metadata (actual MCP servers,
		// not the registry or other non-MCP routed clients).
		g.mu.RLock()
		_, isMCPServer := g.serverMeta[name]
		g.mu.RUnlock()
		if !isMCPServer {
			continue
		}

		for _, replica := range set.Replicas() {
			g.checkReplicaHealth(ctx, name, replica)
		}
		g.recomputeRollup(name, set)
	}
}

// checkReplicaHealth runs one health cycle for a single replica: ping, update
// per-replica status, and optionally trigger a backoff-gated Reconnect.
func (g *Gateway) checkReplicaHealth(ctx context.Context, serverName string, replica *Replica) {
	client := replica.Client()
	pingable, ok := client.(Pingable)
	if !ok {
		// Not pingable (e.g. pure HTTP without ping) — treat as healthy and
		// let tool calls surface real failures. Keep the replica in rotation.
		return
	}

	logger := logging.WithReplicaID(g.logger, replica.ID())
	now := time.Now()
	err := pingable.Ping(ctx)

	g.healthMu.Lock()
	prev := g.replicaStatusLocked(serverName, replica.ID())
	status := &HealthStatus{
		Healthy:   err == nil,
		LastCheck: now,
	}
	if err == nil {
		status.LastHealthy = now
		if prev != nil && !prev.Healthy {
			logger.Info("MCP server recovered", "name", serverName)
		}
	} else {
		status.Error = err.Error()
		if prev != nil {
			status.LastHealthy = prev.LastHealthy
		}
		if prev == nil || prev.Healthy {
			logger.Warn("MCP server unhealthy", "name", serverName, "error", err)
		}
	}
	g.setReplicaStatusLocked(serverName, replica.ID(), status)
	g.healthMu.Unlock()

	if err == nil {
		replica.SetHealthy(true)
		replica.Restart().Reset()
		return
	}

	// A broker-managed auth failure that survived the transport's silent
	// refresh-retry means the authorization itself is gone: surface
	// needs-auth (the actionable state) alongside the unhealthy marker.
	// Raw challenges from non-broker servers stay plain unhealthy.
	var needsAuth *NeedsAuthError
	if errors.As(err, &needsAuth) {
		g.markNeedsAuthIfNew(serverName, "authorization required")
	}

	// Unhealthy: exclude from dispatch and try to restart if eligible.
	replica.SetHealthy(false)

	rc, reconnectable := client.(Reconnectable)
	if !reconnectable {
		return
	}
	if !replica.Restart().ShouldTry(now) {
		// Still in backoff window; wait for next check.
		return
	}

	logger.Info("attempting reconnection", "name", serverName)
	if reconnErr := rc.Reconnect(ctx); reconnErr != nil {
		delay := replica.Restart().Advance(now)
		logger.Warn("reconnection failed", "name", serverName, "error", reconnErr, "next_retry_in", delay)
		return
	}

	// Reconnect succeeded — back in rotation.
	replica.Restart().Reset()
	replica.SetHealthy(true)
	replica.MarkStarted(time.Now())

	g.healthMu.Lock()
	g.setReplicaStatusLocked(serverName, replica.ID(), &HealthStatus{
		Healthy:     true,
		LastCheck:   time.Now(),
		LastHealthy: time.Now(),
	})
	g.healthMu.Unlock()

	g.router.RefreshTools()
	logger.Info("MCP server reconnected", "name", serverName)

	// Verify pins after reconnection using replica-0's tool surface if we
	// can get it; otherwise use this replica's tools. Drift on reconnect is
	// suspicious but pinning stays per-server, not per-replica.
	if g.pinningEnabledForServer(serverName) {
		drifts, pinErr := g.schemaVerifier.VerifyOrPin(serverName, client.Tools())
		if pinErr != nil {
			logger.Warn("pins: verification failed after reconnect", "name", serverName, "error", pinErr)
		} else {
			g.handlePinDrift(serverName, drifts)
		}
	}
}

// replicaStatusLocked returns the stored replica health. Callers must hold
// g.healthMu (read or write).
func (g *Gateway) replicaStatusLocked(serverName string, replicaID int) *HealthStatus {
	if g.replicaHealth == nil {
		return nil
	}
	m := g.replicaHealth[serverName]
	if m == nil {
		return nil
	}
	return m[replicaID]
}

// setReplicaStatusLocked stores replica health. Callers must hold g.healthMu
// (write).
func (g *Gateway) setReplicaStatusLocked(serverName string, replicaID int, status *HealthStatus) {
	m := g.replicaHealth[serverName]
	if m == nil {
		m = make(map[int]*HealthStatus)
		g.replicaHealth[serverName] = m
	}
	m[replicaID] = status
}

// recomputeRollup updates the per-server rollup HealthStatus from the latest
// per-replica statuses. The rollup is healthy when at least one replica is
// healthy; error is the most recent replica error otherwise.
func (g *Gateway) recomputeRollup(serverName string, set *ReplicaSet) {
	g.healthMu.Lock()
	defer g.healthMu.Unlock()

	anyHealthy := false
	sawAny := false
	var lastCheck, lastHealthy time.Time
	var lastErr string
	for _, r := range set.Replicas() {
		s := g.replicaStatusLocked(serverName, r.ID())
		if s == nil {
			continue
		}
		sawAny = true
		if s.LastCheck.After(lastCheck) {
			lastCheck = s.LastCheck
		}
		if s.LastHealthy.After(lastHealthy) {
			lastHealthy = s.LastHealthy
		}
		if s.Healthy {
			anyHealthy = true
		} else if s.Error != "" {
			lastErr = s.Error
		}
	}

	// Non-pingable replicas produce no status; don't fabricate a rollup for them.
	// When the set is empty (scale-to-zero) the prior rollup must be cleared so a
	// stale Unhealthy entry from before the reap does not persist forever.
	if !sawAny {
		delete(g.health, serverName)
		delete(g.replicaHealth, serverName)
		return
	}

	rollup := &HealthStatus{
		Healthy:     anyHealthy,
		LastCheck:   lastCheck,
		LastHealthy: lastHealthy,
	}
	if !anyHealthy {
		rollup.Error = lastErr
	}
	g.health[serverName] = rollup
}

// GetHealthStatus returns the health status for a named MCP server.
// Returns nil if no health data is available.
func (g *Gateway) GetHealthStatus(name string) *HealthStatus {
	g.healthMu.RLock()
	defer g.healthMu.RUnlock()
	return g.health[name]
}

// ReplicaStatuses returns per-replica status for the named server, ordered by
// replica id. Returns nil if the server is not registered.
func (g *Gateway) ReplicaStatuses(serverName string) []ReplicaStatus {
	set := g.router.GetReplicaSet(serverName)
	if set == nil {
		return nil
	}
	replicas := set.Replicas()
	out := make([]ReplicaStatus, 0, len(replicas))

	// Snapshot per-replica health under the lock so later iteration cannot
	// race with the health monitor's concurrent writes to replicaHealth[...].
	replicaHealthSnap := make(map[int]HealthStatus, len(replicas))
	g.healthMu.RLock()
	if m := g.replicaHealth[serverName]; m != nil {
		for id, hs := range m {
			if hs != nil {
				replicaHealthSnap[id] = *hs
			}
		}
	}
	g.healthMu.RUnlock()

	for _, r := range replicas {
		rs := ReplicaStatus{
			ReplicaID: r.ID(),
			Healthy:   r.Healthy(),
			InFlight:  r.InFlight(),
			StartedAt: r.StartedAt(),
		}
		attempts := r.Restart().Attempts()
		rs.RestartAttempts = attempts
		if nextAt := r.Restart().NextAt(); !nextAt.IsZero() {
			t := nextAt
			rs.NextRetryAt = &t
		}
		if hs, ok := replicaHealthSnap[r.ID()]; ok {
			if !hs.LastCheck.IsZero() {
				t := hs.LastCheck
				rs.LastCheck = &t
			}
			if !hs.LastHealthy.IsZero() {
				t := hs.LastHealthy
				rs.LastHealthy = &t
			}
			rs.LastError = hs.Error
		}
		switch client := r.Client().(type) {
		case *ProcessClient:
			rs.PID = client.PID()
		case *StdioClient:
			rs.ContainerID = client.ContainerID()
		}
		rs.State = replicaStateString(rs.Healthy, attempts > 0)
		out = append(out, rs)
	}
	return out
}

// replicaStateString maps a replica's health flag and restart-attempt counter
// to a short state label: "healthy", "restarting" (unhealthy but currently
// backing off a retry), or "unhealthy" (unhealthy with no retry pending).
func replicaStateString(healthy bool, hasAttempts bool) string {
	switch {
	case healthy:
		return "healthy"
	case hasAttempts:
		return "restarting"
	default:
		return "unhealthy"
	}
}

// Close stops the cleanup goroutine and closes all agent client connections.
func (g *Gateway) Close() {
	if g.cancel != nil {
		g.cancel()
	}

	for _, client := range g.router.Clients() {
		if closer, ok := client.(io.Closer); ok {
			closer.Close()
		}
	}
}

// ServerInfo returns the gateway server info.
func (g *Gateway) ServerInfo() ServerInfo {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.serverInfo
}

// RegisterMCPServer registers and initializes a single-replica MCP server.
// Equivalent to RegisterMCPReplicaSet with one config and round-robin policy.
func (g *Gateway) RegisterMCPServer(ctx context.Context, cfg MCPServerConfig) error {
	return g.RegisterMCPReplicaSet(ctx, cfg.Name, ReplicaPolicyRoundRobin, []MCPServerConfig{cfg})
}

// RegisterAutoscaler registers an autoscaled replica set for an MCP server.
// The Spawner owns replica provisioning; the gateway only stores metadata and
// wires the scaler into the router. One synchronous Tick is executed before
// returning so Min (and WarmPool) replicas are available before the caller's
// first tool call — except when IdleToZero=true and Min=0, in which case the
// first tool call triggers a cold-start spawn instead.
func (g *Gateway) RegisterAutoscaler(ctx context.Context, template MCPServerConfig, policy string, spawner Spawner, autoscale AutoscalePolicy) error {
	if template.Name == "" {
		return fmt.Errorf("register autoscaler: empty name")
	}
	if spawner == nil {
		return fmt.Errorf("register autoscaler %s: nil spawner", template.Name)
	}
	if autoscale.Max < 1 {
		return fmt.Errorf("register autoscaler %s: invalid policy (max=%d)", template.Name, autoscale.Max)
	}
	if policy == "" {
		policy = ReplicaPolicyRoundRobin
	}

	// Register metadata up front so pinningEnabledForServer and health
	// check iteration both recognise this as an MCP server.
	func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		g.serverMeta[template.Name] = template
	}()

	set := NewReplicaSet(template.Name, policy, nil)
	g.router.AddReplicaSet(set)

	scaler := NewAutoscaler(template.Name, set, spawner, autoscale, g.logger)
	g.autoMu.Lock()
	g.autoscalers[template.Name] = scaler
	g.autoMu.Unlock()

	// Seed initial replicas synchronously so the set is warm before the
	// first incoming tool call. Idle-to-zero with Min=0 and no warm pool
	// intentionally stays cold until a tool call triggers the cold-start spawn.
	skipInitialTick := autoscale.IdleToZero && autoscale.Min == 0 && autoscale.WarmPool == 0
	if !skipInitialTick {
		if _, err := scaler.Tick(ctx, time.Now()); err != nil {
			g.logger.Warn("initial autoscaler tick failed",
				"server", template.Name, "error", err)
			// Do not fail the whole registration — the next periodic tick
			// will retry; warm_pool ensures the cooldown doesn't block us.
		}
	}

	g.logger.Info("registered autoscaled MCP server",
		"name", template.Name,
		"min", autoscale.Min,
		"max", autoscale.Max,
		"target_in_flight", autoscale.TargetInFlight,
		"warm_pool", autoscale.WarmPool,
		"idle_to_zero", autoscale.IdleToZero,
	)
	return nil
}

// GetAutoscaler returns the autoscaler registered for serverName, or nil if
// the server is not autoscaled.
func (g *Gateway) GetAutoscaler(serverName string) *Autoscaler {
	g.autoMu.RLock()
	defer g.autoMu.RUnlock()
	return g.autoscalers[serverName]
}

// Autoscalers returns a snapshot slice of every registered autoscaler, sorted
// by server name for deterministic iteration in the tick loop.
func (g *Gateway) Autoscalers() []*Autoscaler {
	g.autoMu.RLock()
	names := make([]string, 0, len(g.autoscalers))
	for n := range g.autoscalers {
		names = append(names, n)
	}
	g.autoMu.RUnlock()
	sort.Strings(names)

	g.autoMu.RLock()
	defer g.autoMu.RUnlock()
	out := make([]*Autoscaler, 0, len(names))
	for _, n := range names {
		if s, ok := g.autoscalers[n]; ok {
			out = append(out, s)
		}
	}
	return out
}

// unregisterAutoscaler drops the autoscaler for a server (used during hot
// reload when switching from autoscale back to static replicas, or when the
// server is removed from the stack).
func (g *Gateway) unregisterAutoscaler(serverName string) {
	g.autoMu.Lock()
	defer g.autoMu.Unlock()
	delete(g.autoscalers, serverName)
}

// StartAutoscaler launches a background goroutine that ticks every registered
// autoscaler on the given interval. Cancelling ctx stops the loop. Safe to
// call alongside StartHealthMonitor and StartCleanup.
func (g *Gateway) StartAutoscaler(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultAutoscalerInterval
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				for _, a := range g.Autoscalers() {
					if _, err := a.Tick(ctx, now); err != nil {
						g.logger.Debug("autoscaler tick error",
							"server", a.Name(), "error", err)
					}
				}
			}
		}
	}()
}

// RegisterMCPReplicaSet initializes one AgentClient per config and registers
// them as a single replica set under the given server name. All configs must
// be for the same logical server (same Name, same transport, same tool list);
// only the per-replica runtime handles (ContainerID / Endpoint) should differ.
// For len(cfgs) == 1 this is byte-identical to the old single-client path.
//
// Partial-startup tolerance: if some replicas fail to initialize, the server
// is still registered with the successful ones. The call only returns an
// error when every replica failed, or when the single-replica case fails
// (in which case the caller sees the same error shape as before).
func (g *Gateway) RegisterMCPReplicaSet(ctx context.Context, name, policy string, cfgs []MCPServerConfig) error {
	if len(cfgs) == 0 {
		return fmt.Errorf("register %s: no replica configs", name)
	}
	start := time.Now()
	clients := make([]AgentClient, 0, len(cfgs))
	var firstErr error
	for i := range cfgs {
		client, err := g.buildAgentClient(ctx, cfgs[i])
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			if len(cfgs) > 1 {
				g.logger.Warn("replica registration failed; skipping", "name", name, "replica", i, "error", err)
				continue
			}
			return err
		}
		clients = append(clients, client)
	}
	if len(clients) == 0 {
		return fmt.Errorf("register %s: all %d replicas failed: %w", name, len(cfgs), firstErr)
	}

	// Store metadata before pin check so pinningEnabledForServer can read PinSchemas.
	// For a replica set, we store cfgs[0] as canonical — all replicas share the
	// same logical config modulo per-replica runtime handles.
	canonical := cfgs[0]
	canonical.Name = name
	func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		g.serverMeta[name] = canonical
	}()

	// Schema pinning: verify or pin on first registration. Pins are per-server
	// (not per-replica) — all replicas should expose the same tools.
	if g.pinningEnabledForServer(name) {
		drifts, err := g.schemaVerifier.VerifyOrPin(name, clients[0].Tools())
		if err != nil {
			g.logger.Warn("pins: verification failed", "server", name, "error", err)
		} else {
			g.handlePinDrift(name, drifts)
		}
	}

	g.router.AddReplicaSet(NewReplicaSet(name, policy, clients))
	g.router.RefreshTools()

	g.logger.Info("registered MCP server", "name", name, "transport", cfgs[0].Transport, "replicas", len(clients), "tools", len(clients[0].Tools()), "duration", time.Since(start))
	return nil
}

// BuildAgentClient creates, connects, and initializes an AgentClient from a
// single MCPServerConfig. It does NOT touch serverMeta, pins, health, or the
// router — callers compose that separately. Exported so Spawner implementations
// in pkg/controller can reuse the transport switch rather than duplicating it.
func (g *Gateway) BuildAgentClient(ctx context.Context, cfg MCPServerConfig) (AgentClient, error) {
	return g.buildAgentClient(ctx, cfg)
}

// buildAgentClient creates, connects, and initializes an AgentClient from a
// single MCPServerConfig. It does NOT touch serverMeta, pins, health, or the
// router — callers compose that separately.
func (g *Gateway) buildAgentClient(ctx context.Context, cfg MCPServerConfig) (AgentClient, error) {
	g.logger.Info("connecting to MCP server", "name", cfg.Name, "transport", cfg.Transport)

	var agentClient AgentClient
	clientLogger := g.logger.With("server", cfg.Name)

	// Handle OpenAPI servers
	if cfg.OpenAPI {
		if cfg.OpenAPIConfig == nil {
			return nil, fmt.Errorf("OpenAPI config required for OpenAPI server %s", cfg.Name)
		}
		openAPIClient, err := NewOpenAPIClient(cfg.Name, cfg.OpenAPIConfig)
		if err != nil {
			return nil, fmt.Errorf("creating OpenAPI client %s: %w", cfg.Name, err)
		}
		openAPIClient.SetLogger(clientLogger)
		openAPIClient.SetPingTimeout(cfg.PingTimeout)
		if len(cfg.Tools) > 0 {
			openAPIClient.SetToolWhitelist(cfg.Tools)
		}
		agentClient = openAPIClient
	} else if cfg.SSH {
		// Handle SSH servers (they use stdio over SSH)
		sshCommand := buildSSHCommand(cfg)
		processClient := NewProcessClient(cfg.Name, sshCommand, cfg.WorkDir, cfg.Env)
		processClient.SetLogger(clientLogger)
		processClient.SetPingTimeout(cfg.PingTimeout)
		if len(cfg.Tools) > 0 {
			processClient.SetToolWhitelist(cfg.Tools)
		}
		if err := processClient.Connect(ctx); err != nil {
			return nil, fmt.Errorf("starting SSH process %s: %w", cfg.Name, err)
		}
		agentClient = processClient
	} else if cfg.LocalProcess {
		// Handle local process servers (they use stdio but not Docker)
		processClient := NewProcessClient(cfg.Name, cfg.Command, cfg.WorkDir, cfg.Env)
		processClient.SetLogger(clientLogger)
		processClient.SetPingTimeout(cfg.PingTimeout)
		if len(cfg.Tools) > 0 {
			processClient.SetToolWhitelist(cfg.Tools)
		}
		if err := processClient.Connect(ctx); err != nil {
			return nil, fmt.Errorf("starting process %s: %w", cfg.Name, err)
		}
		agentClient = processClient
	} else {
		switch cfg.Transport {
		case TransportStdio:
			if g.dockerCli == nil {
				return nil, fmt.Errorf("docker client not set for stdio transport")
			}
			stdioClient := NewStdioClient(cfg.Name, cfg.ContainerID, g.dockerCli)
			stdioClient.SetLogger(clientLogger)
			stdioClient.SetPingTimeout(cfg.PingTimeout)
			if len(cfg.Tools) > 0 {
				stdioClient.SetToolWhitelist(cfg.Tools)
			}
			if err := stdioClient.Connect(ctx); err != nil {
				return nil, fmt.Errorf("connecting to container: %w", err)
			}
			agentClient = stdioClient
		case TransportSSE:
			// SSE transport - uses same HTTP client which handles text/event-stream responses
			httpClient := NewClient(cfg.Name, cfg.Endpoint)
			httpClient.SetLogger(clientLogger)
			httpClient.SetPingTimeout(cfg.PingTimeout)
			if cfg.HeaderSource != nil {
				httpClient.SetHeaderSource(cfg.HeaderSource)
			} else if hs := StaticHeaderSourceFor(cfg.Auth); hs != nil {
				httpClient.SetHeaderSource(hs)
			}
			if len(cfg.Tools) > 0 {
				httpClient.SetToolWhitelist(cfg.Tools)
			}
			// Wait for MCP server to be ready with retries
			if err := g.waitForHTTPServer(ctx, httpClient, cfg.ReadyTimeout); err != nil {
				g.handleReadyFailure(ctx, cfg, err)
				return nil, fmt.Errorf("MCP server %s not ready: %w", cfg.Name, err)
			}
			agentClient = httpClient
		case TransportHTTP, "": // Default to HTTP
			httpClient := NewClient(cfg.Name, cfg.Endpoint)
			httpClient.SetLogger(clientLogger)
			httpClient.SetPingTimeout(cfg.PingTimeout)
			if cfg.HeaderSource != nil {
				httpClient.SetHeaderSource(cfg.HeaderSource)
			} else if hs := StaticHeaderSourceFor(cfg.Auth); hs != nil {
				httpClient.SetHeaderSource(hs)
			}
			if len(cfg.Tools) > 0 {
				httpClient.SetToolWhitelist(cfg.Tools)
			}
			// Wait for MCP server to be ready with retries
			if err := g.waitForHTTPServer(ctx, httpClient, cfg.ReadyTimeout); err != nil {
				g.handleReadyFailure(ctx, cfg, err)
				return nil, fmt.Errorf("MCP server %s not ready: %w", cfg.Name, err)
			}
			agentClient = httpClient
		default:
			return nil, fmt.Errorf("unknown transport: %s", cfg.Transport)
		}
	}

	// Initialize MCP connection. Close the client on failure: for stdio,
	// process, and SSH transports Connect() has already spawned a child that
	// would otherwise be orphaned (a downstream server rejected for an
	// unsupported protocol version fails here deterministically on every
	// retry).
	if err := agentClient.Initialize(ctx); err != nil {
		closeAgentClient(agentClient)
		return nil, fmt.Errorf("initializing MCP server %s: %w", cfg.Name, err)
	}

	// Fetch tools (will be filtered by whitelist if set)
	if err := agentClient.RefreshTools(ctx); err != nil {
		closeAgentClient(agentClient)
		return nil, fmt.Errorf("fetching tools from %s: %w", cfg.Name, err)
	}

	return agentClient, nil
}

// closeAgentClient releases a client's transport resources when registration
// fails partway. Best-effort: not every transport implements Close.
func closeAgentClient(client AgentClient) {
	if closer, ok := client.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
}

// SetServerMeta stores metadata for an MCP server without connecting to it.
// This is used by tests and by internal registration paths that manage
// their own client connections.
func (g *Gateway) SetServerMeta(cfg MCPServerConfig) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.serverMeta[cfg.Name] = cfg
}

// UnregisterMCPServer removes an MCP server from the gateway.
func (g *Gateway) UnregisterMCPServer(name string) {
	g.router.RemoveClient(name)
	g.router.RefreshTools()
	g.unregisterAutoscaler(name)
	g.mu.Lock()
	delete(g.serverMeta, name)
	g.mu.Unlock()
	g.ClearRegistrationFailure(name)
	// Auth state follows the same lifecycle as registration failures:
	// without this, a removed server would keep a ghost needs-auth row in
	// Status() (stored grants are unaffected; they are keyed by resource
	// URL, not server name).
	g.ClearServerAuthState(name)
}

// RecordRegistrationFailure records why a server could not be registered so
// Status() surfaces it instead of silently omitting the server. A later
// attempt overwrites the entry; success or unregistration clears it.
//
// Broker-managed auth failures (NeedsAuthError: the server has an OAuth
// auth block and no usable grant) are routed to the needs-auth state
// instead: a server waiting on user authorization is actionable, not
// broken, and 'gridctl auth login' fixes it. A raw challenge from a server
// the broker does not manage (wrong static bearer token, or a server with
// no auth block at all) stays a registration failure — pointing such users
// at auth login would dead-end, since the broker has no configuration for
// the server — with a hint naming the stack.yaml fix.
func (g *Gateway) RecordRegistrationFailure(name string, err error) {
	if name == "" || err == nil {
		return
	}
	var needsAuth *NeedsAuthError
	if errors.As(err, &needsAuth) {
		g.markNeedsAuthIfNew(name, "authorization required")
		g.ClearRegistrationFailure(name)
		return
	}
	msg := err.Error()
	var authErr *AuthRequiredError
	if errors.As(err, &authErr) {
		msg += "; if this server requires OAuth, add 'auth: {type: oauth}' to it in stack.yaml"
	}
	g.regFailMu.Lock()
	g.registrationFailures[name] = msg
	g.regFailMu.Unlock()
}

// isAuthError reports whether err (anywhere in its chain) is an auth
// challenge from the transport or a missing-grant error from the broker.
// Both mean "reachable but unauthorized", which readiness waits abort on.
func isAuthError(err error) bool {
	var authErr *AuthRequiredError
	var needsAuth *NeedsAuthError
	return errors.As(err, &authErr) || errors.As(err, &needsAuth)
}

// markNeedsAuthIfNew transitions a server to needs-auth unless it is
// already there; an existing entry usually carries a more specific reason
// from the broker (e.g. "authorization expired") that must not be
// overwritten with a generic one.
func (g *Gateway) markNeedsAuthIfNew(name, reason string) {
	if st, ok := g.ServerAuthState(name); ok && st.Status == AuthStatusNeedsAuth {
		return
	}
	g.SetServerAuthState(name, ServerAuthState{Status: AuthStatusNeedsAuth, Error: reason})
}

// SetServerAuthState records downstream authorization state for a server.
func (g *Gateway) SetServerAuthState(name string, st ServerAuthState) {
	if name == "" {
		return
	}
	g.authStateMu.Lock()
	g.authState[name] = st
	g.authStateMu.Unlock()
}

// ServerAuthState returns the recorded downstream authorization state for a
// server and whether one exists.
func (g *Gateway) ServerAuthState(name string) (ServerAuthState, bool) {
	g.authStateMu.RLock()
	st, ok := g.authState[name]
	g.authStateMu.RUnlock()
	return st, ok
}

// ClearServerAuthState removes the recorded downstream authorization state.
func (g *Gateway) ClearServerAuthState(name string) {
	g.authStateMu.Lock()
	delete(g.authState, name)
	g.authStateMu.Unlock()
}

// ClearRegistrationFailure removes any recorded registration failure for name.
func (g *Gateway) ClearRegistrationFailure(name string) {
	g.regFailMu.Lock()
	delete(g.registrationFailures, name)
	g.regFailMu.Unlock()
}

// RestartMCPServer restarts an individual MCP server by name.
// It tears down the existing connection, optionally restarts the container
// (for stdio transport), and re-registers the server using its stored config.
func (g *Gateway) RestartMCPServer(ctx context.Context, name string) error {
	g.mu.RLock()
	cfg, ok := g.serverMeta[name]
	g.mu.RUnlock()
	if !ok {
		return fmt.Errorf("unknown MCP server: %s", name)
	}

	g.logger.Info("restarting MCP server", "name", name, "transport", cfg.Transport)

	// Close the existing client connection
	if client := g.router.GetClient(name); client != nil {
		if closer, ok := client.(io.Closer); ok {
			if err := closer.Close(); err != nil {
				g.logger.Warn("error closing MCP server connection", "name", name, "error", err)
			}
		}
	}

	// Unregister from router (removes client + cleans tool registry)
	g.UnregisterMCPServer(name)

	// For stdio (container) transport, restart the Docker container
	if cfg.Transport == TransportStdio && !cfg.External && !cfg.LocalProcess && !cfg.SSH && !cfg.OpenAPI {
		if g.dockerCli != nil && cfg.ContainerID != "" {
			timeout := 10
			if err := g.dockerCli.ContainerRestart(ctx, cfg.ContainerID, container.StopOptions{Timeout: &timeout}); err != nil {
				err = fmt.Errorf("restarting container for %s: %w", name, err)
				// The server was already unregistered; record the failure so
				// it does not silently vanish from status and the UI.
				g.RecordRegistrationFailure(name, err)
				return err
			}
		}
	}

	// Re-register using stored config (creates new client, initializes MCP, fetches tools)
	if err := g.RegisterMCPServer(ctx, cfg); err != nil {
		err = fmt.Errorf("re-registering MCP server %s: %w", name, err)
		g.RecordRegistrationFailure(name, err)
		return err
	}

	// Update health status to healthy
	g.healthMu.Lock()
	g.health[name] = &HealthStatus{
		Healthy:     true,
		LastCheck:   time.Now(),
		LastHealthy: time.Now(),
	}
	g.healthMu.Unlock()

	g.logger.Info("MCP server restarted", "name", name)
	return nil
}

// logToolCountHint logs an INFO message suggesting code_mode when tool count exceeds 50.
func (g *Gateway) logToolCountHint(toolCount int) {
	if g.toolCountWarned || toolCount <= 50 {
		return
	}
	g.toolCountWarned = true
	g.logger.Info("large tool count detected — consider enabling gateway code_mode to reduce context usage",
		"tool_count", toolCount,
		"hint", "add 'code_mode: on' to gateway config or use --code-mode flag",
	)
}

// handleReadyFailure invokes the configured cleanup callback when the readiness
// wait failed with ErrReadyTimeout. It intentionally runs only on a true
// ready-timeout — context cancellation and other error paths leave the workload
// alone so the caller can decide what to do.
func (g *Gateway) handleReadyFailure(ctx context.Context, cfg MCPServerConfig, waitErr error) {
	if !errors.Is(waitErr, ErrReadyTimeout) || cfg.CleanupOnReadyFailure == nil {
		return
	}
	g.logger.Warn("MCP server failed readiness wait; removing container",
		"name", cfg.Name,
		"ready_timeout", effectiveReadyTimeout(cfg.ReadyTimeout),
	)
	if err := cfg.CleanupOnReadyFailure(ctx); err != nil {
		g.logger.Warn("cleanup after ready timeout failed; orphan may remain",
			"name", cfg.Name, "error", err)
	}
}

// effectiveReadyTimeout reports the duration waitForHTTPServer will actually use
// for the given config value. Centralised so logs and errors stay consistent.
func effectiveReadyTimeout(configured time.Duration) time.Duration {
	if configured <= 0 {
		return DefaultReadyTimeout
	}
	return configured
}

// waitForHTTPServer waits for an HTTP MCP server to become available.
// timeout <= 0 falls back to DefaultReadyTimeout so callers can pass the
// per-server override straight through without a nil check.
// Returns an error wrapping ErrReadyTimeout on the ready-poll deadline so
// callers can distinguish it from context cancellation.
func (g *Gateway) waitForHTTPServer(ctx context.Context, client *Client, timeout time.Duration) error {
	timeout = effectiveReadyTimeout(timeout)
	start := time.Now()
	ticker := time.NewTicker(DefaultReadyPollInterval)
	defer ticker.Stop()

	timeoutCh := time.After(timeout)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeoutCh:
			return fmt.Errorf("%w after %s (ready_timeout=%s); set ready_timeout on the server config to wait longer",
				ErrReadyTimeout, time.Since(start).Round(time.Millisecond), timeout)
		case <-ticker.C:
			err := client.Ping(ctx)
			if err == nil {
				g.logger.Debug("MCP server ready", "name", client.Name(), "wait", time.Since(start))
				return nil
			}
			// An auth challenge (or a broker with no grant yet) means the
			// server is reachable but wants authorization; retrying until
			// the ready timeout would only mask that. Surface immediately.
			if isAuthError(err) {
				return err
			}
		}
	}
}

// buildInstructions returns the instructions string for the MCP initialize
// response. The string is built from current server metadata and reflects
// whether code mode is active. Returns "" if no MCP servers are registered.
func (g *Gateway) buildInstructions() string {
	// Get live client data from router first (acquires router lock).
	clients := g.router.Clients()
	toolCounts := make(map[string]int, len(clients))
	for _, c := range clients {
		toolCounts[c.Name()] = len(c.Tools())
	}

	// Read serverMeta and codeModeStr under gateway lock.
	g.mu.RLock()
	isCodeMode := g.codeModeStr == "on"
	mcpServers := make([]string, 0, len(g.serverMeta))
	for name := range g.serverMeta {
		mcpServers = append(mcpServers, name)
	}
	g.mu.RUnlock()

	if len(mcpServers) == 0 {
		return ""
	}

	sort.Strings(mcpServers)

	if isCodeMode {
		totalTools := 0
		for _, name := range mcpServers {
			totalTools += toolCounts[name]
		}
		names := strings.Join(mcpServers, ", ")
		return fmt.Sprintf(
			"gridctl is an MCP gateway running in code mode, aggregating tools from %d downstream MCP servers: %s (%d tools total, hidden behind meta-tools to save context). Two meta-tools are exposed: `search` to discover tools by keyword and `execute` to run JavaScript that calls them via `mcp.callTool(serverName, toolName, args)`. ALWAYS call `search` first (with an empty query to list everything, or a keyword to filter) before attempting any other operation.",
			len(mcpServers), names, totalTools,
		)
	}

	parts := make([]string, len(mcpServers))
	for i, name := range mcpServers {
		parts[i] = fmt.Sprintf("`%s` (%d tools)", name, toolCounts[name])
	}
	return fmt.Sprintf(
		"gridctl is an MCP gateway aggregating tools from %d downstream MCP servers: %s. Use these tools as the primary way to interact with the underlying systems in this session. Tool names are namespaced as `<server>__<tool>` — always invoke them by their full prefixed name (e.g. `%s__example_tool`). Call `tools/list` to see the full inventory.",
		len(mcpServers), strings.Join(parts, ", "), mcpServers[0],
	)
}

// HandleInitialize handles the initialize request. It creates a new session and
// returns both the result and the session so callers can use the session ID.
//
// accessID is the explicit, link-time-assigned client identifier resolved from
// the connection (the `client` query parameter or X-Gridctl-Client-Id header);
// pass "" when the client declared none so the gateway falls back to the
// normalized clientInfo.name for access scoping.
//
// group is the tool group of the endpoint the client connected through
// (/groups/{name}/mcp); "" for the default /mcp endpoint. The transport
// validates the group exists before calling this.
func (g *Gateway) HandleInitialize(params InitializeParams, accessID, group string) (*InitializeResult, *Session, error) {
	// Echo the client's requested protocol version when supported, otherwise
	// counter-offer the latest supported version (per the MCP lifecycle spec,
	// the client decides whether to disconnect). Never fail for version reasons.
	protocolVersion := NegotiateProtocolVersion(params.ProtocolVersion)
	session := g.sessions.Create(params.ClientInfo, accessID, group, protocolVersion)

	caps := Capabilities{
		Tools: &ToolsCapability{
			ListChanged: true,
		},
	}

	// Advertise Prompts and Resources if registry is available
	if g.promptProvider() != nil {
		caps.Prompts = &PromptsCapability{
			ListChanged: true,
		}
		caps.Resources = &ResourcesCapability{
			ListChanged: true,
		}
	}

	// Group endpoints announce a group-suffixed identity so several linked
	// endpoints of the same gateway are distinguishable in clients that
	// display the server-reported name instead of their own config key.
	info := g.ServerInfo()
	if group != "" {
		info.Name = info.Name + "/" + group
	}
	info.Title = info.Name

	return &InitializeResult{
		ProtocolVersion: protocolVersion,
		ServerInfo:      info,
		Capabilities:    caps,
		Instructions:    g.buildInstructions(),
	}, session, nil
}

// HandleToolsList returns all aggregated tools, scoped to what the connecting
// client (resolved from ctx) is allowed to see. When code mode is active,
// returns the two meta-tools instead (the scoped universe is applied to the
// code-mode search/execute path in HandleToolsCall).
func (g *Gateway) HandleToolsList(ctx context.Context) (*ToolsListResult, error) {
	g.mu.RLock()
	cm := g.codeMode
	g.mu.RUnlock()

	if cm != nil {
		return cm.ToolsList(), nil
	}

	tools := g.scopeToolsForContext(ctx, g.router.AggregatedTools())
	// A group session sees its curated, rewritten surface. Client scoping
	// ran first, on canonical names, so a scoped-out tool never reappears
	// under a group rename.
	if group := GroupFromContext(ctx); group != "" {
		tools = g.CurrentGroupPolicy().FilterAndRewrite(group, tools)
	}
	g.logToolCountHint(len(tools))
	return &ToolsListResult{Tools: tools}, nil
}

// HandleToolsListUnscoped returns the full aggregated tool surface, ignoring
// any per-client access scope. It backs operator-facing, informational paths
// (the web console tool list and the optimize schema-token measurement) that
// must see every tool regardless of client scoping. Like HandleToolsList it
// returns the code-mode meta-tools when code mode is active.
func (g *Gateway) HandleToolsListUnscoped() (*ToolsListResult, error) {
	g.mu.RLock()
	cm := g.codeMode
	g.mu.RUnlock()

	if cm != nil {
		return cm.ToolsList(), nil
	}
	return &ToolsListResult{Tools: g.router.AggregatedTools()}, nil
}

// HandleToolsCatalog returns the full downstream tool inventory with each
// tool's raw description and input schema, regardless of code mode. It is
// informational only (served to the web console for tool detail); it never
// affects what tools/list exposes to MCP clients — code mode still hides
// downstream tools behind the meta-tools there.
func (g *Gateway) HandleToolsCatalog() (*ToolsListResult, error) {
	return &ToolsListResult{Tools: g.router.CatalogTools()}, nil
}

// HandleToolsCall routes a tool call to the appropriate MCP server.
// When code mode is active and the tool is a meta-tool, delegates to code mode.
func (g *Gateway) HandleToolsCall(ctx context.Context, params ToolCallParams) (*ToolCallResult, error) {
	g.mu.RLock()
	cm := g.codeMode
	g.mu.RUnlock()

	// Group sessions resolve exposure-layer names to canonical ones at the
	// dispatch boundary, before anything else runs: everything downstream
	// (scoping, gates, routing, telemetry) sees only canonical names. The
	// code-mode meta-tools are exempt from membership (they are the group's
	// window, not members of it); sandboxed inner calls re-enter here with
	// the group still on ctx and are enforced normally.
	if group := GroupFromContext(ctx); group != "" && (cm == nil || !cm.IsMetaTool(params.Name)) {
		canonical, ok := g.CurrentGroupPolicy().ResolveAlias(group, params.Name, g.router.HasTool)
		if !ok {
			g.logger.Debug("tool call denied by group membership",
				"group", group, "tool", params.Name)
			return &ToolCallResult{
				Content: []Content{NewTextContent(g.groupDenialMessage(ctx, group, params.Name))},
				IsError: true,
			}, nil
		}
		params.Name = canonical
	}

	if cm != nil && cm.IsMetaTool(params.Name) {
		// Scope the code-mode tool universe to the connecting client. Code mode
		// sources its search/execute surface from the same aggregated tool set
		// as the direct path, so without this filter a scoped client could
		// discover and call denied tools via code mode.
		allTools := g.scopeToolsForContext(ctx, g.router.AggregatedTools())
		// A group session's code-mode universe is the curated, rewritten
		// surface. Renames stay server-prefixed here: the sandbox ACL and
		// its callTool(server, tool) construction both need names that
		// split into a server half.
		if group := GroupFromContext(ctx); group != "" {
			allTools = g.CurrentGroupPolicy().FilterAndRewritePrefixed(group, allTools)
		}
		return cm.HandleCall(ctx, params, g, allTools)
	}

	// Enforce the per-client access scope on the direct tools/call path. A
	// denied call is rejected before routing; denials are logged at debug.
	if !g.clientAllowsToolCall(ctx, params.Name) {
		g.logger.Debug("tool call denied by client access policy",
			"client", ClientAccessIDFromContext(ctx), "tool", params.Name)
		return &ToolCallResult{
			Content: []Content{NewTextContent(fmt.Sprintf("Error: tool %q is not in this client's access scope", params.Name))},
			IsError: true,
		}, nil
	}

	// Run the pre-call policy gates (rate limits, budgets; see callGates).
	// The first denial short-circuits with the gate's model-readable message.
	// Code-mode inner calls re-enter this function per callTool, so gates
	// cover the sandboxed path without extra wiring.
	if gateCall, gate, decision := g.checkCallGates(ctx, params.Name); gate != "" {
		g.logger.Debug("tool call denied by gate",
			"gate", gate, "client", gateCall.ClientAccessID, "tool", params.Name)
		return &ToolCallResult{
			Content: []Content{NewTextContent(decision.Message)},
			IsError: true,
		}, nil
	}

	// Child span: routing decision.
	tracer := otel.Tracer("gridctl.gateway")
	_, routeSpan := tracer.Start(ctx, "mcp.routing")
	routeSpan.SetAttributes(attribute.String("tool.name", params.Name))
	replica, toolName, err := g.router.RouteToolCallReplica(params.Name)
	if err != nil {
		// Cold-start trigger: if the target server is autoscaled and currently
		// at zero healthy replicas, synchronously spawn one before retrying.
		// Bounded here by the caller's context (tool-call timeout) rather than
		// by a hard-coded deadline so long-spin containers can complete.
		if serverName, _, parseErr := ParsePrefixedTool(params.Name); parseErr == nil {
			if scaler := g.GetAutoscaler(serverName); scaler != nil {
				if cs := scaler.TriggerColdStart(ctx); cs == nil {
					replica, toolName, err = g.router.RouteToolCallReplica(params.Name)
				} else if err == nil {
					err = cs
				}
			}
		}
		if err != nil {
			routeSpan.SetStatus(codes.Error, err.Error())
			routeSpan.End()
			return &ToolCallResult{
				Content: []Content{NewTextContent(fmt.Sprintf("Error: %v", err))},
				IsError: true,
			}, nil
		}
	}
	client := replica.Client()
	replicaID := replica.ID()
	routeSpan.SetAttributes(
		attribute.String("server.name", client.Name()),
		attribute.Int("mcp.replica.id", replicaID),
	)
	routeSpan.End()

	// Reject calls to servers blocked due to schema drift.
	g.blockedMu.RLock()
	isBlocked := g.blockedServers[client.Name()]
	g.blockedMu.RUnlock()
	if isBlocked {
		return &ToolCallResult{
			Content: []Content{NewTextContent(fmt.Sprintf(
				"server %q is blocked pending schema approval; run 'gridctl pins approve %s' to resume",
				client.Name(), client.Name(),
			))},
			IsError: true,
		}, nil
	}

	// Propagate the resolved server name to the root span so the trace-level
	// record (built from root span attrs) carries it for UI filtering.
	if rootSpan := trace.SpanFromContext(ctx); rootSpan.IsRecording() {
		rootSpan.SetAttributes(
			attribute.String("server.name", client.Name()),
			attribute.Int("mcp.replica.id", replicaID),
		)
	}

	// Populate trace ID and replica id on the logger so structured logs are correlated.
	logger := logging.WithReplicaID(g.logger, replicaID)
	if sc := trace.SpanFromContext(ctx).SpanContext(); sc.IsValid() {
		logger = logging.WithTraceID(logger, sc.TraceID().String())
	}

	// Resolve actual transport type from server metadata.
	g.mu.RLock()
	serverCfg, hasMeta := g.serverMeta[client.Name()]
	g.mu.RUnlock()
	networkTransport := resolveNetworkTransport(serverCfg, hasMeta)

	// Child span: downstream client call.
	ctx, span := tracer.Start(ctx, "mcp.client.call_tool")
	defer span.End()
	span.SetAttributes(
		attribute.String("mcp.method.name", "tools/call"),
		attribute.String("server.name", client.Name()),
		attribute.Int("mcp.replica.id", replicaID),
		attribute.String("tool.name", toolName),
		attribute.String("network.transport", networkTransport),
	)

	logger.Info("tool call started", "server", client.Name(), "tool", toolName)
	start := time.Now()

	replica.IncInFlight()
	result, err := client.CallTool(ctx, toolName, params.Arguments)
	replica.DecInFlight()
	duration := time.Since(start)

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		logger.Warn("tool call failed", "server", client.Name(), "tool", toolName, "duration", duration, "error", err)
		return &ToolCallResult{
			Content: []Content{NewTextContent(fmt.Sprintf("Error calling tool: %v", err))},
			IsError: true,
		}, nil
	}

	if result.IsError {
		span.SetStatus(codes.Error, "tool returned error result")
	}
	logger.Info("tool call finished", "server", client.Name(), "tool", toolName, "duration", duration, "is_error", result.IsError)

	// Truncation: clamp oversized results before logging or format conversion
	g.applyTruncation(client.Name(), toolName, result)

	// Format conversion: convert JSON content to the configured output format
	g.applyFormatConversion(ctx, client.Name(), result)

	// Notify the tool-call observer. Observers that implement ClientObserver
	// receive the call synchronously (with ctx + client attribution) so they
	// can return a summary the gateway uses to populate OTel GenAI semantic
	// span attributes; legacy observers fall back to the original async path.
	g.mu.RLock()
	obs := g.toolCallObserver
	g.mu.RUnlock()
	if obs != nil {
		clientID := ClientIDFromContext(ctx)
		if co, ok := obs.(ClientObserver); ok {
			summary := co.ObserveToolCallWithClient(ctx, ToolCallObservation{
				ServerName: client.Name(),
				ReplicaID:  replicaID,
				ClientID:   clientID,
				ToolName:   toolName,
				Arguments:  params.Arguments,
				Result:     result,
			})
			setGenAISpanAttributes(span, client.Name(), toolName, clientID, summary, result)
			// Settle the priced cost into budget windows synchronously, on
			// the same path that computed it. Unpriced calls settle nothing
			// (the attribution gap is documented, never papered over).
			if summary.HasCost {
				g.mu.RLock()
				settler := g.costSettler
				g.mu.RUnlock()
				if settler != nil {
					settler.SettleToolCallCost(ctx, GateCall{
						PrefixedTool:   params.Name,
						ServerName:     client.Name(),
						ClientAccessID: ClientAccessIDFromContext(ctx),
					}, summary.CostUSD)
				}
			}
		} else {
			go obs.ObserveToolCall(client.Name(), replicaID, params.Arguments, result)
		}
	}

	return result, nil
}

// groupDenialMessage builds the model-readable rejection for a call outside
// a group's surface. It names the group and its current tool count (or the
// group's removal) so an agent stops retrying instead of burning tokens.
func (g *Gateway) groupDenialMessage(ctx context.Context, group, toolName string) string {
	policy := g.CurrentGroupPolicy()
	if !policy.Has(group) {
		return fmt.Sprintf("Group %q no longer exists on this gateway. Reconnect or ask the operator which endpoint to use.", group)
	}
	exposed := policy.FilterAndRewrite(group, g.scopeToolsForContext(ctx, g.router.AggregatedTools()))
	return fmt.Sprintf("Tool %q is not in group %q. The group exposes %d tools; list them with tools/list.",
		toolName, group, len(exposed))
}

// checkCallGates runs the installed gates in order against one call. It
// returns the resolved GateCall, the name of the denying gate, and its
// decision; gate == "" means every gate allowed (or none are installed).
func (g *Gateway) checkCallGates(ctx context.Context, prefixedName string) (GateCall, string, GateDecision) {
	g.mu.RLock()
	gates := g.callGates
	g.mu.RUnlock()
	if len(gates) == 0 {
		return GateCall{}, "", GateDecision{Allow: true}
	}
	serverName, _, err := ParsePrefixedTool(prefixedName)
	if err != nil {
		serverName = ""
	}
	call := GateCall{
		PrefixedTool:   prefixedName,
		ServerName:     serverName,
		ClientAccessID: ClientAccessIDFromContext(ctx),
	}
	for _, gate := range gates {
		if decision := gate.CheckToolCall(ctx, call); !decision.Allow {
			return call, gate.Name(), decision
		}
	}
	return call, "", GateDecision{Allow: true}
}

// setGenAISpanAttributes attaches OpenTelemetry GenAI semantic-convention
// attributes to a tool-call span using the values returned by the observer.
// Cache-token attributes are emitted only when the underlying tool result
// reported cache usage; otherwise they are omitted entirely (per the
// March 2026 GenAI spec, zero-valued counters convey "not reported").
//
// gen_ai.cost.usd is gridctl-specific until the GenAI spec defines a cost
// attribute; it is documented in docs/cost-observability.md.
func setGenAISpanAttributes(span trace.Span, serverName, toolName, clientID string, summary ToolCallSummary, result *ToolCallResult) {
	if span == nil || !span.IsRecording() {
		return
	}
	attrs := []attribute.KeyValue{
		attribute.String("mcp.server.name", serverName),
		attribute.String("mcp.tool.name", toolName),
	}
	if clientID != "" {
		attrs = append(attrs, attribute.String("mcp.client.name", clientID))
	}
	if summary.InputTokens > 0 {
		attrs = append(attrs, attribute.Int64("gen_ai.usage.input_tokens", int64(summary.InputTokens)))
	}
	if summary.OutputTokens > 0 {
		attrs = append(attrs, attribute.Int64("gen_ai.usage.output_tokens", int64(summary.OutputTokens)))
	}
	if result != nil && result.Usage != nil {
		if result.Usage.CacheReadTokens > 0 {
			attrs = append(attrs, attribute.Int64("gen_ai.usage.cache_read.input_tokens", int64(result.Usage.CacheReadTokens)))
		}
		if result.Usage.CacheCreationTokens > 0 {
			attrs = append(attrs, attribute.Int64("gen_ai.usage.cache_creation.input_tokens", int64(result.Usage.CacheCreationTokens)))
		}
	}
	if summary.Model != "" {
		attrs = append(attrs, attribute.String("gen_ai.request.model", summary.Model))
	}
	if summary.HasCost {
		attrs = append(attrs, attribute.Float64("gen_ai.cost.usd", summary.CostUSD))
	}
	span.SetAttributes(attrs...)
}

// maxFormatPayloadSize is the maximum text size for format conversion (1MB).
// Payloads larger than this are skipped to prevent excessive memory allocation.
const maxFormatPayloadSize = 1 << 20

// applyFormatConversion converts tool result content to the configured output format.
// It modifies result.Content in place. On any failure, content is left unchanged.
func (g *Gateway) applyFormatConversion(ctx context.Context, serverName string, result *ToolCallResult) {
	if result == nil || result.IsError {
		return
	}

	outputFormat := g.resolveOutputFormat(serverName)
	if outputFormat == "" || outputFormat == "json" || outputFormat == "text" {
		return
	}

	// Child span: format conversion.
	_, fmtSpan := otel.Tracer("gridctl.gateway").Start(ctx, "mcp.format_conversion")
	fmtSpan.SetAttributes(
		attribute.String("server.name", serverName),
		attribute.String("output.format", outputFormat),
	)
	defer fmtSpan.End()

	g.mu.RLock()
	counter := g.tokenCounter
	recorder := g.formatSavingsRecorder
	g.mu.RUnlock()

	var totalOriginalTokens, totalFormattedTokens int

	for i, c := range result.Content {
		if c.Type != "text" || c.Text == "" {
			continue
		}

		if len(c.Text) > maxFormatPayloadSize {
			g.logger.Debug("skipping format conversion for large payload",
				"server", serverName, "size", len(c.Text))
			continue
		}

		var data any
		if err := json.Unmarshal([]byte(c.Text), &data); err != nil {
			continue // Not JSON, leave unchanged
		}

		formatted, err := format.Format(data, outputFormat)
		if err != nil {
			g.logger.Warn("format conversion failed",
				"server", serverName, "format", outputFormat, "error", err)
			continue // Leave unchanged
		}

		// Count tokens before and after
		if counter != nil {
			originalTokens := counter.Count(c.Text)
			formattedTokens := counter.Count(formatted)
			totalOriginalTokens += originalTokens
			totalFormattedTokens += formattedTokens
		}

		result.Content[i].Text = formatted

		g.logger.Info("format conversion applied",
			"server", serverName, "format", outputFormat,
			"original_size", len(c.Text), "formatted_size", len(formatted))
	}

	// Record format savings if any conversion happened
	if recorder != nil && totalOriginalTokens > 0 {
		recorder.RecordFormatSavings(serverName, totalOriginalTokens, totalFormattedTokens)
	}
}

// defaultMaxToolResultBytes is the default maximum tool result size (64KB).
const defaultMaxToolResultBytes = 65536

// applyTruncation truncates oversized tool results before they enter the log buffer.
// It modifies result.Content in place. Results at or under the limit are unchanged.
// Oversized structuredContent is dropped rather than clipped: a byte-clipped JSON
// document is invalid, and clients fall back to Content per the MCP spec. The drop
// is surfaced as a text notice so it is never silent.
func (g *Gateway) applyTruncation(serverName, toolName string, result *ToolCallResult) {
	if result == nil {
		return
	}

	g.mu.RLock()
	limit := g.maxToolResultBytes
	g.mu.RUnlock()

	if limit == 0 {
		limit = defaultMaxToolResultBytes
	}

	for i, c := range result.Content {
		if c.Type != "text" || c.Text == "" {
			continue
		}
		truncated, wasTruncated := format.TruncateResult(c.Text, limit)
		if wasTruncated {
			g.logger.Warn("tool result truncated",
				"tool", toolName, "server", serverName,
				"original_bytes", len(c.Text), "limit_bytes", limit)
			result.Content[i].Text = truncated
		}
	}

	if limit > 0 && len(result.StructuredContent) > limit {
		g.logger.Warn("structured content dropped: exceeds result size limit",
			"tool", toolName, "server", serverName,
			"original_bytes", len(result.StructuredContent), "limit_bytes", limit)
		result.Content = append(result.Content, NewTextContent(fmt.Sprintf(
			"[structuredContent dropped: %d bytes exceeds the %d-byte result limit]",
			len(result.StructuredContent), limit)))
		result.StructuredContent = nil
	}
}

// CallTool implements the ToolCaller interface, allowing components to call
// tools through the gateway without a direct reference to the router.
func (g *Gateway) CallTool(ctx context.Context, name string, arguments map[string]any) (*ToolCallResult, error) {
	return g.HandleToolsCall(ctx, ToolCallParams{
		Name:      name,
		Arguments: arguments,
	})
}

// promptProvider returns the PromptProvider from the router, if registered.
func (g *Gateway) promptProvider() PromptProvider {
	client := g.router.GetClient("registry")
	if client == nil {
		return nil
	}
	if pp, ok := client.(PromptProvider); ok {
		return pp
	}
	return nil
}

// HandlePromptsList returns all active prompts as MCP Prompts.
func (g *Gateway) HandlePromptsList() (*PromptsListResult, error) {
	pp := g.promptProvider()
	if pp == nil {
		return &PromptsListResult{Prompts: []MCPPrompt{}}, nil
	}

	prompts := pp.ListPromptData()
	result := make([]MCPPrompt, len(prompts))
	for i, p := range prompts {
		args := make([]PromptArgument, len(p.Arguments))
		for j, a := range p.Arguments {
			args[j] = PromptArgument{
				Name:        a.Name,
				Description: a.Description,
				Required:    a.Required,
			}
		}
		result[i] = MCPPrompt{
			Name:        p.Name,
			Description: p.Description,
			Arguments:   args,
		}
	}
	return &PromptsListResult{Prompts: result}, nil
}

// HandlePromptsGet returns a specific prompt with argument substitution. The
// ctx carries the originating client id (set on the streamable transport via
// WithClientID) so the prompt-get observer can attribute usage per client.
func (g *Gateway) HandlePromptsGet(ctx context.Context, params PromptsGetParams) (*PromptsGetResult, error) {
	pp := g.promptProvider()
	if pp == nil {
		return nil, fmt.Errorf("registry not available")
	}

	p, err := pp.GetPromptData(params.Name)
	if err != nil {
		return nil, err
	}

	// Perform argument substitution on content
	content := p.Content
	for _, arg := range p.Arguments {
		placeholder := "{{" + arg.Name + "}}"
		value, ok := params.Arguments[arg.Name]
		if !ok {
			if arg.Default != "" {
				value = arg.Default
			} else if arg.Required {
				return nil, fmt.Errorf("required argument %q not provided", arg.Name)
			}
		}
		content = strings.ReplaceAll(content, placeholder, value)
	}

	// Notify the prompt-get observer that this skill was served. Recording is
	// advisory and must never block or fail prompt serving, so it runs on a
	// separate goroutine and the observer swallows its own errors. Fired only
	// on the success path, so a missing required argument does not count as a
	// served skill. The prompt name equals the registry skill's Name.
	g.mu.RLock()
	pObs := g.promptGetObserver
	g.mu.RUnlock()
	if pObs != nil {
		go pObs.ObservePromptGet(PromptGetObservation{
			PromptName: params.Name,
			ClientID:   ClientIDFromContext(ctx),
		})
	}

	return &PromptsGetResult{
		Description: p.Description,
		Messages: []PromptMessage{
			{
				Role:    "user",
				Content: NewTextContent(content),
			},
		},
	}, nil
}

// HandleResourcesList returns prompts as MCP Resources.
func (g *Gateway) HandleResourcesList() (*ResourcesListResult, error) {
	pp := g.promptProvider()
	if pp == nil {
		return &ResourcesListResult{Resources: []MCPResource{}}, nil
	}

	prompts := pp.ListPromptData()
	resources := make([]MCPResource, len(prompts))
	for i, p := range prompts {
		resources[i] = MCPResource{
			URI:         "skills://registry/" + p.Name,
			Name:        p.Name,
			Description: p.Description,
			MimeType:    "text/markdown",
		}
	}
	return &ResourcesListResult{Resources: resources}, nil
}

// HandleResourcesRead returns the content of a prompt resource.
func (g *Gateway) HandleResourcesRead(params ResourcesReadParams) (*ResourcesReadResult, error) {
	pp := g.promptProvider()
	if pp == nil {
		return nil, fmt.Errorf("registry not available")
	}

	// Parse skills://registry/ URI (with legacy prompt:// fallback)
	name := strings.TrimPrefix(params.URI, "skills://registry/")
	if name == params.URI {
		// Try legacy prompt:// scheme for backward compatibility
		name = strings.TrimPrefix(params.URI, "prompt://")
		if name == params.URI {
			return nil, fmt.Errorf("unsupported URI scheme: %s", params.URI)
		}
	}
	if name == "" {
		return nil, fmt.Errorf("empty resource name in URI: %s", params.URI)
	}

	p, err := pp.GetPromptData(name)
	if err != nil {
		return nil, err
	}

	return &ResourcesReadResult{
		Contents: []ResourceContents{
			{
				URI:      params.URI,
				MimeType: "text/markdown",
				Text:     p.Content,
			},
		},
	}, nil
}

// RefreshAllTools refreshes tools from all registered MCP servers.
func (g *Gateway) RefreshAllTools(ctx context.Context) error {
	for _, client := range g.router.Clients() {
		if err := client.RefreshTools(ctx); err != nil {
			g.logger.Warn("failed to refresh tools", "server", client.Name(), "error", err)
		}
	}
	g.router.RefreshTools()
	return nil
}

// MCPServerStatus returns status information about registered MCP servers.
type MCPServerStatus struct {
	Name         string     `json:"name"`
	Transport    Transport  `json:"transport"`
	Endpoint     string     `json:"endpoint,omitempty"`
	ContainerID  string     `json:"containerId,omitempty"`
	Initialized  bool       `json:"initialized"`
	ToolCount    int        `json:"toolCount"`
	Tools        []string   `json:"tools"`
	External     bool       `json:"external"`               // True for external URL servers
	LocalProcess bool       `json:"localProcess"`           // True for local process servers
	SSH          bool       `json:"ssh"`                    // True for SSH servers
	SSHHost      string     `json:"sshHost,omitempty"`      // SSH hostname
	OpenAPI      bool       `json:"openapi"`                // True for OpenAPI servers
	OpenAPISpec  string     `json:"openapiSpec,omitempty"`  // OpenAPI spec location
	OutputFormat string     `json:"outputFormat,omitempty"` // Configured output format (empty = json default)
	Healthy      *bool      `json:"healthy,omitempty"`      // Health check result (nil if not yet checked)
	LastCheck    *time.Time `json:"lastCheck,omitempty"`    // When last health check ran
	HealthError  string     `json:"healthError,omitempty"`  // Error message if unhealthy

	// ProtocolVersion is the MCP protocol version the downstream server
	// reported at initialize. Empty for servers that omit it (lax pre-header
	// implementations), OpenAPI adapters (no MCP handshake), and servers that
	// have not completed a handshake.
	ProtocolVersion string `json:"protocolVersion,omitempty"`

	// RegistrationFailed marks a server that never registered with the
	// gateway (initialize failure, unsupported protocol version, unreachable
	// endpoint). Such entries carry only Name, Healthy=false, and HealthError;
	// they are surfaced so a declared server is never silently absent, and
	// they do not gate readiness.
	RegistrationFailed bool `json:"registrationFailed,omitempty"`

	// ToolWhitelist is the tools: field from the stack YAML for this server.
	// Empty (nil) means no whitelist is configured and the server is exposing
	// every tool it advertises. The UI uses this to distinguish "curated" from
	// "full-list" without needing to diff tools against a hidden pre-filter
	// set that the gateway doesn't retain.
	ToolWhitelist []string `json:"toolWhitelist,omitempty"`

	Replicas []ReplicaStatus `json:"replicas,omitempty"` // Per-replica status; always populated

	// Autoscale is non-nil only for servers with an autoscale block in
	// their stack YAML. Reports current min/max/target/median and the
	// last scaler decision so operators can reason about scale events.
	Autoscale *AutoscaleStatus `json:"autoscale,omitempty"`

	// AuthStatus reports downstream authorization state for external servers
	// with OAuth brokering: "authorized" or "needs_auth". Empty for servers
	// without tracked auth state. A needs_auth server is actionable (run
	// 'gridctl auth login <name>'), not failed, and carries neither
	// RegistrationFailed nor Healthy=false on that account.
	AuthStatus string     `json:"authStatus,omitempty"`
	AuthIssuer string     `json:"authIssuer,omitempty"` // authorization server issuer, when known
	AuthExpiry *time.Time `json:"authExpiry,omitempty"` // access token expiry, when known
}

// ReplicaStatus reports the live state of a single replica within a
// ReplicaSet. Uptime is derived from StartedAt at read time by the consumer.
type ReplicaStatus struct {
	ReplicaID       int        `json:"replicaId"`
	State           string     `json:"state"` // "healthy" | "unhealthy" | "restarting"
	Healthy         bool       `json:"healthy"`
	InFlight        int64      `json:"inFlight"`
	StartedAt       time.Time  `json:"startedAt,omitempty"`
	LastCheck       *time.Time `json:"lastCheck,omitempty"`
	LastHealthy     *time.Time `json:"lastHealthy,omitempty"`
	LastError       string     `json:"lastError,omitempty"`
	RestartAttempts uint32     `json:"restartAttempts,omitempty"`
	NextRetryAt     *time.Time `json:"nextRetryAt,omitempty"`
	PID             int        `json:"pid,omitempty"`
	ContainerID     string     `json:"containerId,omitempty"`
}

// resolveNetworkTransport returns the network.transport attribute value for a
// downstream MCP server based on its registered configuration.
func resolveNetworkTransport(cfg MCPServerConfig, hasMeta bool) string {
	if !hasMeta {
		return string(TransportHTTP)
	}
	if cfg.SSH {
		return "ssh"
	}
	if cfg.LocalProcess {
		return "process"
	}
	if cfg.OpenAPI {
		return string(TransportHTTP)
	}
	switch cfg.Transport {
	case TransportStdio:
		return string(TransportStdio)
	case TransportSSE:
		return string(TransportSSE)
	default:
		return string(TransportHTTP)
	}
}

// buildSSHCommand constructs the ssh command with all options.
func buildSSHCommand(cfg MCPServerConfig) []string {
	args := []string{"ssh", "-o", "BatchMode=yes"}

	// Use strict host key checking when a known_hosts file is provided; otherwise TOFU.
	if cfg.SSHKnownHostsFile != "" {
		args = append(args, "-o", "StrictHostKeyChecking=yes", "-o", "UserKnownHostsFile="+cfg.SSHKnownHostsFile)
	} else {
		args = append(args, "-o", "StrictHostKeyChecking=accept-new")
	}

	// Add identity file if specified
	if cfg.SSHIdentityFile != "" {
		args = append(args, "-i", cfg.SSHIdentityFile)
	}

	// Add port if non-default
	if cfg.SSHPort > 0 && cfg.SSHPort != 22 {
		args = append(args, "-p", strconv.Itoa(cfg.SSHPort))
	}

	// Add jump host (bastion) if specified
	if cfg.SSHJumpHost != "" {
		args = append(args, "-J", cfg.SSHJumpHost)
	}

	// Add user@host
	args = append(args, cfg.SSHUser+"@"+cfg.SSHHost)

	// Add the remote command (as a single argument to be executed remotely)
	args = append(args, cfg.Command...)

	return args
}

// allToolsSource is implemented by clients that can report their full
// pre-whitelist tool set. Every built-in client embeds ClientBase which
// satisfies this interface; mocks or future clients that don't are handled
// by falling back to the filtered Tools().
type allToolsSource interface {
	AllTools() []Tool
}

// allToolsOf returns the downstream server's full tool inventory when the
// client exposes it, or the filtered view as a fallback.
func allToolsOf(client AgentClient) []Tool {
	if ats, ok := client.(allToolsSource); ok {
		return ats.AllTools()
	}
	return client.Tools()
}

// protocolVersionOf returns the protocol version a client's downstream server
// reported at initialize, or "" for clients that do not track one. Type
// assertion (like allToolsSource) keeps this off the AgentClient interface so
// mocks and future clients are not forced to implement it.
func protocolVersionOf(client AgentClient) string {
	if pv, ok := client.(interface{ ProtocolVersion() string }); ok {
		return pv.ProtocolVersion()
	}
	return ""
}

// Status returns status of all registered MCP servers.
// Note: This only returns actual MCP servers, not A2A adapters or other
// clients added directly to the router.
func (g *Gateway) Status() []MCPServerStatus {
	// Gather names from both the router (live replica sets) and the serverMeta
	// map so autoscaled servers without any live replicas still appear.
	g.mu.RLock()
	metaSnapshot := make(map[string]MCPServerConfig, len(g.serverMeta))
	for k, v := range g.serverMeta {
		metaSnapshot[k] = v
	}
	defaultFormat := g.defaultOutputFormat
	g.mu.RUnlock()

	// Build a name set combining router-registered clients and autoscaled
	// metadata entries (which may have empty ReplicaSets at scale-to-zero).
	routerClients := g.router.Clients()
	seen := make(map[string]bool, len(routerClients)+len(metaSnapshot))
	namesOrder := make([]string, 0, len(routerClients)+len(metaSnapshot))
	clientByName := make(map[string]AgentClient, len(routerClients))
	for _, c := range routerClients {
		name := c.Name()
		if _, ok := metaSnapshot[name]; !ok {
			continue
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		namesOrder = append(namesOrder, name)
		clientByName[name] = c
	}
	for name := range metaSnapshot {
		if seen[name] {
			continue
		}
		seen[name] = true
		namesOrder = append(namesOrder, name)
	}

	statuses := make([]MCPServerStatus, 0, len(namesOrder))

	for _, name := range namesOrder {
		meta := metaSnapshot[name]
		client := clientByName[name] // may be nil for autoscaled servers at zero replicas

		// Report every tool the downstream server advertises. When every
		// replica has been reaped (scale-to-zero), client is nil and we
		// fall back to the set's tool cache so the Status surface keeps
		// its shape.
		var tools []Tool
		if client != nil {
			tools = allToolsOf(client)
		} else if set := g.router.GetReplicaSet(name); set != nil {
			tools = set.CachedTools()
		}
		toolNames := make([]string, len(tools))
		for i, t := range tools {
			toolNames[i] = t.Name
		}

		// Resolve effective output format: server override > gateway default
		outputFormat := meta.OutputFormat
		if outputFormat == "" && defaultFormat != "" {
			outputFormat = defaultFormat
		}

		status := MCPServerStatus{
			Name:          name,
			Transport:     meta.Transport,
			Endpoint:      meta.Endpoint,
			ContainerID:   meta.ContainerID,
			Initialized:   client != nil && client.IsInitialized(),
			ToolCount:     len(tools),
			Tools:         toolNames,
			External:      meta.External,
			LocalProcess:  meta.LocalProcess,
			SSH:           meta.SSH,
			SSHHost:       meta.SSHHost,
			OpenAPI:       meta.OpenAPI,
			OutputFormat:  outputFormat,
			ToolWhitelist: meta.Tools,
		}
		if client != nil {
			status.ProtocolVersion = protocolVersionOf(client)
		}
		if meta.OpenAPIConfig != nil {
			status.OpenAPISpec = meta.OpenAPIConfig.Spec
		}

		// Include health status if available
		g.healthMu.RLock()
		if hs, ok := g.health[name]; ok {
			status.Healthy = &hs.Healthy
			status.LastCheck = &hs.LastCheck
			status.HealthError = hs.Error
		}
		g.healthMu.RUnlock()

		g.authStateMu.RLock()
		if st, ok := g.authState[name]; ok {
			status.AuthStatus = st.Status
			status.AuthIssuer = st.Issuer
			status.AuthExpiry = st.Expiry
		}
		g.authStateMu.RUnlock()

		status.Replicas = g.ReplicaStatuses(name)

		if scaler := g.GetAutoscaler(name); scaler != nil {
			st := scaler.Status()
			status.Autoscale = &st
		}

		statuses = append(statuses, status)
	}

	// Servers that failed registration entirely have no router client and no
	// serverMeta entry; surface them as unhealthy rows so they are never a
	// silent absence in the CLI or the UI.
	g.regFailMu.RLock()
	for name, msg := range g.registrationFailures {
		if seen[name] {
			continue
		}
		seen[name] = true
		failed := false
		statuses = append(statuses, MCPServerStatus{
			Name:               name,
			Tools:              []string{},
			Healthy:            &failed,
			HealthError:        msg,
			RegistrationFailed: true,
		})
	}
	g.regFailMu.RUnlock()

	// Servers waiting on authorization have no router client either; surface
	// them as actionable needs-auth rows, never as errors. Healthy stays nil
	// (no health check has meaningfully run against an unauthorized server).
	g.authStateMu.RLock()
	for name, st := range g.authState {
		if seen[name] {
			continue
		}
		seen[name] = true
		statuses = append(statuses, MCPServerStatus{
			Name:       name,
			Tools:      []string{},
			AuthStatus: st.Status,
			AuthIssuer: st.Issuer,
			AuthExpiry: st.Expiry,
		})
	}
	g.authStateMu.RUnlock()

	sort.Slice(statuses, func(i, j int) bool { return statuses[i].Name < statuses[j].Name })
	return statuses
}
