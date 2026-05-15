package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/agent/compose"
	"github.com/gridctl/gridctl/pkg/agent/persist"
	"github.com/gridctl/gridctl/pkg/agent/runner"
)

// agentRunsListDefaultLimit caps the default page size for
// GET /api/agent/runs when no `limit` query param is supplied.
const agentRunsListDefaultLimit = 50

// agentRunsListMaxLimit caps the page size operators can request.
// Large requests still page through the directory scan; a hard ceiling
// keeps a malicious or buggy client from forcing the daemon to read
// thousands of summary files in one request.
const agentRunsListMaxLimit = 500

// SetAgentRunStore wires the persist.Store the /api/agent/runs/*
// handlers fall back to when no runtime aggregate is installed.
// Production callers should use SetAgentRuntime, which takes precedence
// over this setter at read time. Retained for tests that need only one
// slice of runtime state.
func (s *Server) SetAgentRunStore(store *persist.Store) {
	s.agentRunStore = store
}

// SetAgentApprovalRegistry wires the in-process approval registry the
// /api/agent/runs/{id}/approve handler falls back to when no runtime
// aggregate is installed. SetAgentRuntime takes precedence at read
// time. Retained for test-fixture wiring.
func (s *Server) SetAgentApprovalRegistry(reg *compose.Registry) {
	s.agentApprovalRegistry = reg
}

// agentRunListItem is the response element for GET /api/agent/runs.
// Mirrors persist.RunSummary but explicitly omits the on-disk path
// so the API doesn't leak the daemon's filesystem layout to clients.
type agentRunListItem struct {
	RunID           string    `json:"run_id"`
	Skill           string    `json:"skill,omitempty"`
	Flavor          string    `json:"flavor,omitempty"`
	Status          string    `json:"status"`
	StartedAt       time.Time `json:"started_at,omitempty"`
	CompletedAt     time.Time `json:"completed_at,omitempty"`
	EventCount      int       `json:"event_count"`
	ParentRunID     string    `json:"parent_run_id,omitempty"`
	TraceID         string    `json:"trace_id,omitempty"`
	PendingApproval string    `json:"pending_approval,omitempty"`
	Error           string    `json:"error,omitempty"`
}

// handleAgentRunsList returns a paginated list of recent runs.
//
// Query params (all optional):
//   - status  — exact match against summary status (running / ok / error /
//     cancelled / awaiting_approval / suspended).
//   - skill   — exact match against the skill name the run was launched
//     against.
//   - parent  — exact match against parent_run_id; surfaces child runs of
//     a given root.
//   - since   — RFC 3339 timestamp or relative duration (`5m`, `1h`,
//     `24h`, `7d`); excludes runs older than the cutoff.
//   - limit   — page size, clamped to [1, agentRunsListMaxLimit].
//   - cursor  — last run_id from the previous page; resumes the scan
//     from the next-older run.
//
// Filtering scans the ledger directory in run-ID order (newest first)
// and applies the predicates in-memory. For installs with more than
// ~10k persisted runs the store should grow a sidecar index; until
// then the scan cost is bounded by the page size + filter window.
func (s *Server) handleAgentRunsList(w http.ResponseWriter, r *http.Request) {
	store := s.runStore()
	if store == nil {
		writeJSONError(w, "agent runtime not configured", http.StatusServiceUnavailable)
		return
	}

	q := r.URL.Query()
	limit := agentRunsListDefaultLimit
	if v := q.Get("limit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if limit > agentRunsListMaxLimit {
		limit = agentRunsListMaxLimit
	}

	filter := persist.ListFilter{
		Status:   strings.TrimSpace(q.Get("status")),
		Skill:    strings.TrimSpace(q.Get("skill")),
		Parent:   strings.TrimSpace(q.Get("parent")),
		BeforeID: strings.TrimSpace(q.Get("cursor")),
	}
	if since := strings.TrimSpace(q.Get("since")); since != "" {
		t, err := parseSinceParam(since)
		if err != nil {
			writeJSONError(w, fmt.Sprintf("invalid since: %v", err), http.StatusBadRequest)
			return
		}
		filter.Since = t
	}

	// Over-fetch by one so we can emit a forward cursor only when
	// there is something to page to.
	summaries, err := store.ListFiltered(filter, limit+1)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var nextCursor string
	if len(summaries) > limit {
		summaries = summaries[:limit]
	}
	if len(summaries) == limit && limit > 0 {
		nextCursor = summaries[limit-1].RunID
	}

	out := make([]agentRunListItem, 0, len(summaries))
	for _, sum := range summaries {
		out = append(out, summaryToListItem(sum))
	}
	body := map[string]any{"runs": out}
	if nextCursor != "" {
		body["next_cursor"] = nextCursor
	}
	writeJSON(w, body)
}

