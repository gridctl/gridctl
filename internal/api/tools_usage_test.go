package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
