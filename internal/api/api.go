package api

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gridctl/gridctl/internal/probe"
	"github.com/gridctl/gridctl/pkg/agent"
	"github.com/gridctl/gridctl/pkg/agent/compose"
	"github.com/gridctl/gridctl/pkg/agent/dev/devserver"
	"github.com/gridctl/gridctl/pkg/agent/persist"
	agentruntime "github.com/gridctl/gridctl/pkg/agent/runtime"
	"github.com/gridctl/gridctl/pkg/dockerclient"
	"github.com/gridctl/gridctl/pkg/logging"
	"github.com/gridctl/gridctl/pkg/mcp"
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
	gateway          *mcp.Gateway
	streamableServer *mcp.StreamableHTTPServer
	sseServer        *mcp.SSEServer
	staticFS       fs.FS
	dockerClient   dockerclient.DockerClient
	stackName      string
	logBuffer      *logging.LogBuffer
	reloadHandler  *reload.Handler
	provisioners   *provisioner.Registry
	linkServerName string
	registryServer *registry.Server
	pinStore           *pins.PinStore
	vaultStore         *vault.Store
	metricsAccumulator *metrics.Accumulator
	traceBuffer        *tracing.Buffer
	stackFile          string
	allowedOrigins     []string
	authType       string
	authToken      string
	authHeader     string

	gatewayAddr        string // e.g. "http://localhost:8180" — used to build MCP config for CLI proxy
	tokenizerName      string // active tokenizer mode: "embedded" or "api"

	// startWatcher, when set, starts a file watcher on the given stack path.
	// Injected by GatewayBuilder so POST /api/stack/initialize can activate live reload.
	startWatcher func(stackPath string)

	// prober enumerates an MCP server's tool list ephemerally (not registered
	// with the gateway). Nil disables the /api/servers/probe endpoint.
	prober       *probe.Prober
	probeLimiter *probeLimiter

	// Skill source paths. Empty values fall back to the global defaults
	// (skills.LockFilePath / skills.SkillsConfigPath) so production code is
	// unchanged; tests inject temp paths to stay isolated from $HOME.
	skillLockPath    string
	skillsConfigPath string

	// Playground LLM provider. When nil, /api/playground/chat returns
	// a 400 explaining that the user needs to configure a vault key
	// and restart the daemon. The provider is the route layer
	// (agent/llm/gateway.Provider) chosen at apply-time by the
	// gateway builder so a single API server can serve mixed
	// provider models.
	playgroundProvider agent.ChatModel
	playgroundOnce     sync.Once
	playgroundSvc      *playgroundService

	// Agent runtime persistence + approval surface. SetAgentRunStore
	// and SetAgentApprovalRegistry wire these at apply time; nil
	// values cause /api/agent/runs/* handlers to return 503.
	agentRunStore         *persist.Store
	agentApprovalRegistry *compose.Registry

	// Agent IDE dev surface. SetAgentDevServer wires a project-rooted
	// devserver.Server that powers the /api/agent/dev/* endpoints
	// (skills list, AST graphs, file-watcher SSE). Nil → 503.
	agentDevServer *devserver.Server

	// agentRuntime is the unified runtime aggregate; when non-nil,
	// /api/agent/* and /api/playground/* handlers prefer it over the
	// per-component fields above. SetAgentRuntime installs it; the
	// per-component setters are retained as test-fixture wrappers.
	agentRuntime *agentruntime.Runtime
}

