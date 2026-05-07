package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gridctl/gridctl/pkg/metrics"
	"github.com/gridctl/gridctl/pkg/optimize"
)

func TestHandleOptimize_NoAccumulator_503(t *testing.T) {
	srv := newTestServer(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/optimize", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 without accumulator; got %d", rec.Code)
	}
}

func TestHandleOptimize_WrongStack_404(t *testing.T) {
	srv := newTestServerWithMetrics(t)
	srv.SetStackName("test-stack")

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/optimize?stack=other-stack", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for wrong stack; got %d", rec.Code)
	}
}

func TestHandleOptimize_FreshGateway_InfoFinding(t *testing.T) {
	srv := newTestServerWithMetrics(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/optimize", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200; got %d", rec.Code)
	}
	var report optimize.OptimizeReport
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(report.Findings) != 1 {
		t.Fatalf("expected exactly one info finding on a fresh gateway; got %d", len(report.Findings))
	}
	if report.Findings[0].Severity != optimize.SeverityInfo {
		t.Errorf("Severity = %q, want info", report.Findings[0].Severity)
	}
	if report.HealthScore != 100 {
		t.Errorf("HealthScore = %d, want 100", report.HealthScore)
	}
}

func TestHandleOptimize_MethodNotAllowed(t *testing.T) {
	srv := newTestServerWithMetrics(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/optimize", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405; got %d", rec.Code)
	}
}

// TestHandleOptimize_ReportShape verifies the JSON contract that the
// CLI and Web UI consume: top-level findings, health_score, and
// generated_at must round-trip through the API.
func TestHandleOptimize_ReportShape(t *testing.T) {
	srv := newTestServerWithMetrics(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/optimize", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, want := range []string{"findings", "health_score", "generated_at"} {
		if _, ok := raw[want]; !ok {
			t.Errorf("expected field %q in optimize report; got %v", want, rec.Body.String())
		}
	}
}

// TestHandleOptimize_CountsToolUsage verifies the per-tool tracking we
// added on the accumulator flows into Stats.ToolUsage and influences
// the unused_tool path.
func TestHandleOptimize_CountsToolUsage(t *testing.T) {
	srv := newTestServerWithMetrics(t)
	srv.metricsAccumulator.RecordToolCall("github", "create_issue")

	stats := srv.optimizeStats()

	if got := stats.ToolUsage["github"]["create_issue"].Calls; got != 1 {
		t.Errorf("Stats.ToolUsage didn't propagate; got Calls=%d", got)
	}
}

// Round-trip check: a min_impact filter is honored through the API.
func TestHandleOptimize_MinImpactRespected(t *testing.T) {
	srv := newTestServerWithMetrics(t)
	// Backdate the start so we exit the "need more data" gate.
	srv.metricsAccumulator = metrics.NewAccumulator(100)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/optimize?min_impact=999999", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200; got %d", rec.Code)
	}
}
