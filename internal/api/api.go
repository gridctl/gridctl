package api

import (
	"context"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gridctl/gridctl/internal/probe"
	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/contexts"
	"github.com/gridctl/gridctl/pkg/dockerclient"
	"github.com/gridctl/gridctl/pkg/limits"
	"github.com/gridctl/gridctl/pkg/logging"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/mcpauth"
	"github.com/gridctl/gridctl/pkg/metrics"
	"github.com/gridctl/gridctl/pkg/pins"
	"github.com/gridctl/gridctl/pkg/provisioner"
	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/gridctl/gridctl/pkg/reload"
	"github.com/gridctl/gridctl/pkg/runtime/docker"
	"github.com/gridctl/gridctl/pkg/tracing"
	"github.com/gridctl/gridctl/pkg/vault"
)

// HTTP status code for locked vault.
const statusLocked = 423

// Server provides the combined API server for gridctl.
type Server struct {
	gateway            *mcp.Gateway
	streamableServer   *mcp.StreamableHTTPServer
	sseServer          *mcp.SSEServer
	staticFS           fs.FS
	dockerClient       dockerclient.DockerClient
	stackName          string
	logBuffer          *logging.LogBuffer
	reloadHandler      *reload.Handler
	provisioners       *provisioner.Registry
	linkServerName     string
	registryServer     *registry.Server
	pinStore           *pins.PinStore
	vaultStore         *vault.Store
	metricsAccumulator *metrics.Accumulator
	traceBuffer        *tracing.Buffer
	stackFile          string
	allowedOrigins     []string
	authType           string
	authToken          string
	authHeader         string

	gatewayAddr   string // e.g. "http://localhost:8180" — used to build MCP config for CLI proxy
	tokenizerName string // active tokenizer mode: "embedded" or "api"

	// modelAttribution returns the server -> model mapping used to price
	// tool calls. Nil (or an empty map) means no server-level cost
	// attribution is configured. Must be safe for concurrent calls.
	modelAttribution func() map[string]string

	// clientModelAttribution returns the client ID -> model mapping
	// (stack.yaml client_models) used to price tool calls by calling
	// client. Nil (or an empty map) means no client-level attribution is
	// configured. Must be safe for concurrent calls.
	clientModelAttribution func() map[string]string

	// declaredServerModels returns the server -> model mapping as DECLARED
	// in stack.yaml (per-server model: only, no default_model folded in).
	// The UI needs the declared value as the edit baseline; the effective
	// value comes from modelAttribution. Must be safe for concurrent calls.
	declaredServerModels func() map[string]string

	// defaultModel returns the gateway-level default_model from stack.yaml,
	// or "" when none is configured. Must be safe for concurrent calls.
	defaultModel func() string

	// limitsStatus returns the budgets/rate-limits consumption snapshot for
	// GET /api/limits. Nil means the builder wired no limits support (the
	// endpoint then reports configured: false). Must be safe for concurrent
	// calls and must reflect hot-reload policy swaps.
	limitsStatus func() limits.StatusReport

	// startWatcher, when set, starts a file watcher on the given stack path.
	// Injected by GatewayBuilder so POST /api/stack/initialize can activate live reload.
	startWatcher func(stackPath string)

	// prober enumerates an MCP server's tool list ephemerally (not registered
	// with the gateway). Nil disables the /api/servers/probe endpoint.
	prober       *probe.Prober
	probeLimiter *probeLimiter

	// oauthBroker handles downstream OAuth for external servers. Nil
	// disables the /api/servers/{name}/auth/* endpoints and the
	// /oauth/callback route.
	oauthBroker *mcpauth.Broker

	// Skill source paths. Empty values fall back to the global defaults
	// (skills.LockFilePath / skills.SkillsConfigPath / skills.UpdateCachePath)
	// so production code is unchanged; tests inject temp paths to stay
	// isolated from $HOME.
	skillLockPath        string
	skillsConfigPath     string
	skillUpdateCachePath string

	// Global-context manager (pkg/contexts), lazily built against the
	// real home directory on first use; tests inject a temp-dir manager
	// via SetContextsManager. Pure file operations — works stackless.
	contextsManager *contexts.Manager
	contextsOnce    sync.Once
	contextsErr     error
}

// NewServer creates a new API server.
func NewServer(gateway *mcp.Gateway, staticFS fs.FS) *Server {
	return &Server{
		gateway:          gateway,
		streamableServer: mcp.NewStreamableHTTPServer(gateway, nil),
		sseServer:        mcp.NewSSEServer(gateway),
		staticFS:         staticFS,
	}
}

// SetDockerClient sets the Docker client for container operations.
func (s *Server) SetDockerClient(cli dockerclient.DockerClient) {
	s.dockerClient = cli
}

// SetStackName sets the stack name for container lookups.
func (s *Server) SetStackName(name string) {
	s.stackName = name
}

// SetLogBuffer sets the log buffer for gateway logs.
func (s *Server) SetLogBuffer(buffer *logging.LogBuffer) {
	s.logBuffer = buffer
}

// LogBuffer returns the log buffer for gateway logs.
func (s *Server) LogBuffer() *logging.LogBuffer {
	return s.logBuffer
}

// SetReloadHandler sets the reload handler for hot reload support.
func (s *Server) SetReloadHandler(h *reload.Handler) {
	s.reloadHandler = h
}

// ReloadHandler returns the reload handler.
func (s *Server) ReloadHandler() *reload.Handler {
	return s.reloadHandler
}

// SetAllowedOrigins sets the CORS allowed origins for the server.
func (s *Server) SetAllowedOrigins(origins []string) {
	s.allowedOrigins = origins
	s.streamableServer.SetAllowedOrigins(origins)
}

// SetAuth configures authentication for the server.
// When configured, all requests (except /health and /ready) must include a valid token.
func (s *Server) SetAuth(authType, token, header string) {
	s.authType = authType
	s.authToken = token
	s.authHeader = header
}

// SetOAuthBroker wires the downstream OAuth broker: enables the
// /api/servers/{name}/auth/* endpoints and mounts the /oauth/callback
// route (outside the inbound auth middleware).
func (s *Server) SetOAuthBroker(b *mcpauth.Broker) {
	s.oauthBroker = b
}

// SetProvisionerRegistry sets the provisioner registry for client detection.
func (s *Server) SetProvisionerRegistry(r *provisioner.Registry, serverName string) {
	s.provisioners = r
	s.linkServerName = serverName
}

// SetRegistryServer sets the registry server for skill management.
func (s *Server) SetRegistryServer(r *registry.Server) {
	s.registryServer = r
}

