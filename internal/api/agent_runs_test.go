package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/agent/compose"
	"github.com/gridctl/gridctl/pkg/agent/persist"
	"github.com/gridctl/gridctl/pkg/registry"
)

func newAgentRunsTestServer(t *testing.T) (*Server, *persist.Store, *compose.Registry) {
	t.Helper()
	srv := newTestServer(t)
	store := persist.NewStore(t.TempDir())
	registry := compose.NewRegistry()
	srv.SetAgentRunStore(store)
	srv.SetAgentApprovalRegistry(registry)
	return srv, store, registry
}

func TestAgentRunsListReturnsRecorded(t *testing.T) {
	srv, store, _ := newAgentRunsTestServer(t)
	runID := persist.NewRunID()
	rec, err := store.OpenWriter(runID)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	if _, err := rec.Record(persist.EventRunStarted, persist.RunStartedPayload{Skill: "demo", Flavor: "ts"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/agent/runs", nil)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		Runs []agentRunListItem `json:"runs"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Runs) != 1 || body.Runs[0].RunID != runID {
		t.Fatalf("unexpected runs: %+v", body.Runs)
	}
}

func TestAgentRunsGetReturnsEvents(t *testing.T) {
	srv, store, _ := newAgentRunsTestServer(t)
	runID := persist.NewRunID()
	rec, err := store.OpenWriter(runID)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	if _, err := rec.Record(persist.EventRunStarted, persist.RunStartedPayload{Skill: "demo"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if _, err := rec.Record(persist.EventNodeEnter, persist.NodeEnterPayload{NodeID: "n1"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/agent/runs/"+runID, nil)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		Run    agentRunListItem `json:"run"`
		Events []persist.Event  `json:"events"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Run.RunID != runID {
		t.Fatalf("expected run id in response: %+v", body.Run)
	}
	if len(body.Events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(body.Events))
	}
}

func TestAgentRunsGetReturns404(t *testing.T) {
	srv, _, _ := newAgentRunsTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/agent/runs/missing", nil)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestAgentRunsResumeBuildsPlan(t *testing.T) {
	srv, store, _ := newAgentRunsTestServer(t)
	runID := persist.NewRunID()
	rec, err := store.OpenWriter(runID)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	if _, err := rec.Record(persist.EventRunStarted, persist.RunStartedPayload{Skill: "demo"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if _, err := rec.Record(persist.EventNodeEnter, persist.NodeEnterPayload{NodeID: "n1"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if _, err := rec.Record(persist.EventNodeExit, persist.NodeExitPayload{NodeID: "n1", Success: true}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/agent/runs/"+runID+"/resume", strings.NewReader(`{}`))
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var plan persist.ResumePlan
	if err := json.NewDecoder(rr.Body).Decode(&plan); err != nil {
		t.Fatalf("decode plan: %v", err)
	}
	if plan.RunID != runID {
		t.Fatalf("plan run id mismatch: %+v", plan)
	}
	if plan.LastSeq != 3 {
		t.Fatalf("expected LastSeq=3, got %d", plan.LastSeq)
	}
}

func TestAgentRunsApproveResolvesRegistry(t *testing.T) {
	srv, store, registry := newAgentRunsTestServer(t)
	runID := persist.NewRunID()
	rec, err := store.OpenWriter(runID)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	if _, err := rec.Record(persist.EventRunStarted, persist.RunStartedPayload{Skill: "demo"}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	gate, err := compose.NewGate(runID, rec, registry, &compose.SlogNotifier{}, compose.WithTimeout(2*time.Second))
	if err != nil {
		t.Fatalf("NewGate: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	var approved bool
	var approveErr error
	go func() {
		defer wg.Done()
		dec, err := gate.Approve(context.Background(), "ship?")
		approved = dec.Approved
		approveErr = err
	}()

	// Wait for the gate to register.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if list := registry.List(); len(list) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	body := strings.NewReader(`{"approved":true,"reason":"shippy","source":"web"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/agent/runs/"+runID+"/approve", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	wg.Wait()
	if approveErr != nil {
		t.Fatalf("Approve returned error: %v", approveErr)
	}
	if !approved {
		t.Fatalf("expected approved=true")
	}

	if err := rec.Close(); err != nil {
		t.Fatalf("rec.Close: %v", err)
	}
	events, err := store.Read(runID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	// Should include run_started, approval_request, approval_response.
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
}

func TestAgentRunsApprove404WhenNoPending(t *testing.T) {
	srv, store, _ := newAgentRunsTestServer(t)
	runID := persist.NewRunID()
	rec, err := store.OpenWriter(runID)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	if _, err := rec.Record(persist.EventRunStarted, persist.RunStartedPayload{}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	body := strings.NewReader(`{"approved":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/agent/runs/"+runID+"/approve", body)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestAgentRunsRoutes503WithoutStore(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/agent/runs", nil)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rr.Code)
	}
}

// newAgentRunLaunchTestServer wires the same per-component fixtures as
// newAgentRunsTestServer plus a registry server seeded with TS, Go, and
// prompt-only skills. The skills are written to a temp directory so the
// handler can read the cached AgentSkill metadata; no on-disk handler
// source is required because the test does not wait for the async
// dispatch to succeed.
func newAgentRunLaunchTestServer(t *testing.T) (*Server, *persist.Store, *registry.Server) {
	t.Helper()
	srv, store, _ := newAgentRunsTestServer(t)
	regStore := registry.NewStore(t.TempDir())
	regServer := registry.New(regStore)
	if err := regServer.Initialize(context.Background()); err != nil {
		t.Fatalf("registry init: %v", err)
	}
	srv.SetRegistryServer(regServer)
	return srv, store, regServer
}

func seedTypedSkill(t *testing.T, regServer *registry.Server, name, handler string) {
	t.Helper()
	sk := &registry.AgentSkill{
		Name:            name,
		Description:     "Test skill: " + name,
		State:           registry.StateActive,
		Body:            "# " + name + "\n\nSkill instructions.",
		HandlerLanguage: handler,
	}
	if err := regServer.Store().SaveSkill(sk); err != nil {
		t.Fatalf("SaveSkill %q: %v", name, err)
	}
}

func postLaunch(t *testing.T, srv *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/agent/runs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	return rr
}

func TestAgentRunsLaunch_ReturnsRunIDForTSSkill(t *testing.T) {
	srv, store, regServer := newAgentRunLaunchTestServer(t)
	seedTypedSkill(t, regServer, "demo-ts", "ts")

	rr := postLaunch(t, srv, `{"skill_name":"demo-ts","input":{"q":"hi"}}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var body agentRunLaunchResponse
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.RunID == "" {
		t.Fatal("expected non-empty run_id")
	}
	if body.StartedAt.IsZero() {
		t.Fatal("expected non-zero started_at")
	}

	// run_started is written synchronously before the response — the
	// ledger contains it immediately.
	events, err := store.Read(body.RunID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(events) == 0 || events[0].Type != persist.EventRunStarted {
		t.Fatalf("expected first event run_started, got %+v", events)
	}
	var started persist.RunStartedPayload
	if err := json.Unmarshal(events[0].Payload, &started); err != nil {
		t.Fatalf("decode run_started: %v", err)
	}
	if started.Skill != "demo-ts" {
		t.Fatalf("expected skill=demo-ts, got %q", started.Skill)
	}
	if started.Flavor != "ts" {
		t.Fatalf("expected flavor=ts, got %q", started.Flavor)
	}
	if string(started.Input) != `{"q":"hi"}` {
		t.Fatalf("expected raw input preserved, got %s", started.Input)
	}
}

func TestAgentRunsLaunch_DefaultsInputToEmptyObject(t *testing.T) {
	srv, store, regServer := newAgentRunLaunchTestServer(t)
	seedTypedSkill(t, regServer, "demo-ts", "ts")

	rr := postLaunch(t, srv, `{"skill_name":"demo-ts"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var body agentRunLaunchResponse
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	events, err := store.Read(body.RunID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	var started persist.RunStartedPayload
	if err := json.Unmarshal(events[0].Payload, &started); err != nil {
		t.Fatalf("decode run_started: %v", err)
	}
	if string(started.Input) != `{}` {
		t.Fatalf("expected default input {}, got %s", started.Input)
	}
}

func TestAgentRunsLaunch_404ForUnknownSkill(t *testing.T) {
	srv, _, _ := newAgentRunLaunchTestServer(t)
	rr := postLaunch(t, srv, `{"skill_name":"missing","input":{}}`)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestAgentRunsLaunch_422ForGoSkill(t *testing.T) {
	srv, _, regServer := newAgentRunLaunchTestServer(t)
	seedTypedSkill(t, regServer, "demo-go", "go")

	rr := postLaunch(t, srv, `{"skill_name":"demo-go","input":{}}`)
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "Go handler") {
		t.Fatalf("expected error to mention Go handler, got %s", rr.Body.String())
	}
}

func TestAgentRunsLaunch_422ForPromptOnlySkill(t *testing.T) {
	srv, _, regServer := newAgentRunLaunchTestServer(t)
	seedTypedSkill(t, regServer, "demo-prompt", "")

	rr := postLaunch(t, srv, `{"skill_name":"demo-prompt","input":{}}`)
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "prompt-only") {
		t.Fatalf("expected error to mention prompt-only, got %s", rr.Body.String())
	}
}

func TestAgentRunsLaunch_400ForMissingSkillName(t *testing.T) {
	srv, _, _ := newAgentRunLaunchTestServer(t)
	rr := postLaunch(t, srv, `{"input":{}}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestAgentRunsLaunch_400ForNonObjectInput(t *testing.T) {
	srv, _, regServer := newAgentRunLaunchTestServer(t)
	seedTypedSkill(t, regServer, "demo-ts", "ts")

	cases := []string{
		`{"skill_name":"demo-ts","input":[1,2,3]}`,
		`{"skill_name":"demo-ts","input":"string"}`,
		`{"skill_name":"demo-ts","input":42}`,
		`{"skill_name":"demo-ts","input":null}`,
	}
	for _, body := range cases {
		rr := postLaunch(t, srv, body)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for body %q, got %d body=%s", body, rr.Code, rr.Body.String())
		}
	}
}

func TestAgentRunsLaunch_400ForMalformedBody(t *testing.T) {
	srv, _, _ := newAgentRunLaunchTestServer(t)
	rr := postLaunch(t, srv, `not json`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestAgentRunsLaunch_503WithoutStore(t *testing.T) {
	srv := newTestServer(t)
	rr := postLaunch(t, srv, `{"skill_name":"demo","input":{}}`)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rr.Code)
	}
}

func TestAgentRunsLaunch_503WithoutRegistryServer(t *testing.T) {
	srv, _, _ := newAgentRunsTestServer(t) // sets store + approval registry, but not registryServer
	rr := postLaunch(t, srv, `{"skill_name":"demo","input":{}}`)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rr.Code)
	}
}
