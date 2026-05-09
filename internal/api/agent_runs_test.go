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