// SetPinStore sets the pin store for schema pin management.
func (s *Server) SetPinStore(ps *pins.PinStore) {
	s.pinStore = ps
}

// PinStore returns the wired pin store, or nil when schema pinning is not
// configured. Exposed so callers and tests can confirm whether pin management
// is active.
func (s *Server) PinStore() *pins.PinStore {
	return s.pinStore
}

// SetVaultStore sets the vault store for secrets management.
func (s *Server) SetVaultStore(v *vault.Store) {
	s.vaultStore = v
}

// SetStackFile sets the path to the stack YAML file for spec endpoints.
func (s *Server) SetStackFile(path string) {
	s.stackFile = path
}

// SetMetricsAccumulator sets the token metrics accumulator.
func (s *Server) SetMetricsAccumulator(acc *metrics.Accumulator) {
	s.metricsAccumulator = acc
}

// MetricsAccumulator returns the token metrics accumulator.
func (s *Server) MetricsAccumulator() *metrics.Accumulator {
	return s.metricsAccumulator
}

// SetTraceBuffer sets the distributed tracing ring buffer.
func (s *Server) SetTraceBuffer(buf *tracing.Buffer) {
	s.traceBuffer = buf
}

// SetGatewayAddr sets the base URL of this server (e.g. "http://localhost:8180").
// Used to build the MCP config JSON for CLI proxy sessions so the claude CLI can
// reach gridctl's MCP gateway at <gatewayAddr>/sse.
func (s *Server) SetGatewayAddr(addr string) {
	s.gatewayAddr = addr
}

// SetTokenizerName sets the active tokenizer mode for display in /api/status.
func (s *Server) SetTokenizerName(name string) {
	s.tokenizerName = name
}

// SetModelAttribution sets a getter for the server -> model mapping used to
// price tool calls. The getter (rather than a static map) lets hot reloads of
// `model:` / `default_model:` reach handlers without re-wiring; it must be
// safe for concurrent calls. Feeds the optimize model stats and the
// /api/status cost_attribution flag.
func (s *Server) SetModelAttribution(get func() map[string]string) {
	s.modelAttribution = get
}

// SetClientModelAttribution sets a getter for the client ID -> model mapping
// (stack.yaml client_models) used to price tool calls by calling client.
// Same contract as SetModelAttribution: the getter follows hot reloads and
// must be safe for concurrent calls. Feeds the /api/status client_models
// exposure, the cost_attribution flag, and the /api/clients model field.
func (s *Server) SetClientModelAttribution(get func() map[string]string) {
	s.clientModelAttribution = get
}

// modelAttributionMap returns the current server -> model mapping, or nil
// when no attribution getter is wired or nothing is configured.
func (s *Server) modelAttributionMap() map[string]string {
	if s.modelAttribution == nil {
		return nil
	}
	return s.modelAttribution()
}

// clientModelAttributionMap returns the current client -> model mapping, or
// nil when no attribution getter is wired or nothing is configured.
func (s *Server) clientModelAttributionMap() map[string]string {
	if s.clientModelAttribution == nil {
		return nil
	}
	return s.clientModelAttribution()
}

// SetDeclaredServerModels sets a getter for the server -> model mapping as
// declared in stack.yaml (per-server model: only). Same contract as
// SetModelAttribution: the getter follows hot reloads and must be safe for
// concurrent calls. Feeds the /api/status server model exposure.
func (s *Server) SetDeclaredServerModels(get func() map[string]string) {
	s.declaredServerModels = get
}

// SetDefaultModel sets a getter for the gateway-level default_model. The
// getter follows hot reloads and must be safe for concurrent calls. Feeds
// the /api/status default_model exposure.
func (s *Server) SetDefaultModel(get func() string) {
	s.defaultModel = get
}

// declaredServerModelsMap returns the current declared server -> model
// mapping, or nil when no getter is wired or nothing is configured.
func (s *Server) declaredServerModelsMap() map[string]string {
	if s.declaredServerModels == nil {
		return nil
	}
	return s.declaredServerModels()
}

// defaultModelValue returns the current gateway default_model, or "" when no
// getter is wired or none is configured.
func (s *Server) defaultModelValue() string {
	if s.defaultModel == nil {
		return ""
	}
	return s.defaultModel()
}

// effectiveClientModels and effectiveServerModels derive the read-time
// effective-model + provenance maps from the live accumulator snapshots.
// Return nil when no accumulator is wired or no traffic has been observed.
func (s *Server) effectiveClientModels() map[string]EffectiveModel {
	if s.metricsAccumulator == nil {
		return nil
	}
	return deriveEffectiveModels(s.metricsAccumulator.Snapshot().PerClient, s.metricsAccumulator.CostSnapshot().PerClientModels)
}

func (s *Server) effectiveServerModels() map[string]EffectiveModel {
	if s.metricsAccumulator == nil {
		return nil
	}
	return deriveEffectiveModels(s.metricsAccumulator.Snapshot().PerServer, s.metricsAccumulator.CostSnapshot().PerServerModels)
}

// SetStartWatcher sets a callback that activates live-reload file watching for
// the given stack path. Called by POST /api/stack/initialize after cold-loading.
func (s *Server) SetStartWatcher(fn func(stackPath string)) {
	s.startWatcher = fn
}

// SetSkillSourcePaths overrides the skill lock-file and skills.yaml paths used
// by /api/skills/* handlers. Empty values keep the global defaults.
func (s *Server) SetSkillSourcePaths(lockPath, configPath string) {
	s.skillLockPath = lockPath
	s.skillsConfigPath = configPath
}

// SetSkillUpdateCachePath overrides the skill update cache path. Empty keeps
// the global default. Tests use this to isolate from $HOME/.gridctl/cache.
func (s *Server) SetSkillUpdateCachePath(path string) {
	s.skillUpdateCachePath = path
}

// RegistryServer returns the registry server.
func (s *Server) RegistryServer() *registry.Server {
	return s.registryServer
}

// Close performs cleanup of the API server's managed resources.
func (s *Server) Close() {
	if s.sseServer != nil {
		s.sseServer.Close()
	}
	if s.streamableServer != nil {
		s.streamableServer.Close()
	}
	if s.gateway != nil {
		s.gateway.Close()
	}
}

