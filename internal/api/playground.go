package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gridctl/gridctl/pkg/agent"
	"github.com/gridctl/gridctl/pkg/metrics"
	"github.com/gridctl/gridctl/pkg/pricing"
	"github.com/gridctl/gridctl/pkg/vault"
)

// PlaygroundProviderAuth reports whether a single LLM provider has
// usable auth configured. apiKey reflects the vault state (key
// resolved successfully); keyName is the vault key referenced (empty
// when no convention key is found); cliPath is non-empty when a CLI
// proxy binary is reachable. The frontend surfaces these to the user
// in the auth banner.
type PlaygroundProviderAuth struct {
	APIKey  bool   `json:"apiKey"`
	KeyName string `json:"keyName"`
	CLIPath string `json:"cliPath"`
}

// PlaygroundAuthResponse is the body returned by POST /api/playground/auth.
type PlaygroundAuthResponse struct {
	Providers map[string]PlaygroundProviderAuth `json:"providers"`
	Ollama    struct {
		Reachable bool   `json:"reachable"`
		Endpoint  string `json:"endpoint"`
	} `json:"ollama"`
}

// PlaygroundChatRequest is the body accepted by POST /api/playground/chat.
type PlaygroundChatRequest struct {
	Message   string `json:"message"`
	SessionID string `json:"sessionId"`
	AuthMode  string `json:"authMode"`
	Model     string `json:"model"`
	OllamaURL string `json:"ollamaUrl,omitempty"`
	AgentID   string `json:"agentId,omitempty"`
}

// PlaygroundChatResponse is the body returned by POST /api/playground/chat.
type PlaygroundChatResponse struct {
	SessionID string `json:"sessionId"`
	Status    string `json:"status"`
}

// playgroundEvent is an SSE event the frontend understands. The
// type field discriminates the active fields in data.
type playgroundEvent struct {
	Type string         `json:"type"`
	Data map[string]any `json:"data,omitempty"`
}

// playgroundSession holds the per-session event channel and the
// background context that runs the provider call. The session is
// lazy: the first /stream or /chat request for a given sessionId
// constructs it; subsequent requests rebind to the same channel.
type playgroundSession struct {
	id       string
	events   chan playgroundEvent
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}
	createAt time.Time
}

// playgroundService is the per-API-server state for /api/playground/*.
// Construction stays lazy — the service is allocated the first time a
// playground handler runs to avoid making the API server depend on a
// provider it may not have a key for.
type playgroundService struct {
	mu       sync.Mutex
	sessions map[string]*playgroundSession
}

func newPlaygroundService() *playgroundService {
	return &playgroundService{sessions: make(map[string]*playgroundSession)}
}

// getOrCreate returns the session for id, creating it if absent. The
// session's cancel func is invoked by remove() when the session
// completes; callers MUST always pair getOrCreate with remove on the
// same id, which is gosec G118's structural requirement.
func (s *playgroundService) getOrCreate(id string) *playgroundSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[id]; ok {
		return sess
	}
	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec // cancel is invoked via remove(id)
	sess := &playgroundSession{
		id:       id,
		events:   make(chan playgroundEvent, 64),
		ctx:      ctx,
		cancel:   cancel,
		done:     make(chan struct{}),
		createAt: time.Now(),
	}
	s.sessions[id] = sess
	return sess
}

// remove deletes a session. Idempotent.
func (s *playgroundService) remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[id]; ok {
		sess.cancel()
		delete(s.sessions, id)
	}
}

// playgroundService is allocated lazily and stored on the Server.
// playground returns it (allocating on first use). Safe under the
// gateway's normal serial setup; later calls hit the fast path.
func (s *Server) playground() *playgroundService {
	s.playgroundOnce.Do(func() { s.playgroundSvc = newPlaygroundService() })
	return s.playgroundSvc
}