// NewServer creates a new API server.
func NewServer(gateway *mcp.Gateway, staticFS fs.FS) *Server {
	s := &Server{
		gateway:          gateway,
		streamableServer: mcp.NewStreamableHTTPServer(gateway, nil),
		sseServer:        mcp.NewSSEServer(gateway),
		staticFS:         staticFS,
	}
	// Wire the run-persister adapter so MCP tools/call against typed
	// skills lands in ~/.gridctl/runs/<run_id>.jsonl alongside Run
	// Launcher invocations. The adapter resolves the store and
	// registry lazily, so SetRegistryServer / SetAgentRunStore /
	// SetAgentRuntime can land in any order without losing wiring.
	gateway.SetRunPersister(newRunPersisterAdapter(s))
	return s
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

// SetPlaygroundProvider injects the LLM provider used by
// /api/playground/{chat,stream}. Passing nil disables the playground
// (the chat endpoint returns a clear error). The provider is typically
// the prefix-routing agent/llm/gateway.Provider built at apply-time by
// pkg/controller from the vault keys present.
//
// SetAgentRuntime takes precedence over this setter at read time;
// retained for test fixtures.
func (s *Server) SetPlaygroundProvider(p agent.ChatModel) {
	s.playgroundProvider = p
}

// SetAgentRuntime installs the unified runtime aggregate. When set, the
// per-component setters below are ignored at read time. Wire-time only:
// the controller calls this once during apply before HTTP serving
// starts; field access is unsynchronised to match the rest of the
// per-server setter pattern.
func (s *Server) SetAgentRuntime(rt *agentruntime.Runtime) {
	s.agentRuntime = rt
}

// The four accessors below all share the same shape: prefer the runtime
// aggregate's component when set, otherwise fall back to the legacy
// per-field value the matching setter wrote. Production wiring goes
// through SetAgentRuntime; the per-field setters survive for tests
// that need to wire a single component without building a full
// Runtime.

func (s *Server) runStore() *persist.Store {
	if s.agentRuntime != nil {
		if store := s.agentRuntime.RunStore(); store != nil {
			return store
		}
	}
	return s.agentRunStore
}

func (s *Server) approvalRegistry() *compose.Registry {
	if s.agentRuntime != nil {
		if reg := s.agentRuntime.ApprovalRegistry(); reg != nil {
			return reg
		}
	}
	return s.agentApprovalRegistry
}

func (s *Server) chatProvider() agent.ChatModel {
	if s.agentRuntime != nil {
		if m := s.agentRuntime.ChatModel(); m != nil {
			return m
		}
	}
	return s.playgroundProvider
}

func (s *Server) devServer() *devserver.Server {
	if s.agentRuntime != nil {
		if d := s.agentRuntime.DevServer(); d != nil {
			return d
		}
	}
	return s.agentDevServer
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
	mux.Handle("/mcp", s.streamableServer)                 // Streamable HTTP transport
	mux.Handle("/sse", s.sseServer)                        // Legacy negotiation redirect
	mux.HandleFunc("/message", s.sseServer.HandleMessage)  // Legacy endpoint (410 Gone)

	// API endpoints
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/sessions", s.handleSessions)
	mux.HandleFunc("GET /api/agents/{name}/logs", s.handleAgentLogs)

	// Agent runtime — typed runs persisted as JSONL at
	// ~/.gridctl/runs/<run_id>.jsonl. List and inspect surfaces are
	// always available when a store is wired; resume/approve route
	// through the in-process approval registry.
	mux.HandleFunc("GET /api/agent/runs", s.handleAgentRunsList)
	mux.HandleFunc("POST /api/agent/runs", s.handleAgentRunsLaunch)
	// Global live tail across every run — wired before the {run_id}
	// surface so the more-specific event subscription doesn't shadow
	// the path. The per-run SSE remains the source of truth for any
	// single run; this stream is the cross-run observability surface.
	mux.HandleFunc("GET /api/agent/runs/events/stream", s.handleAgentRunsEventsStream)
	mux.HandleFunc("GET /api/agent/runs/{run_id}", s.handleAgentRunGet)
	mux.HandleFunc("GET /api/agent/runs/{run_id}/events", s.handleAgentRunEvents)
	mux.HandleFunc("POST /api/agent/runs/{run_id}/resume", s.handleAgentRunResume)
	mux.HandleFunc("POST /api/agent/runs/{run_id}/approve", s.handleAgentRunApprove)

	// Agent IDE dev surface — code-canon AST graphs + file-watcher SSE.
	// All paths funnel through handleAgentDev so SetAgentDevServer
	// can swap implementations without re-binding routes.
	mux.HandleFunc("/api/agent/dev/", s.handleAgentDev)
	mux.HandleFunc("POST /api/mcp-servers/{name}/restart", s.handleMCPServerRestart)
	mux.HandleFunc("PUT /api/mcp-servers/{name}/tools", s.handleSetServerTools)
	mux.HandleFunc("/api/mcp-servers", s.handleMCPServers)
	mux.HandleFunc("/api/tools", s.handleTools)
	mux.HandleFunc("/api/logs", s.handleGatewayLogs)
	mux.HandleFunc("/api/metrics/tokens", s.handleMetricsTokens)
	mux.HandleFunc("/api/metrics/cost", s.handleMetricsCost)
	mux.HandleFunc("GET /api/optimize", s.handleOptimize)
	mux.HandleFunc("GET /api/traces", s.handleTraces)
	mux.HandleFunc("GET /api/traces/{traceId}", s.handleTraces)
	mux.HandleFunc("/api/clients", s.handleClients)
	mux.HandleFunc("/api/reload", s.handleReload)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/ready", s.handleReady)

	// Pins endpoints
	mux.HandleFunc("GET /api/pins", s.handleListPins)
	mux.HandleFunc("GET /api/pins/{server}", s.handleGetServerPins)
	mux.HandleFunc("POST /api/pins/{server}/approve", s.handleApprovePins)
	mux.HandleFunc("DELETE /api/pins/{server}", s.handleResetPins)

	// Vault endpoints
	mux.HandleFunc("GET /api/vault", s.handleVaultList)
	mux.HandleFunc("POST /api/vault", s.handleVaultCreate)
	mux.HandleFunc("POST /api/vault/import", s.handleVaultImport)
	mux.HandleFunc("GET /api/vault/status", s.handleVaultStatus)
	mux.HandleFunc("POST /api/vault/unlock", s.handleVaultUnlock)
	mux.HandleFunc("POST /api/vault/lock", s.handleVaultLock)
	mux.HandleFunc("GET /api/vault/sets", s.handleVaultSetsList)
	mux.HandleFunc("POST /api/vault/sets", s.handleVaultSetsCreate)
	mux.HandleFunc("DELETE /api/vault/sets/{name}", s.handleVaultSetsDelete)
	mux.HandleFunc("GET /api/vault/{key}", s.handleVaultKeyGet)
	mux.HandleFunc("PUT /api/vault/{key}", s.handleVaultKeyPut)
	mux.HandleFunc("DELETE /api/vault/{key}", s.handleVaultKeyDelete)
	mux.HandleFunc("PUT /api/vault/{key}/set", s.handleVaultAssignSet)

	// Stack spec endpoints
	mux.HandleFunc("POST /api/stack/validate", s.handleStackValidate)
	mux.HandleFunc("GET /api/stack/plan", s.handleStackPlan)
	mux.HandleFunc("GET /api/stack/health", s.handleStackHealth)
	mux.HandleFunc("GET /api/stack/spec", s.handleStackSpec)
	mux.HandleFunc("GET /api/stack/export", s.handleStackExport)
	mux.HandleFunc("GET /api/stack/secrets-map", s.handleStackSecretsMap)
	mux.HandleFunc("GET /api/stack/recipes", s.handleStackRecipes)
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
	mux.HandleFunc("GET /api/skills/updates", s.handleSkillUpdates)
	mux.HandleFunc("DELETE /api/skills/sources/{name}", s.handleSkillSourceRemove)
	mux.HandleFunc("POST /api/skills/sources/{name}/check", s.handleSkillSourceCheck)
	mux.HandleFunc("POST /api/skills/sources/{name}/update", s.handleSkillSourceUpdate)
	// Preview accepts either GET (query params, no auth) or POST (JSON body,
	// with optional auth) so the wizard can pass credentials without
	// leaking them into query strings or browser history.
	mux.HandleFunc("GET /api/skills/sources/{name}/preview", s.handleSkillSourcePreview)
	mux.HandleFunc("POST /api/skills/sources/{name}/preview", s.handleSkillSourcePreview)

	// Wizard endpoints
	mux.HandleFunc("GET /api/wizard/drafts", s.handleWizardDraftsList)
	mux.HandleFunc("POST /api/wizard/drafts", s.handleWizardDraftCreate)
	mux.HandleFunc("DELETE /api/wizard/drafts/{id}", s.handleWizardDraftDelete)

	// Server probe — ephemeral tool enumeration used by the wizard's
	// "Discover tools" flow for servers not yet loaded in the topology.
	mux.HandleFunc("POST /api/servers/probe", s.handleProbe)

	// Playground — LLM provider abstraction surface. /auth probes the
	// vault for provider keys; /chat kicks off an inference into a
	// session channel; /stream is the SSE the React client subscribes to.
	mux.HandleFunc("POST /api/playground/auth", s.handlePlaygroundAuth)
	mux.HandleFunc("POST /api/playground/chat", s.handlePlaygroundChat)
	mux.HandleFunc("GET /api/playground/stream", s.handlePlaygroundStream)

	// Registry endpoints
	mux.HandleFunc("GET /api/registry/status", s.handleRegistryStatus)
	mux.HandleFunc("GET /api/registry/skills", s.handleRegistrySkillsList)
	mux.HandleFunc("POST /api/registry/skills", s.handleRegistrySkillCreate)
	mux.HandleFunc("POST /api/registry/skills/validate", s.handleRegistryValidate)
	mux.HandleFunc("GET /api/registry/skills/{name}", s.handleRegistrySkillGet)
	mux.HandleFunc("PUT /api/registry/skills/{name}", s.handleRegistrySkillPut)
	mux.HandleFunc("DELETE /api/registry/skills/{name}", s.handleRegistrySkillDelete)
	mux.HandleFunc("POST /api/registry/skills/{name}/activate", s.handleRegistrySkillActivate)
	mux.HandleFunc("POST /api/registry/skills/{name}/disable", s.handleRegistrySkillDisable)
	mux.HandleFunc("POST /api/registry/skills/{name}/test", s.handleRegistrySkillTest)
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
		StackName  string                   `json:"stack_name,omitempty"`
	}{
		Gateway: ServerInfo{
			Name:      s.gateway.ServerInfo().Name,
			Version:   s.gateway.ServerInfo().Version,
			Tokenizer: s.tokenizerName,
		},
		MCPServers: s.getMCPServerStatuses(),
		Resources:  s.getResourceStatuses(),
		Sessions: s.gateway.SessionCount(),
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
		if cost := s.metricsAccumulator.CostSnapshot(); !costSnapshotIsZero(cost) {
			status.Cost = &cost
		}
	}

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

	result, _ := s.gateway.HandleToolsList()
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

	Replicas  []mcp.ReplicaStatus  `json:"replicas,omitempty"`
	Autoscale *mcp.AutoscaleStatus `json:"autoscale,omitempty"`
}