// Handler returns the main HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// MCP endpoints - Streamable HTTP (POST/GET/DELETE) and legacy SSE negotiation
	mux.Handle("/mcp", s.streamableServer)
	// Group endpoints: the same streamable transport serving a curated
	// surface. The wrapper 404s unknown groups BEFORE any MCP handling so a
	// typo'd link URL never creates a session, and injects the group into
	// the request context for initialize to bind onto the session.
	mux.HandleFunc("/groups/{name}/mcp", s.handleGroupMCP)
	mux.HandleFunc("GET /groups/{name}/sse", s.handleGroupSSE) // Streamable HTTP transport
	mux.Handle("/sse", s.sseServer)                            // Legacy negotiation redirect
	mux.HandleFunc("/message", s.sseServer.HandleMessage)      // Legacy endpoint (410 Gone)

	// API endpoints
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/sessions", s.handleSessions)

	mux.HandleFunc("GET /api/mcp-servers/{name}/logs", s.handleMCPServerLogs)
	mux.HandleFunc("POST /api/mcp-servers/{name}/restart", s.handleMCPServerRestart)
	mux.HandleFunc("PUT /api/mcp-servers/tools", s.handleSetServerToolsBatch)
	mux.HandleFunc("PUT /api/mcp-servers/{name}/tools", s.handleSetServerTools)
	mux.HandleFunc("PUT /api/mcp-servers/{name}/model", s.handleSetServerModel)
	mux.HandleFunc("PUT /api/gateway/default-model", s.handleSetDefaultModel)
	mux.HandleFunc("/api/mcp-servers", s.handleMCPServers)
	mux.HandleFunc("GET /api/auth/servers", s.handleAuthServers)
	mux.HandleFunc("POST /api/servers/{name}/auth/login", s.handleAuthLogin)
	mux.HandleFunc("GET /api/servers/{name}/auth/wait", s.handleAuthWait)
	mux.HandleFunc("POST /api/servers/{name}/auth/manual", s.handleAuthManual)
	mux.HandleFunc("POST /api/servers/{name}/auth/logout", s.handleAuthLogout)
	mux.HandleFunc("POST /api/servers/{name}/auth/reset", s.handleAuthReset)
	mux.HandleFunc("/api/tools", s.handleTools)
	mux.HandleFunc("GET /api/tools/catalog", s.handleToolsCatalog)
	mux.HandleFunc("GET /api/tools/usage", s.handleToolsUsage)
	mux.HandleFunc("GET /api/skills/usage", s.handleSkillsUsage)
	mux.HandleFunc("/api/logs", s.handleGatewayLogs)
	mux.HandleFunc("/api/metrics/tokens", s.handleMetricsTokens)
	mux.HandleFunc("/api/metrics/cost", s.handleMetricsCost)
	mux.HandleFunc("GET /api/optimize", s.handleOptimize)
	mux.HandleFunc("GET /api/traces", s.handleTraces)
	mux.HandleFunc("GET /api/traces/{traceId}", s.handleTraces)
	mux.HandleFunc("GET /api/traces/{traceId}/otlp", s.handleTraceOTLP)
	mux.HandleFunc("POST /api/clients/{slug}/scope/preview", s.handleClientScopePreview)
	mux.HandleFunc("PUT /api/clients/{slug}/scope", s.handleSetClientScope)
	mux.HandleFunc("PUT /api/clients/{slug}/model", s.handleSetClientModel)
	mux.HandleFunc("POST /api/clients/{slug}/link", s.handleLinkClient)
	mux.HandleFunc("DELETE /api/clients/{slug}/link", s.handleUnlinkClient)
	mux.HandleFunc("POST /api/clients/{slug}/link/preview", s.handleLinkPreview)
	mux.HandleFunc("/api/clients", s.handleClients)
	mux.HandleFunc("GET /api/pricing/models", s.handlePricingModels)
	mux.HandleFunc("/api/reload", s.handleReload)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/ready", s.handleReady)

	// Pins endpoints
	mux.HandleFunc("GET /api/pins", s.handleListPins)
	mux.HandleFunc("GET /api/pins/{server}", s.handleGetServerPins)
	mux.HandleFunc("GET /api/pins/{server}/diff", s.handlePinsDiff)
	mux.HandleFunc("POST /api/pins/{server}/approve", s.handleApprovePins)
	mux.HandleFunc("DELETE /api/pins/{server}", s.handleResetPins)

	// Global context (pkg/contexts) — pure file operations, stackless-safe.
	mux.HandleFunc("GET /api/context", s.handleContextGet)
	mux.HandleFunc("PUT /api/context", s.handleContextPut)
	mux.HandleFunc("GET /api/context/scan", s.handleContextScan)
	mux.HandleFunc("POST /api/context/init", s.handleContextInit)
	mux.HandleFunc("POST /api/context/sync", s.handleContextSync)
	mux.HandleFunc("POST /api/context/adopt/{slug}", s.handleContextAdopt)
	mux.HandleFunc("POST /api/context/unsync/{slug}", s.handleContextUnsync)
	mux.HandleFunc("GET /api/context/diff/{slug}", s.handleContextDiff)

	// Variable store endpoints — canonical /api/var/* surface plus a
	// deprecated /api/vault/* alias that wears Deprecation/Sunset headers.
	// Both register the same handler functions so behaviour is identical;
	// only the response headers differ on the deprecated path.
	registerVarRoutes := func(prefix string, deprecated bool) {
		wrap := func(canonical string, h http.HandlerFunc) http.HandlerFunc {
			if !deprecated {
				return h
			}
			return deprecatedVaultHandler(canonical, h)
		}
		mux.HandleFunc("GET "+prefix, wrap("/api/var", s.handleVaultList))
		mux.HandleFunc("POST "+prefix, wrap("/api/var", s.handleVaultCreate))
		mux.HandleFunc("POST "+prefix+"/import", wrap("/api/var/import", s.handleVaultImport))
		mux.HandleFunc("GET "+prefix+"/status", wrap("/api/var/status", s.handleVaultStatus))
		mux.HandleFunc("GET "+prefix+"/usage", wrap("/api/var/usage", s.handleVariableUsage))
		mux.HandleFunc("POST "+prefix+"/unlock", wrap("/api/var/unlock", s.handleVaultUnlock))
		mux.HandleFunc("POST "+prefix+"/lock", wrap("/api/var/lock", s.handleVaultLock))
		mux.HandleFunc("GET "+prefix+"/sets", wrap("/api/var/sets", s.handleVaultSetsList))
		mux.HandleFunc("POST "+prefix+"/sets", wrap("/api/var/sets", s.handleVaultSetsCreate))
		mux.HandleFunc("DELETE "+prefix+"/sets/{name}", wrap("/api/var/sets/{name}", s.handleVaultSetsDelete))
		mux.HandleFunc("GET "+prefix+"/{key}", wrap("/api/var/{key}", s.handleVaultKeyGet))
		mux.HandleFunc("PUT "+prefix+"/{key}", wrap("/api/var/{key}", s.handleVaultKeyPut))
		mux.HandleFunc("DELETE "+prefix+"/{key}", wrap("/api/var/{key}", s.handleVaultKeyDelete))
		mux.HandleFunc("PUT "+prefix+"/{key}/set", wrap("/api/var/{key}/set", s.handleVaultAssignSet))
	}
	registerVarRoutes("/api/var", false)
	registerVarRoutes("/api/vault", true)

	// Stack spec endpoints
	mux.HandleFunc("POST /api/stack/validate", s.handleStackValidate)
	mux.HandleFunc("GET /api/stack/plan", s.handleStackPlan)
	mux.HandleFunc("GET /api/stack/health", s.handleStackHealth)
	mux.HandleFunc("GET /api/stack/spec", s.handleStackSpec)
	mux.HandleFunc("GET /api/stack/export", s.handleStackExport)
	mux.HandleFunc("GET /api/stack/recipes", s.handleStackRecipes)
	mux.HandleFunc("GET /api/catalog", s.handleCatalog)
	mux.HandleFunc("GET /api/limits", s.handleLimits)
	mux.HandleFunc("GET /api/groups", s.handleGroups)
	mux.HandleFunc("POST /api/stack/append", s.handleStackAppend)
	mux.HandleFunc("POST /api/stack/initialize", s.handleStackInitialize)
	mux.HandleFunc("PATCH /api/stack/telemetry", s.handlePatchStackTelemetry)

	// Telemetry persistence endpoints — opt-in disk persistence inventory
	// and wipe; the per-server PATCH lives under /api/mcp-servers/{name}/.
	mux.HandleFunc("PATCH /api/mcp-servers/{name}/telemetry", s.handlePatchServerTelemetry)
	mux.HandleFunc("GET /api/telemetry/inventory", s.handleGetTelemetryInventory)
	mux.HandleFunc("DELETE /api/telemetry", s.handleDeleteTelemetry)

	// Stack library endpoints
	mux.HandleFunc("GET /api/stacks", s.handleStacksList)
	mux.HandleFunc("POST /api/stacks", s.handleStacksSave)

	// Skills endpoints (remote skill import)
	mux.HandleFunc("GET /api/skills/sources", s.handleSkillSourcesList)
	mux.HandleFunc("POST /api/skills/sources", s.handleSkillSourceAdd)
	mux.HandleFunc("POST /api/skills/sources/update", s.handleSkillSourcesSyncAll)
	mux.HandleFunc("GET /api/skills/updates", s.handleSkillUpdates)
	mux.HandleFunc("DELETE /api/skills/sources/{name}", s.handleSkillSourceRemove)
	mux.HandleFunc("POST /api/skills/sources/{name}/check", s.handleSkillSourceCheck)
	mux.HandleFunc("POST /api/skills/sources/{name}/update", s.handleSkillSourceUpdate)
	// Preview accepts either GET (query params, no auth) or POST (JSON body,
	// with optional auth) so the wizard can pass credentials without
	// leaking them into query strings or browser history.
	mux.HandleFunc("GET /api/skills/sources/{name}/preview", s.handleSkillSourcePreview)
	mux.HandleFunc("POST /api/skills/sources/{name}/preview", s.handleSkillSourcePreview)
	// Per-skill reconciliation: compare with upstream, detach to local-only, or
	// reset (force-overwrite with backup) a single tracked skill.
	mux.HandleFunc("GET /api/skills/sources/{name}/skills/{skill}/diff", s.handleSkillDiff)
	mux.HandleFunc("POST /api/skills/sources/{name}/skills/{skill}/detach", s.handleSkillDetach)
	mux.HandleFunc("POST /api/skills/sources/{name}/skills/{skill}/reset", s.handleSkillReset)

	// Wizard endpoints
	mux.HandleFunc("GET /api/wizard/drafts", s.handleWizardDraftsList)
	mux.HandleFunc("POST /api/wizard/drafts", s.handleWizardDraftCreate)
	mux.HandleFunc("DELETE /api/wizard/drafts/{id}", s.handleWizardDraftDelete)

	// Server probe — ephemeral tool enumeration used by the wizard's
	// "Discover tools" flow for servers not yet loaded in the stack.
	mux.HandleFunc("POST /api/servers/probe", s.handleProbe)

	// Registry endpoints
	mux.HandleFunc("GET /api/registry/status", s.handleRegistryStatus)
	mux.HandleFunc("GET /api/registry/skills", s.handleRegistrySkillsList)
	mux.HandleFunc("POST /api/registry/skills", s.handleRegistrySkillCreate)
	mux.HandleFunc("POST /api/registry/skills/validate", s.handleRegistryValidate)
	mux.HandleFunc("PUT /api/registry/skills/batch", s.handleRegistrySkillsBatch)
	mux.HandleFunc("GET /api/registry/skills/{name}", s.handleRegistrySkillGet)
	mux.HandleFunc("PUT /api/registry/skills/{name}", s.handleRegistrySkillPut)
	mux.HandleFunc("DELETE /api/registry/skills/{name}", s.handleRegistrySkillDelete)
	mux.HandleFunc("POST /api/registry/skills/{name}/activate", s.handleRegistrySkillActivate)
	mux.HandleFunc("POST /api/registry/skills/{name}/disable", s.handleRegistrySkillDisable)
	mux.HandleFunc("GET /api/registry/skills/{name}/files", s.handleRegistrySkillFileList)
	mux.HandleFunc("GET /api/registry/skills/{name}/files/{path...}", s.handleRegistrySkillFileGet)
	mux.HandleFunc("PUT /api/registry/skills/{name}/files/{path...}", s.handleRegistrySkillFilePut)
	mux.HandleFunc("DELETE /api/registry/skills/{name}/files/{path...}", s.handleRegistrySkillFileDelete)

	// Static files (UI) - served at root
	if s.staticFS != nil {
		fileServer := http.FileServer(http.FS(s.staticFS))
		mux.Handle("/", spaHandler(fileServer, s.staticFS))
	}

	handler := authMiddleware(s.authType, s.authToken, s.authHeader, mux)

	// The OAuth authorization callback mounts OUTSIDE the inbound auth
	// middleware: the browser performing the redirect carries no gateway
	// bearer token, and the route authenticates via its single-use state
	// parameter instead. Nothing else escapes the middleware.
	if s.oauthBroker != nil {
		inner := handler
		outer := http.NewServeMux()
		outer.Handle("GET "+mcpauth.CallbackPath, s.oauthBroker.CallbackHandler())
		outer.Handle("/", inner)
		handler = outer
	}

	var extraHeaders []string
	if s.authHeader != "" && s.authHeader != "Authorization" {
		extraHeaders = append(extraHeaders, s.authHeader)
	}
	handler = corsMiddleware(s.allowedOrigins, extraHeaders, handler)
	return handler
}