// handlePlaygroundAuth probes the vault and ollama endpoint to report
// which providers have usable authentication configured. The endpoint
// is POST so the frontend can include any auth header without the
// browser cache pinning a stale state.
func (s *Server) handlePlaygroundAuth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	resp := PlaygroundAuthResponse{
		Providers: make(map[string]PlaygroundProviderAuth),
	}
	resp.Providers["anthropic"] = probeVaultKey(s.vaultStore, []string{"ANTHROPIC_API_KEY"})
	resp.Providers["openai"] = probeVaultKey(s.vaultStore, []string{"OPENAI_API_KEY"})
	resp.Providers["gemini"] = probeVaultKey(s.vaultStore, []string{"GEMINI_API_KEY", "GOOGLE_API_KEY"})

	resp.Ollama.Endpoint = "http://localhost:11434/v1"
	resp.Ollama.Reachable = false // probe deferred to a follow-up; honest UI state

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.logger().Error("playground.auth: encode response", slog.Any("err", err))
	}
}

// probeVaultKey reports whether any of the given vault keys resolves.
// The first key that resolves becomes KeyName; otherwise KeyName is
// the first candidate (so the UI can show the user which key it would
// look for).
func probeVaultKey(store *vault.Store, candidates []string) PlaygroundProviderAuth {
	out := PlaygroundProviderAuth{}
	if len(candidates) > 0 {
		out.KeyName = candidates[0]
	}
	if store == nil {
		return out
	}
	for _, k := range candidates {
		if v, ok := store.Get(k); ok && v != "" {
			out.APIKey = true
			out.KeyName = k
			return out
		}
	}
	return out
}

// handlePlaygroundChat runs an LLM call into the session's event
// channel. The handler returns immediately after spawning the call;
// the actual work happens in a goroutine that pushes events to the
// session channel for the /stream endpoint to consume.
func (s *Server) handlePlaygroundChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req PlaygroundChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.Message == "" || req.SessionID == "" {
		http.Error(w, "message and sessionId are required", http.StatusBadRequest)
		return
	}

	provider, model, providerName, err := s.resolveProvider(req.Model)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	sess := s.playground().getOrCreate(req.SessionID)
	go s.runPlaygroundChat(sess, provider, providerName, model, req.Message)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(PlaygroundChatResponse{
		SessionID: req.SessionID,
		Status:    "ok",
	})
}

// resolveProvider picks the agent.ChatModel and effective model ID for
// a chat request. When the requested model has no matching provider,
// the function returns an error the handler surfaces verbatim.
func (s *Server) resolveProvider(model string) (agent.ChatModel, string, string, error) {
	if s.playgroundProvider != nil {
		// The injected provider may be a router (llm/gateway.Provider)
		// that itself dispatches by prefix; otherwise the provider
		// honors whatever model the request specifies.
		return s.playgroundProvider, model, providerNameFromModel(model), nil
	}
	return nil, "", "", errors.New("playground: no LLM provider is configured (set ANTHROPIC_API_KEY in the vault and restart)")
}