// parseSinceParam accepts an RFC 3339 timestamp ("2026-05-14T10:00:00Z")
// or a relative duration ("5m", "1h", "24h", "7d"). Relative values are
// resolved against time.Now() so the lower bound moves with the
// request. Custom shapes (e.g. "1d2h") are rejected — the UI only ever
// sends the canonical forms.
func parseSinceParam(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	// Go's ParseDuration accepts the unit suffixes we care about; "d"
	// is not native, so expand it before delegating.
	if strings.HasSuffix(s, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil || days <= 0 {
			return time.Time{}, fmt.Errorf("invalid duration %q", s)
		}
		return time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour), nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return time.Time{}, fmt.Errorf("invalid duration %q", s)
	}
	return time.Now().UTC().Add(-d), nil
}

// handleAgentRunGet returns a run's typed event timeline. The full
// payload list is returned in one shot — for streaming, see
// handleAgentRunEvents (SSE).
func (s *Server) handleAgentRunGet(w http.ResponseWriter, r *http.Request) {
	store := s.runStore()
	if store == nil {
		writeJSONError(w, "agent runtime not configured", http.StatusServiceUnavailable)
		return
	}
	runID := r.PathValue("run_id")
	if runID == "" {
		writeJSONError(w, "run_id is required", http.StatusBadRequest)
		return
	}
	events, err := store.Read(runID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeJSONError(w, fmt.Sprintf("run %q not found", runID), http.StatusNotFound)
			return
		}
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	summary, err := store.Summary(runID)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{
		"run":    summaryToListItem(summary),
		"events": events,
	})
}