// handleStatus returns the overall gateway status.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := struct {
		Gateway    ServerInfo               `json:"gateway"`
		MCPServers []MCPServerStatus        `json:"mcp-servers"`
		Resources  []ResourceStatus         `json:"resources"`
		Sessions   int                      `json:"sessions"`
		Registry   *registry.RegistryStatus `json:"registry,omitempty"`
		CodeMode   string                   `json:"code_mode,omitempty"`
		TokenUsage *metrics.TokenUsage      `json:"token_usage,omitempty"`
		Cost       *metrics.CostUsage       `json:"cost,omitempty"`
		// CostAttribution reports whether any client or server has a model
		// configured for pricing. False lets the UI explain why cost stays
		// empty (set `client_models:` or `model:` in stack.yaml) instead of
		// showing a bare $0.00.
		CostAttribution bool `json:"cost_attribution,omitempty"`
		// ClientModels is the declared client ID -> model pricing map from
		// stack.yaml client_models. Omitted when empty. The UI uses it to
		// label per-client cost with the model it was priced as.
		ClientModels map[string]string `json:"client_models,omitempty"`
		// ServerModels is the EFFECTIVE server -> model pricing map (a
		// server's own model: with gateway default_model folded in).
		// Omitted when no server-tier attribution is configured. The
		// declared per-server value rides on each MCPServerStatus.Model.
		ServerModels map[string]string `json:"server_models,omitempty"`
		// DefaultModel is the gateway-level default_model from stack.yaml.
		// Omitted when not configured. Lets the UI render "inherits
		// default: <id>" provenance for servers without their own model.
		DefaultModel string `json:"default_model,omitempty"`
		// EffectiveClientModels and EffectiveServerModels report which model
		// actually priced each client's / server's recorded cost, with
		// provenance (declared | mixed | none). Derived read-only from the
		// accumulator's model histograms; they describe which declaration
		// gridctl applied, not what the upstream client ran. Omitted when no
		// traffic has been observed.
		EffectiveClientModels map[string]EffectiveModel `json:"effective_client_models,omitempty"`
		EffectiveServerModels map[string]EffectiveModel `json:"effective_server_models,omitempty"`
		StackName             string                    `json:"stack_name,omitempty"`
	}{
		Gateway: ServerInfo{
			Name:      s.gateway.ServerInfo().Name,
			Version:   s.gateway.ServerInfo().Version,
			Tokenizer: s.tokenizerName,
		},
		MCPServers: s.getMCPServerStatuses(),
		Resources:  s.getResourceStatuses(r.Context()),
		Sessions:   s.gateway.SessionCount(),
	}
	// Only expose stack_name when a user-defined stack is loaded.
	// The embedded gateway uses "gridctl" as its default name even in stackless
	// mode, so stackFile is the authoritative indicator.
	if s.stackFile != "" {
		status.StackName = s.stackName
	}
	if cm := s.gateway.CodeModeStatus(); cm != "off" {
		status.CodeMode = cm
	}
	if s.registryServer != nil && s.registryServer.HasContent() {
		regStatus := s.registryServer.Store().Status()
		status.Registry = &regStatus
	}
	if s.metricsAccumulator != nil {
		snap := s.metricsAccumulator.Snapshot()
		status.TokenUsage = &snap
		cost := s.metricsAccumulator.CostSnapshot()
		if !costSnapshotIsZero(cost) {
			status.Cost = &cost
		}
		status.EffectiveClientModels = deriveEffectiveModels(snap.PerClient, cost.PerClientModels)
		status.EffectiveServerModels = deriveEffectiveModels(snap.PerServer, cost.PerServerModels)
	}
	status.ClientModels = s.clientModelAttributionMap()
	status.ServerModels = s.modelAttributionMap()
	status.DefaultModel = s.defaultModelValue()
	status.CostAttribution = len(status.ServerModels) > 0 || len(status.ClientModels) > 0

	writeJSON(w, status)
}

