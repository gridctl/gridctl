package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gridctl/gridctl/internal/probe"
	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/pins"
)

// Concurrency caps. Per-session keeps a single misbehaving tab from swamping
// the daemon; the global cap is a defense-in-depth check against scripts or
// tests calling the endpoint in a loop.
const (
	probeSessionCap = 3
	probeGlobalCap  = 10
)

// probeRequestMaxBytes caps the decoded body size. The wire shape is small —
// there is no reason to accept megabyte-scale probe configs.
const probeRequestMaxBytes = 64 * 1024

// probeSessionKey identifies a client for per-session concurrency accounting.
// Built from X-Session-ID if present, else falling back to the remote address.
// Stable-enough for a single browser tab — overloaded tabs still hit the cap.
const sessionHeader = "X-Session-ID"

// probeLimiter enforces the two-tier concurrency cap. Zero value is not
// usable — call newProbeLimiter.
type probeLimiter struct {
	mu          sync.Mutex
	perSession  map[string]int
	globalInUse atomic.Int32
}

func newProbeLimiter() *probeLimiter {
	return &probeLimiter{perSession: make(map[string]int)}
}

// acquire returns true if the slot was granted. The caller must invoke the
// returned release exactly once.
func (l *probeLimiter) acquire(session string) (release func(), sessionLimited bool, globalLimited bool) {
	// Check the global cap first — a session-exhausted caller still gets a
	// 429 even when the global has room, but a global-exhausted daemon
	// rejects with 503 regardless of the session.
	if l.globalInUse.Load() >= probeGlobalCap {
		return nil, false, true
	}
	l.mu.Lock()
	if l.perSession[session] >= probeSessionCap {
		l.mu.Unlock()
		return nil, true, false
	}
	// Atomically bump global. If we race past the cap (two goroutines saw
	// room simultaneously), back off and report the right kind of limit.
	if l.globalInUse.Add(1) > probeGlobalCap {
		l.globalInUse.Add(-1)
		l.mu.Unlock()
		return nil, false, true
	}
	l.perSession[session]++
	l.mu.Unlock()

	var released atomic.Bool
	return func() {
		if !released.CompareAndSwap(false, true) {
			return
		}
		l.mu.Lock()
		l.perSession[session]--
		if l.perSession[session] <= 0 {
			delete(l.perSession, session)
		}
		l.mu.Unlock()
		l.globalInUse.Add(-1)
	}, false, false
}

// SetProber wires an externally-constructed prober. The API server owns the
// limiter but the prober's cache and spawner come from the gateway builder.
func (s *Server) SetProber(p *probe.Prober) {
	s.prober = p
	if s.probeLimiter == nil {
		s.probeLimiter = newProbeLimiter()
	}
}

// handleProbe is the HTTP entry point for the wizard's "Discover tools"
// button. It is intentionally a thin shell around probe.Prober.Probe — the
// hard work (validation, caching, cleanup) lives in the probe package where
// it is unit-tested without HTTP scaffolding.
func (s *Server) handleProbe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.prober == nil {
		writeProbeError(w, http.StatusServiceUnavailable, probe.CodeUnsupportedTransport,
			"Probe is not configured on this daemon.",
			"Upgrade to a build with probe support, or enter tool names manually.")
		return
	}

	session := r.Header.Get(sessionHeader)
	if session == "" {
		session = r.RemoteAddr
	}
	release, sessionLimited, globalLimited := s.probeLimiter.acquire(session)
	if sessionLimited || globalLimited {
		w.Header().Set("Retry-After", "3")
		code := http.StatusTooManyRequests
		msg := "Too many probes in progress for this session."
		if globalLimited {
			code = http.StatusServiceUnavailable
			msg = "Probe service is at capacity. Try again in a few seconds."
		}
		writeProbeError(w, code, probe.CodeRateLimited, msg, "")
		return
	}
	defer release()

	body, err := io.ReadAll(io.LimitReader(r.Body, probeRequestMaxBytes))
	if err != nil {
		writeProbeError(w, http.StatusBadRequest, probe.CodeInvalidConfig,
			"Failed to read request body: "+err.Error(), "")
		return
	}
	var req probeRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeProbeError(w, http.StatusBadRequest, probe.CodeInvalidConfig,
			"Invalid JSON: "+err.Error(), "")
		return
	}
	cfg := req.toMCPServer()

	result, probeErr := s.prober.Probe(r.Context(), cfg)
	if probeErr != nil {
		scrubSecrets(probeErr, cfg)
		writeProbeError(w, probeFailureStatus(probeErr), probeErr.Code, probeErr.Message, probeErr.Hint)
		return
	}

	writeJSON(w, probeResponse{
		Tools:    s.toToolsWire(result.Tools, cfg),
		ProbedAt: time.Now().UTC().Format(time.RFC3339),
		Cached:   result.Cached,
	})
}