// handleAgentRunEvents streams a run's events as an SSE feed. Each
// event lands as a `data:` line carrying the JSON-encoded persist.Event.
// The handler streams the complete ledger then closes the connection;
// live tailing lands when the runtime exposes an event-bus surface.
func (s *Server) handleAgentRunEvents(w http.ResponseWriter, r *http.Request) {
	store := s.runStore()
	if store == nil {
		writeJSONError(w, "agent runtime not configured", http.StatusServiceUnavailable)
		return
	}
	runID := r.PathValue("run_id")
	if runID == "" {
		writeJSONError(w, "run_id is required", http.StatusBadRequest)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONError(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")

	err := store.Stream(r.Context(), runID, func(ev persist.Event) error {
		raw, err := json.Marshal(ev)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", raw); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	})
	if err != nil && !errors.Is(err, r.Context().Err()) {
		// Best-effort: the connection is already partway through an
		// SSE stream, so a structured error response would corrupt
		// the framing. Emit an `event: error` frame instead. The
		// error string is JSON-encoded so any embedded quotes or
		// newlines are escaped before they hit the SSE framing.
		safe, _ := json.Marshal(err.Error())
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", safe)
		flusher.Flush()
	}
}

// handleAgentRunsEventsStream is the global live tail across every
// run. It subscribes to the persist.Store bus and forwards each event
// as an SSE `data:` line for the lifetime of the connection.
//
// Reconnect contract: clients dedupe by `(run_id, seq)` against a
// per-run watermark, because the per-run event endpoint replays the
// full ledger on subscribe and the same event will appear on both
// streams. When the per-subscriber buffer overflows the handler emits
// an `event: stream_restarted` frame ahead of the next event so the
// client can resync its watermark — at-least-once with a one-shot
// gap signal, not at-most-once.
func (s *Server) handleAgentRunsEventsStream(w http.ResponseWriter, r *http.Request) {
	store := s.runStore()
	if store == nil {
		writeJSONError(w, "agent runtime not configured", http.StatusServiceUnavailable)
		return
	}
	bus := store.Bus()
	if bus == nil {
		writeJSONError(w, "global event bus not available", http.StatusServiceUnavailable)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONError(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")

	sub, unsub := bus.Subscribe()
	defer unsub()

	// Emit a no-op ready frame so the client can confirm the stream is
	// open before any actual events arrive. EventSource readers ignore
	// unknown event types, so this is safe to ship without a matching
	// listener.
	fmt.Fprint(w, "event: ready\ndata: {}\n\n")
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-sub.C:
			if !ok {
				return
			}
			if sub.TakeDropped() {
				fmt.Fprint(w, "event: stream_restarted\ndata: {}\n\n")
			}
			raw, err := json.Marshal(ev)
			if err != nil {
				// Best-effort: keep the stream alive but surface the
				// problem so the client can decide to refetch.
				safe, _ := json.Marshal(err.Error())
				fmt.Fprintf(w, "event: error\ndata: %s\n\n", safe)
				flusher.Flush()
				continue
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", raw); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// agentResumeRequest is the body accepted by POST /api/agent/runs/{run_id}/resume.
type agentResumeRequest struct {
	FromStep string `json:"from_step,omitempty"`
}

// handleAgentRunResume builds a resume plan from the on-disk ledger
// and returns it. The runtime hook that re-executes graphs lands
// alongside the agent IDE; today the handler is a deterministic
// projection over the JSONL so CLI/web clients see the same
// rehydrated state.
func (s *Server) handleAgentRunResume(w http.ResponseWriter, r *http.Request) {
	store := s.runStore()
	if store == nil {
		writeJSONError(w, "agent runtime not configured", http.StatusServiceUnavailable)
		return
	}
	runID := r.PathValue("run_id")
	if runID == "" {
		writeJSONError(w, "run_id is required", http.StatusBadRequest)
		return
	}
	var req agentResumeRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, fmt.Sprintf("decoding body: %v", err), http.StatusBadRequest)
			return
		}
	}
	plan, err := store.BuildResumePlan(runID, req.FromStep)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, plan)
}

// agentApproveRequest is the body accepted by POST /api/agent/runs/{run_id}/approve.
type agentApproveRequest struct {
	ApprovalID string `json:"approval_id,omitempty"`
	Approved   bool   `json:"approved"`
	Reason     string `json:"reason,omitempty"`
	Source     string `json:"source,omitempty"`
}

// handleAgentRunApprove resolves an approval gate. The approval ID
// can be supplied explicitly or inferred from the run's pending
// approval (most CLIs use the run-id-only form for convenience).
func (s *Server) handleAgentRunApprove(w http.ResponseWriter, r *http.Request) {
	registry := s.approvalRegistry()
	if registry == nil {
		writeJSONError(w, "agent runtime not configured", http.StatusServiceUnavailable)
		return
	}
	store := s.runStore()
	if store == nil {
		writeJSONError(w, "agent runtime not configured", http.StatusServiceUnavailable)
		return
	}

	runID := r.PathValue("run_id")
	if runID == "" {
		writeJSONError(w, "run_id is required", http.StatusBadRequest)
		return
	}
	var req agentApproveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, fmt.Sprintf("decoding body: %v", err), http.StatusBadRequest)
		return
	}

	approvalID := req.ApprovalID
	if approvalID == "" {
		summary, err := store.Summary(runID)
		if err != nil {
			writeJSONError(w, fmt.Sprintf("locating run: %v", err), http.StatusNotFound)
			return
		}
		if summary.PendingApproval == "" {
			writeJSONError(w, fmt.Sprintf("run %q has no pending approval", runID), http.StatusNotFound)
			return
		}
		approvalID = summary.PendingApproval
	}

	source := req.Source
	if source == "" {
		source = "api"
	}
	if err := registry.Resolve(approvalID, req.Approved, req.Reason, source); err != nil {
		switch {
		case errors.Is(err, compose.ErrApprovalNotFound):
			writeJSONError(w, err.Error(), http.StatusNotFound)
		case errors.Is(err, compose.ErrAlreadyResolved):
			writeJSONError(w, err.Error(), http.StatusConflict)
		default:
			writeJSONError(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	writeJSON(w, map[string]any{
		"run_id":      runID,
		"approval_id": approvalID,
		"approved":    req.Approved,
	})
}

// agentRunLaunchRequest is the body accepted by POST /api/agent/runs.
type agentRunLaunchRequest struct {
	SkillName string          `json:"skill_name"`
	Input     json.RawMessage `json:"input,omitempty"`
}

// agentRunLaunchResponse is the body returned by POST /api/agent/runs.
type agentRunLaunchResponse struct {
	RunID     string    `json:"run_id"`
	StartedAt time.Time `json:"started_at"`
}

// handleAgentRunsLaunch validates the request and dispatches a new run
// via the daemon's wired registry server. EventRunStarted is recorded
// synchronously before the response returns so SSE subscribers polling
// the new run see the head of the ledger without racing the first
// event. Execution flows through registry.Server.CallTool so
// tool()/llm()/approval() bindings (vault, gateway routing, approval
// registry) apply unchanged.
//
// Initial scope is TS-handler skills only — Go and prompt-only handlers
// are rejected with 422 and an actionable message.
func (s *Server) handleAgentRunsLaunch(w http.ResponseWriter, r *http.Request) {
	store := s.runStore()
	if store == nil || s.registryServer == nil {
		writeJSONError(w, "agent runtime not configured", http.StatusServiceUnavailable)
		return
	}

	var req agentRunLaunchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, fmt.Sprintf("decoding body: %v", err), http.StatusBadRequest)
		return
	}
	if req.SkillName == "" {
		writeJSONError(w, "skill_name is required", http.StatusBadRequest)
		return
	}

	// Default input to {} when absent. When present, it must be a
	// JSON object — a top-level array, scalar, or null is rejected so
	// the dispatcher receives a stable shape.
	input := map[string]any{}
	rawInput := json.RawMessage(`{}`)
	if len(req.Input) > 0 {
		if err := json.Unmarshal(req.Input, &input); err != nil || input == nil {
			writeJSONError(w, "input must be a JSON object", http.StatusBadRequest)
			return
		}
		rawInput = req.Input
	}

	sk, err := s.registryServer.Store().GetSkill(req.SkillName)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("skill %q not found", req.SkillName), http.StatusNotFound)
		return
	}

	switch sk.HandlerLanguage {
	case "ts":
		// proceed
	case "go":
		writeJSONError(w, fmt.Sprintf("skill %q has a Go handler; the launcher does not yet support Go plugins — invoke via `gridctl run` after `gridctl agent build`", req.SkillName), http.StatusUnprocessableEntity)
		return
	case "":
		writeJSONError(w, fmt.Sprintf("skill %q is prompt-only and surfaces as an MCP prompt, not an invocable tool", req.SkillName), http.StatusUnprocessableEntity)
		return
	default:
		writeJSONError(w, fmt.Sprintf("skill %q has unsupported handler language %q", req.SkillName, sk.HandlerLanguage), http.StatusUnprocessableEntity)
		return
	}

	runID, startedAt, err := runner.Start(r.Context(), store, s.registryServer, runner.StartOptions{
		Skill:    req.SkillName,
		Flavor:   sk.HandlerLanguage,
		Input:    input,
		RawInput: rawInput,
	})
	if err != nil {
		writeJSONError(w, fmt.Sprintf("starting run: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, agentRunLaunchResponse{RunID: runID, StartedAt: startedAt})
}

// summaryToListItem strips the on-disk path off a persist.RunSummary
// so the API doesn't leak the daemon's filesystem layout.
func summaryToListItem(s persist.RunSummary) agentRunListItem {
	return agentRunListItem{
		RunID:           s.RunID,
		Skill:           s.Skill,
		Flavor:          s.Flavor,
		Status:          s.Status,
		StartedAt:       s.StartedAt,
		CompletedAt:     s.CompletedAt,
		EventCount:      s.EventCount,
		ParentRunID:     s.ParentRunID,
		TraceID:         s.TraceID,
		PendingApproval: s.PendingApproval,
		Error:           s.Error,
	}
}