// costSnapshotIsZero reports whether a CostUsage has any recorded cost data.
// We omit the /api/status `cost` field entirely when zero so consumers see
// the same JSON shape they did before per-call cost was recorded.
func costSnapshotIsZero(c metrics.CostUsage) bool {
	return c.Session.TotalUSD == 0 && len(c.PerServer) == 0 && len(c.PerReplica) == 0 && len(c.PerClient) == 0
}

// handleSessions returns active MCP session count and IDs.
// GET /api/sessions
func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	response := struct {
		Count    int      `json:"count"`
		Sessions []string `json:"sessions"`
	}{
		Count:    s.streamableServer.SessionCount(),
		Sessions: s.streamableServer.SessionIDs(),
	}
	writeJSON(w, response)
}

// handleMCPServers returns information about registered MCP servers.
func (s *Server) handleMCPServers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	writeJSON(w, s.gateway.Status())
}

// handleTools returns all aggregated tools.
func (s *Server) handleTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	result, _ := s.gateway.HandleToolsListUnscoped()
	// Always serialize an empty inventory as [], never null: web consumers
	// index into the list (e.g. fuzzy search) where a null would throw.
	if result != nil && result.Tools == nil {
		result.Tools = []mcp.Tool{}
	}
	writeJSON(w, result)
}

// handleToolsCatalog returns the full downstream tool inventory (each tool's
// raw description + input schema) for the web console, regardless of code
// mode. Read-only and informational: it does not change what MCP clients see
// from tools/list.
func (s *Server) handleToolsCatalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	result, _ := s.gateway.HandleToolsCatalog()
	// Always serialize an empty catalog as [], never null (see handleTools).
	if result != nil && result.Tools == nil {
		result.Tools = []mcp.Tool{}
	}
	writeJSON(w, result)
}

// ServerInfo mirrors the mcp.ServerInfo type for API responses.
type ServerInfo struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Tokenizer string `json:"tokenizer,omitempty"`
}