// probeFailureStatus maps a probe error code to the HTTP status the spec
// pins down. Unknown codes default to 422 — "semantically valid request but
// the operation failed" — matching how the spec describes most probe
// failures.
func probeFailureStatus(e *probe.Error) int {
	switch e.Code {
	case probe.CodeInvalidConfig:
		return http.StatusBadRequest
	case probe.CodeRateLimited:
		return http.StatusTooManyRequests
	case probe.CodeInternal:
		return http.StatusInternalServerError
	default:
		return http.StatusUnprocessableEntity
	}
}

// probeRequest is the wire shape accepted by POST /api/servers/probe. It
// intentionally mirrors config.MCPServer but uses explicit JSON tags so the
// frontend can send snake_case fields that match the YAML schema exactly.
// Converting here (rather than adding JSON tags to config.MCPServer) keeps the
// wire contract local to the handler.
type probeRequest struct {
	Name         string            `json:"name,omitempty"`
	Image        string            `json:"image,omitempty"`
	Source       *config.Source    `json:"source,omitempty"`
	URL          string            `json:"url,omitempty"`
	Port         int               `json:"port,omitempty"`
	Transport    string            `json:"transport,omitempty"`
	Command      []string          `json:"command,omitempty"`
	Env          map[string]string `json:"env,omitempty"`
	BuildArgs    map[string]string `json:"build_args,omitempty"`
	Network      string            `json:"network,omitempty"`
	SSH          *config.SSHConfig `json:"ssh,omitempty"`
	OpenAPI      *config.OpenAPIConfig `json:"openapi,omitempty"`
	Tools        []string          `json:"tools,omitempty"`
	OutputFormat string            `json:"output_format,omitempty"`
	ReadyTimeout string            `json:"ready_timeout,omitempty"`
	Replicas     int               `json:"replicas,omitempty"`
	Auth         *serverAuthWire   `json:"auth,omitempty"`
}

// serverAuthWire mirrors config.ServerAuth with snake_case JSON tags so the
// wizard can send the same field names the stack YAML schema uses.
type serverAuthWire struct {
	Type         string   `json:"type"`
	Token        string   `json:"token,omitempty"`
	Header       string   `json:"header,omitempty"`
	Value        string   `json:"value,omitempty"`
	Scopes       []string `json:"scopes,omitempty"`
	ClientID     string   `json:"client_id,omitempty"`
	ClientSecret string   `json:"client_secret,omitempty"`
}

func (r probeRequest) toMCPServer() config.MCPServer {
	var auth *config.ServerAuth
	if r.Auth != nil {
		auth = &config.ServerAuth{
			Type:         r.Auth.Type,
			Token:        r.Auth.Token,
			Header:       r.Auth.Header,
			Value:        r.Auth.Value,
			Scopes:       r.Auth.Scopes,
			ClientID:     r.Auth.ClientID,
			ClientSecret: r.Auth.ClientSecret,
		}
	}
	return config.MCPServer{
		Name:         r.Name,
		Image:        r.Image,
		Source:       r.Source,
		URL:          r.URL,
		Port:         r.Port,
		Transport:    r.Transport,
		Command:      r.Command,
		Env:          r.Env,
		BuildArgs:    r.BuildArgs,
		Network:      r.Network,
		SSH:          r.SSH,
		OpenAPI:      r.OpenAPI,
		Tools:        r.Tools,
		OutputFormat: r.OutputFormat,
		ReadyTimeout: r.ReadyTimeout,
		Replicas:     r.Replicas,
		Auth:         auth,
	}
}