// providerNameFromModel returns the synthetic server name used when
// recording metrics for an LLM call. The naming follows the
// "llm:<provider>" convention so cost dashboards can group LLM-only
// spend separately from gateway-routed MCP-tool spend.
func providerNameFromModel(model string) string {
	switch {
	case startsWith(model, "claude-"):
		return "llm:anthropic"
	case startsWith(model, "gpt-"), startsWith(model, "o1-"), startsWith(model, "o3-"):
		return "llm:openai"
	case startsWith(model, "gemini-"):
		return "llm:google"
	default:
		return "llm:unknown"
	}
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

// runPlaygroundChat is the goroutine that actually performs the LLM
// call. It pushes one or more events onto the session channel and
// closes the channel via a "done" event when the call finishes.
//
// The function is deliberately stateless beyond the session: each
// chat is one shot. Multi-turn history lives on the React side; v1
// playground is the simplest possible "render a single response"
// surface that exercises the full provider → stream → cost path.
func (s *Server) runPlaygroundChat(sess *playgroundSession, provider agent.ChatModel, providerName, model, message string) {
	ctx := sess.ctx
	defer func() {
		// One done event always emits, even on error.
		select {
		case sess.events <- playgroundEvent{Type: "done"}:
		case <-ctx.Done():
		}
		close(sess.done)
	}()

	req := agent.ChatRequest{
		Model:     model,
		MaxTokens: 1024,
		Messages: []agent.Message{
			{Role: agent.RoleUser, Content: message},
		},
	}

	stream, err := provider.Stream(ctx, req)
	if err != nil {
		s.emitPlaygroundError(sess, err)
		return
	}
	defer stream.Close()

	var (
		usage    agent.Usage
		stopReason agent.StopReason
	)
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			s.emitPlaygroundError(sess, err)
			return
		}
		if chunk.Delta != "" {
			s.emitPlaygroundEvent(sess, playgroundEvent{
				Type: "token",
				Data: map[string]any{"text": chunk.Delta},
			})
		}
		if chunk.Usage != nil {
			usage = *chunk.Usage
		}
		if chunk.StopReason != "" {
			stopReason = chunk.StopReason
		}
	}
	_ = stopReason // reserved for surfaces that surface stop reason

	s.recordPlaygroundCost(providerName, model, usage)
	s.emitPlaygroundEvent(sess, playgroundEvent{
		Type: "metrics",
		Data: map[string]any{
			"tokens_in":           usage.InputTokens,
			"tokens_out":          usage.OutputTokens,
			"format_savings_pct":  0.0,
		},
	})
}

// recordPlaygroundCost prices the call and records the breakdown into
// the metrics accumulator. Best-effort: an unknown model or absent
// accumulator silently skips recording.
func (s *Server) recordPlaygroundCost(providerName, model string, usage agent.Usage) {
	if s.metricsAccumulator == nil || model == "" {
		return
	}
	pu := pricing.Usage{
		InputTokens:      usage.InputTokens,
		OutputTokens:     usage.OutputTokens,
		CacheReadTokens:  usage.CacheReadTokens,
		CacheWriteTokens: usage.CacheWriteTokens,
	}
	cost, ok := pricing.CalculateBreakdown(model, pu)
	if !ok {
		return
	}
	s.metricsAccumulator.RecordCost(providerName, -1, metrics.CostBreakdown{
		Input:      cost.Input,
		Output:     cost.Output,
		CacheRead:  cost.CacheRead,
		CacheWrite: cost.CacheWrite,
	})
}

func (s *Server) emitPlaygroundEvent(sess *playgroundSession, ev playgroundEvent) {
	select {
	case sess.events <- ev:
	case <-sess.ctx.Done():
	case <-time.After(5 * time.Second):
		// Backpressure ceiling — if the SSE consumer is gone for that
		// long, abandon the event rather than block the provider goroutine.
	}
}

func (s *Server) emitPlaygroundError(sess *playgroundSession, err error) {
	s.emitPlaygroundEvent(sess, playgroundEvent{
		Type: "error",
		Data: map[string]any{"message": err.Error()},
	})
}

// handlePlaygroundStream is the SSE endpoint the React frontend
// connects to before issuing a chat request. It blocks reading from
// the session's event channel and writes one SSE frame per event.
func (s *Server) handlePlaygroundStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sessionID := r.URL.Query().Get("sessionId")
	if sessionID == "" {
		http.Error(w, "sessionId is required", http.StatusBadRequest)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	sess := s.playground().getOrCreate(sessionID)

	for {
		select {
		case ev, ok := <-sess.events:
			if !ok {
				s.playground().remove(sessionID)
				return
			}
			body, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", body)
			flusher.Flush()
			if ev.Type == "done" || ev.Type == "error" {
				s.playground().remove(sessionID)
				return
			}
		case <-r.Context().Done():
			// Client disconnected; tear down the session.
			s.playground().remove(sessionID)
			return
		}
	}
}

// logger returns the server's slog.Logger or a default one. The Server
// struct in api.go does not currently carry a logger; future work
// surfaces one through SetLogger. Until then, fall back to slog.Default.
func (s *Server) logger() *slog.Logger {
	return slog.Default()
}