// MCPServerStatus mirrors the mcp.MCPServerStatus type for API responses.
type MCPServerStatus struct {
	Name          string   `json:"name"`
	Transport     string   `json:"transport"`
	Endpoint      string   `json:"endpoint"`
	Initialized   bool     `json:"initialized"`
	ToolCount     int      `json:"toolCount"`
	Tools         []string `json:"tools"`
	External      bool     `json:"external"`
	LocalProcess  bool     `json:"localProcess"`
	SSH           bool     `json:"ssh"`
	SSHHost       string   `json:"sshHost,omitempty"`
	OpenAPI       bool     `json:"openapi"`
	OpenAPISpec   string   `json:"openapiSpec,omitempty"`
	OutputFormat  string   `json:"outputFormat,omitempty"`
	Healthy       *bool    `json:"healthy,omitempty"`
	LastCheck     *string  `json:"lastCheck,omitempty"`
	HealthError   string   `json:"healthError,omitempty"`
	ToolWhitelist []string `json:"toolWhitelist,omitempty"`
	// ProtocolVersion is the MCP protocol version the downstream server
	// reported at initialize; empty for lax servers and OpenAPI adapters.
	ProtocolVersion string `json:"protocolVersion,omitempty"`
	// RegistrationFailed marks a server that never registered with the
	// gateway; the UI shows it as failed instead of omitting the node.
	RegistrationFailed bool `json:"registrationFailed,omitempty"`
	// Model is the pricing model DECLARED on this server in stack.yaml
	// (model: field only — a gateway default_model is not folded in here).
	// Empty when the server inherits the default or has no attribution.
	Model string `json:"model,omitempty"`
	// EffectiveModel reports which model actually priced this server's
	// recorded cost, with provenance (declared | mixed | none). Read-only
	// and derived from observed cost; nil when the server has no traffic.
	EffectiveModel *EffectiveModel `json:"effectiveModel,omitempty"`

	Replicas  []mcp.ReplicaStatus  `json:"replicas,omitempty"`
	Autoscale *mcp.AutoscaleStatus `json:"autoscale,omitempty"`

	// AuthStatus reports downstream authorization state ("authorized" or
	// "needs_auth"); empty for servers without tracked auth state.
	AuthStatus string     `json:"authStatus,omitempty"`
	AuthIssuer string     `json:"authIssuer,omitempty"`
	AuthExpiry *time.Time `json:"authExpiry,omitempty"`
}

func (s *Server) getMCPServerStatuses() []MCPServerStatus {
	mcpStatuses := s.gateway.Status()
	declaredModels := s.declaredServerModelsMap()
	effective := s.effectiveServerModels()
	statuses := make([]MCPServerStatus, len(mcpStatuses))
	for i, ms := range mcpStatuses {
		status := MCPServerStatus{
			Name:               ms.Name,
			Transport:          string(ms.Transport),
			Endpoint:           ms.Endpoint,
			Initialized:        ms.Initialized,
			ToolCount:          ms.ToolCount,
			Tools:              ms.Tools,
			External:           ms.External,
			LocalProcess:       ms.LocalProcess,
			SSH:                ms.SSH,
			SSHHost:            ms.SSHHost,
			OpenAPI:            ms.OpenAPI,
			OpenAPISpec:        ms.OpenAPISpec,
			OutputFormat:       ms.OutputFormat,
			Healthy:            ms.Healthy,
			HealthError:        ms.HealthError,
			ToolWhitelist:      ms.ToolWhitelist,
			ProtocolVersion:    ms.ProtocolVersion,
			RegistrationFailed: ms.RegistrationFailed,
			Model:              declaredModels[ms.Name],
			Replicas:           ms.Replicas,
			Autoscale:          ms.Autoscale,
			AuthStatus:         ms.AuthStatus,
			AuthIssuer:         ms.AuthIssuer,
			AuthExpiry:         ms.AuthExpiry,
		}
		if ms.LastCheck != nil {
			ts := ms.LastCheck.Format(time.RFC3339)
			status.LastCheck = &ts
		}
		if em, ok := effective[ms.Name]; ok {
			status.EffectiveModel = &em
		}
		statuses[i] = status
	}
	return statuses
}

// writeJSON writes a JSON response.
func writeJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(data)
}

