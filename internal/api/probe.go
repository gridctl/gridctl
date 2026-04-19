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
		scrubSecrets(probeErr, cfg.Env)
		writeProbeError(w, probeFailureStatus(probeErr), probeErr.Code, probeErr.Message, probeErr.Hint)
		return
	}

	writeJSON(w, probeResponse{
		Tools:    toToolsWire(result.Tools),
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
}

func (r probeRequest) toMCPServer() config.MCPServer {
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
	}
}

type probeToolWire struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema"`
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

// scrubSecrets replaces occurrences of env-var values inside the probe error's
// user-facing strings with "***". Keys with empty values are ignored — those
// can never accidentally leak, and scrubbing them would turn every error into
// "***".
func scrubSecrets(e *probe.Error, env map[string]string) {
	if e == nil || len(env) == 0 {
		return
	}
	for _, v := range env {
		if v == "" {
			continue
		}
		e.Message = strings.ReplaceAll(e.Message, v, "***")
		e.Hint = strings.ReplaceAll(e.Hint, v, "***")
	}
}

func toToolsWire(tools []mcp.Tool) []probeToolWire {
	out := make([]probeToolWire, len(tools))
	for i, t := range tools {
		out[i] = probeToolWire{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		}
	}
	return out
}