func (s *Server) getMCPServerStatuses() []MCPServerStatus {
	mcpStatuses := s.gateway.Status()
	statuses := make([]MCPServerStatus, len(mcpStatuses))
	for i, ms := range mcpStatuses {
		status := MCPServerStatus{
			Name:          ms.Name,
			Transport:     string(ms.Transport),
			Endpoint:      ms.Endpoint,
			Initialized:   ms.Initialized,
			ToolCount:     ms.ToolCount,
			Tools:         ms.Tools,
			External:      ms.External,
			LocalProcess:  ms.LocalProcess,
			SSH:           ms.SSH,
			SSHHost:       ms.SSHHost,
			OpenAPI:       ms.OpenAPI,
			OpenAPISpec:   ms.OpenAPISpec,
			OutputFormat:  ms.OutputFormat,
			Healthy:       ms.Healthy,
			HealthError:   ms.HealthError,
			ToolWhitelist: ms.ToolWhitelist,
			Replicas:      ms.Replicas,
			Autoscale:     ms.Autoscale,
		}
		if ms.LastCheck != nil {
			ts := ms.LastCheck.Format(time.RFC3339)
			status.LastCheck = &ts
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
	allowHeaders := "Content-Type, X-Agent-Name, Authorization"
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

// getResourceStatuses returns status of all resource containers.
func (s *Server) getResourceStatuses() []ResourceStatus {
	if s.dockerClient == nil || s.stackName == "" {
		return []ResourceStatus{}
	}

	ctx := context.Background()
	containers, err := docker.ListManagedContainers(ctx, s.dockerClient, s.stackName)
	if err != nil {
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

// handleAgentLogs returns structured logs from the global buffer filtered by server name.
// GET /api/agents/{name}/logs?lines=100
func (s *Server) handleAgentLogs(w http.ResponseWriter, r *http.Request) {
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
	// state, so do not reject them here.
	for _, status := range s.gateway.Status() {
		if status.Initialized {
			continue
		}
		if status.Autoscale != nil && len(status.Replicas) == 0 {
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

	infos := s.provisioners.AllClientInfo(serverName)
	statuses := make([]ClientStatus, 0, len(infos))
	for _, info := range infos {
		statuses = append(statuses, ClientStatus{
			Name:       info.Name,
			Slug:       info.Slug,
			Detected:   info.Detected,
			Linked:     info.Linked,
			Transport:  info.Transport,
			ConfigPath: info.ConfigPath,
		})
	}

	writeJSON(w, statuses)
}