// writeJSONError writes a JSON error response.
func writeJSONError(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// corsMiddleware adds CORS headers to responses based on allowed origins.
// extraHeaders are additional headers to include in Access-Control-Allow-Headers.
func corsMiddleware(allowedOrigins []string, extraHeaders []string, next http.Handler) http.Handler {
	originSet := make(map[string]bool, len(allowedOrigins))
	allowAll := false
	for _, o := range allowedOrigins {
		if o == "*" {
			allowAll = true
		}
		originSet[o] = true
	}
	allowHeaders := "Content-Type, Authorization"
	for _, h := range extraHeaders {
		allowHeaders += ", " + h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && (allowAll || originSet[origin]) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", allowHeaders)
			w.Header().Set("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// spaHandler wraps the file server to handle SPA routing.
func spaHandler(fileServer http.Handler, staticFS fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/" {
			path = "index.html"
		} else if path[0] == '/' {
			path = path[1:]
		}

		// Check if file exists
		if _, err := fs.Stat(staticFS, path); err != nil {
			// File doesn't exist, serve index.html for SPA routing
			r.URL.Path = "/"
		}

		fileServer.ServeHTTP(w, r)
	})
}

// ResourceStatus contains status information for a resource container.
type ResourceStatus struct {
	Name   string `json:"name"`
	Image  string `json:"image"`
	Status string `json:"status"`
}

// getResourceStatuses returns status of all resource containers. A listing
// failure is logged and reported as an empty slice so /api/status stays
// serveable during a runtime outage; the warning distinguishes that outage
// from a stack with no resources.
func (s *Server) getResourceStatuses(ctx context.Context) []ResourceStatus {
	if s.dockerClient == nil || s.stackName == "" {
		return []ResourceStatus{}
	}

	containers, err := docker.ListManagedContainers(ctx, s.dockerClient, s.stackName)
	if err != nil {
		slog.Warn("status: failed to list resource containers; reporting none",
			"stack", s.stackName, "error", err)
		return []ResourceStatus{}
	}

	var resources []ResourceStatus
	for _, c := range containers {
		// Only include resource containers (not MCP servers)
		if resName, ok := c.Labels[docker.LabelResource]; ok {
			status := "stopped"
			if c.State == "running" {
				status = "running"
			} else if c.State != "exited" {
				status = c.State
			}

			resources = append(resources, ResourceStatus{
				Name:   resName,
				Image:  c.Image,
				Status: status,
			})
		}
	}

	return resources
}

// handleMCPServerLogs returns structured logs from the global buffer filtered by server name.
// GET /api/mcp-servers/{name}/logs?lines=100
func (s *Server) handleMCPServerLogs(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	if s.logBuffer == nil {
		writeJSON(w, []logging.BufferedEntry{})
		return
	}

	// Get number of lines from query param (default 100)
	lines := 100
	if linesParam := r.URL.Query().Get("lines"); linesParam != "" {
		if n, err := strconv.Atoi(linesParam); err == nil && n > 0 {
			lines = n
		}
	}

	// Over-fetch to account for filtering — most entries may belong to other servers
	all := s.logBuffer.GetRecent(lines * 10)
	filtered := make([]logging.BufferedEntry, 0)
	for _, entry := range all {
		if entry.Attrs["server"] == name {
			filtered = append(filtered, entry)
		}
	}
	if len(filtered) > lines {
		filtered = filtered[len(filtered)-lines:]
	}
	writeJSON(w, filtered)
}

// handleMCPServerRestart restarts an individual MCP server connection.
func (s *Server) handleMCPServerRestart(w http.ResponseWriter, r *http.Request) {
	serverName := r.PathValue("name")

	if err := s.gateway.RestartMCPServer(r.Context(), serverName); err != nil {
		if strings.Contains(err.Error(), "unknown MCP server") {
			writeJSONError(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSONError(w, "Restart failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]string{"status": "restarted", "server": serverName})
}

// handleGatewayLogs returns structured logs from the gateway log buffer.
// GET /api/logs?lines=100&level=error,warn,info
func (s *Server) handleGatewayLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.logBuffer == nil {
		writeJSON(w, []logging.BufferedEntry{})
		return
	}

	// Get number of lines from query param (default 100)
	lines := 100
	if linesParam := r.URL.Query().Get("lines"); linesParam != "" {
		if n, err := strconv.Atoi(linesParam); err == nil && n > 0 {
			lines = n
		}
	}

	entries := s.logBuffer.GetRecent(lines)

	// Filter by level if specified
	if levelParam := r.URL.Query().Get("level"); levelParam != "" {
		levels := make(map[string]bool)
		for _, l := range strings.Split(levelParam, ",") {
			levels[strings.ToUpper(strings.TrimSpace(l))] = true
		}

		filtered := make([]logging.BufferedEntry, 0, len(entries))
		for _, entry := range entries {
			if levels[entry.Level] {
				filtered = append(filtered, entry)
			}
		}
		entries = filtered
	}

	if entries == nil {
		entries = []logging.BufferedEntry{}
	}
	writeJSON(w, entries)
}

// handleMetricsTokens handles token metrics requests.
// GET /api/metrics/tokens?range=1h — returns historical time-series data
// DELETE /api/metrics/tokens — clears all token metrics
func (s *Server) handleMetricsTokens(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetMetricsTokens(w, r)
	case http.MethodDelete:
		s.handleDeleteMetricsTokens(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleGetMetricsTokens returns historical token metrics.
// GET /api/metrics/tokens?range=1h
func (s *Server) handleGetMetricsTokens(w http.ResponseWriter, r *http.Request) {
	if s.metricsAccumulator == nil {
		writeJSON(w, metrics.TimeSeriesResponse{
			Range:     "1h",
			Interval:  "1m",
			Points:    []metrics.DataPoint{},
			PerServer: map[string][]metrics.DataPoint{},
		})
		return
	}

	rangeParam := r.URL.Query().Get("range")
	duration := parseRange(rangeParam)

	result := s.metricsAccumulator.Query(duration)

	// Ensure non-nil slices for JSON serialization
	if result.Points == nil {
		result.Points = []metrics.DataPoint{}
	}
	if result.PerServer == nil {
		result.PerServer = map[string][]metrics.DataPoint{}
	}
	for name, points := range result.PerServer {
		if points == nil {
			result.PerServer[name] = []metrics.DataPoint{}
		}
	}

	writeJSON(w, result)
}

// handleDeleteMetricsTokens clears all token metrics.
// DELETE /api/metrics/tokens
func (s *Server) handleDeleteMetricsTokens(w http.ResponseWriter, _ *http.Request) {
	if s.metricsAccumulator == nil {
		writeJSON(w, map[string]string{"status": "ok", "message": "Token metrics cleared"})
		return
	}

	s.metricsAccumulator.Clear()
	writeJSON(w, map[string]string{"status": "ok", "message": "Token metrics cleared"})
}

// handleMetricsCost handles cost metrics requests.
// GET /api/metrics/cost?range=1h&per_client=true — historical cost time-series
// DELETE /api/metrics/cost — clears recorded cost data without touching tokens
func (s *Server) handleMetricsCost(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetMetricsCost(w, r)
	case http.MethodDelete:
		s.handleDeleteMetricsCost(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleGetMetricsCost returns historical cost-over-time data with the same
// time-range vocabulary as /api/metrics/tokens. Setting per_client=true on
// the query string includes a `per_client` map grouping cost by the
// originating MCP client; otherwise the response carries only the
// session-wide series and per-server breakdown.
func (s *Server) handleGetMetricsCost(w http.ResponseWriter, r *http.Request) {
	emptyResponse := metrics.CostTimeSeriesResponse{
		Range:     "1h",
		Interval:  "1m",
		Points:    []metrics.CostDataPoint{},
		PerServer: map[string][]metrics.CostDataPoint{},
	}
	if s.metricsAccumulator == nil {
		writeJSON(w, emptyResponse)
		return
	}

	rangeParam := r.URL.Query().Get("range")
	duration := parseRange(rangeParam)
	includeClients := parseBoolQuery(r, "per_client", false)

	var result metrics.CostTimeSeriesResponse
	if includeClients {
		result = s.metricsAccumulator.QueryCostByClient(duration)
	} else {
		result = s.metricsAccumulator.QueryCost(duration)
	}

	// Ensure non-nil slices for stable JSON serialization.
	if result.Points == nil {
		result.Points = []metrics.CostDataPoint{}
	}
	if result.PerServer == nil {
		result.PerServer = map[string][]metrics.CostDataPoint{}
	}
	for name, points := range result.PerServer {
		if points == nil {
			result.PerServer[name] = []metrics.CostDataPoint{}
		}
	}
	if includeClients {
		if result.PerClient == nil {
			result.PerClient = map[string][]metrics.CostDataPoint{}
		}
		for name, points := range result.PerClient {
			if points == nil {
				result.PerClient[name] = []metrics.CostDataPoint{}
			}
		}
	}

	writeJSON(w, result)
}

// handleDeleteMetricsCost clears recorded cost data while leaving token
// counters and the format-savings tally intact.
func (s *Server) handleDeleteMetricsCost(w http.ResponseWriter, _ *http.Request) {
	if s.metricsAccumulator == nil {
		writeJSON(w, map[string]string{"status": "ok", "message": "Cost metrics cleared"})
		return
	}
	s.metricsAccumulator.ClearCost()
	writeJSON(w, map[string]string{"status": "ok", "message": "Cost metrics cleared"})
}

// parseBoolQuery returns the boolean value of a query parameter, falling
// back to def when the parameter is unset or unparseable. "1", "true",
// "yes", "on" (case-insensitive) all read as true.
func parseBoolQuery(r *http.Request, key string, def bool) bool {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}

// parseRange converts a range query parameter to a duration.
func parseRange(s string) time.Duration {
	switch s {
	case "30m":
		return 30 * time.Minute
	case "1h":
		return time.Hour
	case "6h":
		return 6 * time.Hour
	case "24h":
		return 24 * time.Hour
	case "7d":
		return 7 * 24 * time.Hour
	default:
		return time.Hour // Default to 1h
	}
}

// handleReload triggers a configuration reload from disk.
// POST /api/reload
func (s *Server) handleReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.reloadHandler == nil {
		writeJSONError(w, "Reload not enabled (start with --watch flag)", http.StatusServiceUnavailable)
		return
	}

	result, err := s.reloadHandler.Reload(r.Context())
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if !result.Success {
		w.WriteHeader(http.StatusBadRequest)
	}
	writeJSON(w, result)
}

// handleHealth returns 200 OK when the daemon is alive and serving requests.
// This is a liveness check - it returns OK immediately without checking MCP server status.
// Use /ready for a full readiness check that verifies all MCP servers are initialized.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

// handleReady returns 200 OK only when a stack is loaded and all MCP servers
// are connected and initialized. Returns 503 when no stack is loaded (stackless
// mode) or when any MCP server has not yet initialized.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Not ready until a stack is loaded
	if s.stackFile == "" {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("No stack loaded"))
		return
	}

	// Check all MCP servers are initialized. Autoscaled servers that have
	// scaled to zero deliberately have no client and therefore report
	// Initialized=false; they can cold-start on demand and are not a failed
	// state, so do not reject them here. Registration failures do not gate
	// readiness either: before they were surfaced in Status() the daemon
	// reported ready with those servers silently absent, and a permanently
	// failed server must not wedge /ready at 503 (apply's readiness wait
	// would time out even though the gateway serves every healthy server).
	for _, status := range s.gateway.Status() {
		if status.Initialized {
			continue
		}
		if status.Autoscale != nil && len(status.Replicas) == 0 {
			continue
		}
		if status.RegistrationFailed {
			continue
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("MCP server not initialized: " + status.Name))
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

// ClientStatus describes an LLM client's detection and link state.
type ClientStatus struct {
	Name       string `json:"name"`
	Slug       string `json:"slug"`
	Detected   bool   `json:"detected"`
	Linked     bool   `json:"linked"`
	Transport  string `json:"transport"`
	ConfigPath string `json:"configPath,omitempty"`
	// Model is the client's declared pricing model from stack.yaml
	// client_models, when present. Pricing attribution only — it carries no
	// access-control meaning and is independent of EffectiveScope.
	Model string `json:"model,omitempty"`
	// EffectiveModel reports which model actually priced this client's
	// recorded cost, with provenance (declared | mixed | none). Read-only
	// and derived from observed cost; nil when the client has no traffic.
	EffectiveModel *EffectiveModel `json:"effectiveModel,omitempty"`
	// EffectiveScope is the backend-computed per-client tool access scope when a
	// `clients:` block is configured: the servers and prefixed tools this client
	// can reach. nil when no access scoping is in effect, so the frontend can
	// distinguish "unscoped (legacy)" from "scoped to nothing".
	EffectiveScope *mcp.ClientScopeResult `json:"effectiveScope,omitempty"`
	// Declared reports whether the stack's link: block lists this client;
	// LinkEntry carries the declared options when it does. Desired state,
	// distinct from Linked (actual config-file state).
	Declared  bool           `json:"declared,omitempty"`
	LinkEntry *LinkEntryInfo `json:"linkEntry,omitempty"`
}

// LinkEntryInfo is the wire shape of a declared link: entry's options.
type LinkEntryInfo struct {
	Group    string `json:"group,omitempty"`
	ClientID string `json:"clientId,omitempty"`
	Name     string `json:"name,omitempty"`
}

// handleClients returns detected LLM clients and their link status.
// GET /api/clients
func (s *Server) handleClients(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.provisioners == nil {
		writeJSON(w, []ClientStatus{})
		return
	}

	serverName := s.linkServerName
	if serverName == "" {
		serverName = "gridctl"
	}

	scopingOn := s.gateway != nil && s.gateway.ClientAccessConfigured()
	clientModels := s.clientModelAttributionMap()
	effective := s.effectiveClientModels()

	declared := make(map[string]LinkEntryInfo)
	for _, e := range s.declaredLinks() {
		declared[e.Client] = LinkEntryInfo{Group: e.Group, ClientID: e.ClientID, Name: e.Name}
	}

	infos := s.provisioners.AllClientInfo(serverName)
	statuses := make([]ClientStatus, 0, len(infos))
	for _, info := range infos {
		status := ClientStatus{
			Name:       info.Name,
			Slug:       info.Slug,
			Detected:   info.Detected,
			Linked:     info.Linked,
			Transport:  info.Transport,
			ConfigPath: info.ConfigPath,
			Model:      clientModels[info.Slug],
		}
		if entry, ok := declared[info.Slug]; ok {
			status.Declared = true
			e := entry
			status.LinkEntry = &e
			// A declared group or name override writes a different entry name
			// than the default this handler polls, so additionally check the
			// resolved name. OR, not replace: an entry under either name means
			// the client reaches this gateway, and flipping Linked to false
			// while a default-name entry exists would lie to every consumer.
			if resolved := (config.LinkEntry{Client: info.Slug, Group: entry.Group, Name: entry.Name}).EffectiveName(); resolved != serverName && info.Detected && !status.Linked {
				if prov, ok := s.provisioners.FindBySlug(info.Slug); ok {
					if linked, err := prov.IsLinked(info.ConfigPath, resolved); err == nil && linked {
						status.Linked = true
					}
				}
			}
		}
		if em, ok := effective[info.Slug]; ok {
			status.EffectiveModel = &em
		}
		// Surface the backend-computed effective scope keyed on the client's
		// stable identifier (its slug, which is what `gridctl link` assigns and
		// what stack.yaml profiles are keyed on).
		if scopingOn {
			scope := s.gateway.ClientScope(info.Slug)
			status.EffectiveScope = &scope
		}
		statuses = append(statuses, status)
	}

	writeJSON(w, statuses)
}
