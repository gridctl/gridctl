package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/metrics"
)

// decodeToolUsage runs GET /api/tools/usage against srv and returns the
// decoded response plus the HTTP status.
func decodeToolUsage(t *testing.T, srv *Server) (toolUsageResponse, int) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/tools/usage", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	var resp toolUsageResponse
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response: %v (body=%s)", err, rec.Body.String())
		}
	}
	return resp, rec.Code
}

func TestHandleToolsUsage_ReportsPerToolCounts(t *testing.T) {
	srv := newTestServerWithMetrics(t)
	// The observer records both direct and code-mode calls through
	// RecordToolCall with the real downstream (server, tool); record a few
	// here to stand in for that path.
	srv.metricsAccumulator.RecordToolCall("github", "create_issue")
	srv.metricsAccumulator.RecordToolCall("github", "create_issue")
	srv.metricsAccumulator.RecordToolCall("github", "list_repos")

	resp, code := decodeToolUsage(t, srv)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}

	gh, ok := resp.Servers["github"]
	if !ok {
		t.Fatalf("github missing from response: %+v", resp.Servers)
	}
	if gh["create_issue"].Calls != 2 {
		t.Errorf("create_issue calls = %d, want 2", gh["create_issue"].Calls)
	}
	if gh["list_repos"].Calls != 1 {
		t.Errorf("list_repos calls = %d, want 1", gh["list_repos"].Calls)
	}
	if gh["create_issue"].LastCalledAt == nil || gh["create_issue"].LastCalledAt.IsZero() {
		t.Error("create_issue lastCalledAt should be set")
	}
	if resp.ObservedSince == nil || resp.ObservedSince.IsZero() {
		t.Error("observedSince should be set when an accumulator is wired")
	}
}

// TestHandleToolsUsage_ReportsTokensAndCost covers the per-tool cost
// extension: tokens ride every recorded call, costUsd appears only for
// priced tools, and an unpriced tool omits the field entirely (never $0).
func TestHandleToolsUsage_ReportsTokensAndCost(t *testing.T) {
	srv := newTestServerWithMetrics(t)
	acc := srv.metricsAccumulator
	acc.RecordToolCallUsage("github", "create_issue", 120, 80)
	acc.RecordToolCost("github", "create_issue", metrics.CostBreakdown{Input: 0.001, Output: 0.002})
	acc.RecordToolCallUsage("github", "list_repos", 30, 10)

	resp, code := decodeToolUsage(t, srv)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}

	priced := resp.Servers["github"]["create_issue"]
	if priced.InputTokens != 120 || priced.OutputTokens != 80 {
		t.Errorf("priced tokens = %d/%d, want 120/80", priced.InputTokens, priced.OutputTokens)
	}
	if priced.CostUSD == nil {
		t.Fatal("priced tool costUsd missing")
	}
	if got, want := *priced.CostUSD, 0.003; got < want-1e-9 || got > want+1e-9 {
		t.Errorf("costUsd = %v, want %v", got, want)
	}

	unpriced := resp.Servers["github"]["list_repos"]
	if unpriced.InputTokens != 30 || unpriced.OutputTokens != 10 {
		t.Errorf("unpriced tokens = %d/%d, want 30/10", unpriced.InputTokens, unpriced.OutputTokens)
	}
	if unpriced.CostUSD != nil {
		t.Errorf("unpriced tool costUsd = %v, want absent", *unpriced.CostUSD)
	}
}

// TestHandleToolsUsage_UnpricedOmitsCostKey pins the raw JSON contract:
// the costUsd key is absent for unpriced tools, not null or 0.
func TestHandleToolsUsage_UnpricedOmitsCostKey(t *testing.T) {
	srv := newTestServerWithMetrics(t)
	srv.metricsAccumulator.RecordToolCall("github", "list_repos")

	req := httptest.NewRequest(http.MethodGet, "/api/tools/usage", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); strings.Contains(body, "costUsd") {
		t.Errorf("unpriced response must not carry costUsd: %s", body)
	}
}

func TestHandleToolsUsage_EmptyIsObjectNotNull(t *testing.T) {
	srv := newTestServerWithMetrics(t)
	resp, code := decodeToolUsage(t, srv)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if resp.Servers == nil {
		t.Error("servers should be a non-nil object when nothing has been called")
	}
	if len(resp.Servers) != 0 {
		t.Errorf("servers = %+v, want empty", resp.Servers)
	}
}

func TestHandleToolsUsage_NoAccumulatorReturns503(t *testing.T) {
	srv := newTestServer(t) // no metrics accumulator wired
	req := httptest.NewRequest(http.MethodGet, "/api/tools/usage", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

// TestHandleToolsUsage_SurvivesRestoreSeed asserts the endpoint surfaces
// usage that was restored from disk (the persistence path), not just usage
// recorded live this process — i.e. Audit Mode reflects pre-restart history.
func TestHandleToolsUsage_SurvivesRestoreSeed(t *testing.T) {
	srv := newTestServerWithMetrics(t)
	last := time.Now().Add(-3 * time.Hour).Truncate(time.Second)
	srv.metricsAccumulator.RestoreToolUsage(map[string]map[string]metrics.ToolStat{
		"atlassian": {"get_page": {Calls: 7, LastCalledAt: last}},
	})

	resp, code := decodeToolUsage(t, srv)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	got := resp.Servers["atlassian"]["get_page"]
	if got.Calls != 7 {
		t.Errorf("restored get_page calls = %d, want 7", got.Calls)
	}
	if got.LastCalledAt == nil || !got.LastCalledAt.Equal(last.UTC()) {
		t.Errorf("restored lastCalledAt = %v, want %v", got.LastCalledAt, last.UTC())
	}
}
