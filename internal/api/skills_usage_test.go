package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/metrics"
)

// decodeSkillUsage runs GET /api/skills/usage against srv and returns the
// decoded response plus the HTTP status.
func decodeSkillUsage(t *testing.T, srv *Server) (skillUsageResponse, int) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/skills/usage", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	var resp skillUsageResponse
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response: %v (body=%s)", err, rec.Body.String())
		}
	}
	return resp, rec.Code
}

func TestHandleSkillsUsage_ReportsPerSkillCounts(t *testing.T) {
	srv := newTestServerWithMetrics(t)
	srv.metricsAccumulator.RecordPromptGet("code-review")
	srv.metricsAccumulator.RecordPromptGet("code-review")
	srv.metricsAccumulator.RecordPromptGet("summarize")

	resp, code := decodeSkillUsage(t, srv)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if resp.Skills["code-review"].Calls != 2 {
		t.Errorf("code-review calls = %d, want 2", resp.Skills["code-review"].Calls)
	}
	if resp.Skills["summarize"].Calls != 1 {
		t.Errorf("summarize calls = %d, want 1", resp.Skills["summarize"].Calls)
	}
	if resp.Skills["code-review"].LastCalledAt == nil || resp.Skills["code-review"].LastCalledAt.IsZero() {
		t.Error("code-review lastCalledAt should be set")
	}
	if resp.ObservedSince == nil || resp.ObservedSince.IsZero() {
		t.Error("observedSince should be set when an accumulator is wired")
	}
}

func TestHandleSkillsUsage_EmptyIsObjectNotNull(t *testing.T) {
	srv := newTestServerWithMetrics(t)
	resp, code := decodeSkillUsage(t, srv)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if resp.Skills == nil {
		t.Error("skills should be a non-nil object when nothing has been served")
	}
	if len(resp.Skills) != 0 {
		t.Errorf("skills = %+v, want empty", resp.Skills)
	}
}

func TestHandleSkillsUsage_NoAccumulatorReturns503(t *testing.T) {
	srv := newTestServer(t) // no metrics accumulator wired
	req := httptest.NewRequest(http.MethodGet, "/api/skills/usage", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

// TestHandleSkillsUsage_SurvivesRestoreSeed asserts the endpoint surfaces
// usage that was restored from disk (the persistence path), not just usage
// recorded live this process (i.e. the Skills Library reflects pre-restart
// history).
func TestHandleSkillsUsage_SurvivesRestoreSeed(t *testing.T) {
	srv := newTestServerWithMetrics(t)
	last := time.Now().Add(-3 * time.Hour).Truncate(time.Second)
	srv.metricsAccumulator.RestorePromptUsage(map[string]metrics.ToolStat{
		"deep-research": {Calls: 7, LastCalledAt: last},
	})

	resp, code := decodeSkillUsage(t, srv)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	got := resp.Skills["deep-research"]
	if got.Calls != 7 {
		t.Errorf("restored deep-research calls = %d, want 7", got.Calls)
	}
	if got.LastCalledAt == nil || !got.LastCalledAt.Equal(last.UTC()) {
		t.Errorf("restored lastCalledAt = %v, want %v", got.LastCalledAt, last.UTC())
	}
}