type probeToolWire struct {
	Name         string          `json:"name"`
	Description  string          `json:"description,omitempty"`
	InputSchema  json.RawMessage `json:"inputSchema"`
	OutputSchema json.RawMessage `json:"outputSchema,omitempty"`
	// Findings are advisory poisoning-scan results (P001-P005) so the wizard
	// can flag a suspicious server before it joins the stack. Never blocking.
	Findings []pins.Finding `json:"findings,omitempty"`
}

type probeResponse struct {
	Tools    []probeToolWire `json:"tools"`
	ProbedAt string          `json:"probedAt"`
	Cached   bool            `json:"cached"`
}

type probeErrorWire struct {
	Error probeErrorPayload `json:"error"`
}

type probeErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

func writeProbeError(w http.ResponseWriter, status int, code, message, hint string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(probeErrorWire{
		Error: probeErrorPayload{Code: code, Message: message, Hint: hint},
	})
}

// configSecrets collects the non-empty secret-bearing values of a server
// config: env-var values plus the auth block's token, header value, and
// client secret. The set of "what counts as a secret for this server" lives
// here once; scrubSecrets and scrubFindings both consume it. Empty values are
// excluded — those can never accidentally leak, and scrubbing them would turn
// every string into "***".
func configSecrets(cfg config.MCPServer) []string {
	secrets := make([]string, 0, len(cfg.Env)+3)
	for _, v := range cfg.Env {
		if v != "" {
			secrets = append(secrets, v)
		}
	}
	if cfg.Auth != nil {
		for _, v := range []string{cfg.Auth.Token, cfg.Auth.Value, cfg.Auth.ClientSecret} {
			if v != "" {
				secrets = append(secrets, v)
			}
		}
	}
	return secrets
}

// scrubSecrets replaces occurrences of secret-bearing config values inside
// the probe error's user-facing strings with "***".
func scrubSecrets(e *probe.Error, cfg config.MCPServer) {
	if e == nil {
		return
	}
	for _, v := range configSecrets(cfg) {
		e.Message = strings.ReplaceAll(e.Message, v, "***")
		e.Hint = strings.ReplaceAll(e.Hint, v, "***")
	}
}

// toToolsWire converts probed tools to the wire shape, attaching advisory
// poisoning-scan findings. The probed server predates any stack membership,
// but the RUNNING stack's scan settings still apply: scan false suppresses
// probe findings and scan_ignore filters them, so the wizard never shows
// findings the operator has deliberately turned off. With no pin store (or
// pinning disabled), scanning defaults on: pre-deploy advice is the wizard's
// whole point.
func (s *Server) toToolsWire(tools []mcp.Tool, cfg config.MCPServer) []probeToolWire {
	scanEnabled := true
	var ignore []string
	if s.pinStore != nil {
		scanEnabled = s.pinStore.ScanEnabled()
		ignore = s.pinStore.ScanIgnoreCodes()
	}
	out := make([]probeToolWire, len(tools))
	for i, t := range tools {
		out[i] = probeToolWire{
			Name:         t.Name,
			Description:  t.Description,
			InputSchema:  t.InputSchema,
			OutputSchema: t.OutputSchema,
		}
		if scanEnabled {
			out[i].Findings = scrubFindings(pins.FilterFindings(pins.ScanTool(t), ignore), cfg)
		}
	}
	return out
}

// scrubFindings applies the same secret scrubbing as probe errors to finding
// snippets and decoded payloads, which quote text from an untrusted server
// and could otherwise echo a credential the user typed into the wizard.
func scrubFindings(findings []pins.Finding, cfg config.MCPServer) []pins.Finding {
	if len(findings) == 0 {
		return nil
	}
	for i := range findings {
		for _, v := range configSecrets(cfg) {
			findings[i].Snippet = strings.ReplaceAll(findings[i].Snippet, v, "***")
			findings[i].Decoded = strings.ReplaceAll(findings[i].Decoded, v, "***")
		}
	}
	return findings
}
