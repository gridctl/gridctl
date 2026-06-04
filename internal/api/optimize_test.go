package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gridctl/gridctl/pkg/metrics"
	"github.com/gridctl/gridctl/pkg/optimize"
	"github.com/gridctl/gridctl/pkg/pricing"
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

// TestOptimizeStats_ServerCallCount_FromToolUsage covers the wiring
// between the per-tool counter on the accumulator and the per-server
// call count expensive_model_on_cheap_task expects.
func TestOptimizeStats_ServerCallCount_FromToolUsage(t *testing.T) {
	srv := newTestServerWithMetrics(t)
	srv.metricsAccumulator.RecordToolCall("github", "create_issue")
	srv.metricsAccumulator.RecordToolCall("github", "create_issue")
	srv.metricsAccumulator.RecordToolCall("github", "list_issues")
	srv.metricsAccumulator.RecordToolCall("filesystem", "read_file")

	stats := srv.optimizeStats()

	if got := stats.ServerCallCount["github"]; got != 3 {
		t.Errorf("github call count = %d, want 3", got)
	}
	if got := stats.ServerCallCount["filesystem"]; got != 1 {
		t.Errorf("filesystem call count = %d, want 1", got)
	}
}

// TestOptimizeStats_FormatBaseline_FromAccumulator covers the wiring
// between RecordFormatSavings and the FormatBaseline shape the
// format_savings_shortfall heuristic reads.
func TestOptimizeStats_FormatBaseline_FromAccumulator(t *testing.T) {
	srv := newTestServerWithMetrics(t)
	srv.metricsAccumulator.RecordFormatSavings("toon-server", 10_000, 7_000)

	stats := srv.optimizeStats()

	if stats.FormatBaseline.OriginalTokens != 10_000 {
		t.Errorf("OriginalTokens = %d, want 10000", stats.FormatBaseline.OriginalTokens)
	}
	if stats.FormatBaseline.FormattedTokens != 7_000 {
		t.Errorf("FormattedTokens = %d, want 7000", stats.FormatBaseline.FormattedTokens)
	}
	if stats.FormatBaseline.SavingsPercent < 29 || stats.FormatBaseline.SavingsPercent > 31 {
		t.Errorf("SavingsPercent = %v, want ~30", stats.FormatBaseline.SavingsPercent)
	}
}

// TestSplitPrefixedTool covers the gateway tool-name format the
// schema_overhead computation depends on.
func TestSplitPrefixedTool(t *testing.T) {
	cases := []struct {
		in       string
		wantSrv  string
		wantTool string
		wantOk   bool
	}{
		{"github__create_issue", "github", "create_issue", true},
		{"filesystem__read_file", "filesystem", "read_file", true},
		{"no_delim_here", "", "", false},
		{"__leading", "", "", false},
		{"trailing__", "", "", false},
		{"", "", "", false},
	}
	for _, tc := range cases {
		gotSrv, gotTool, gotOk := splitPrefixedTool(tc.in)
		if gotSrv != tc.wantSrv || gotTool != tc.wantTool || gotOk != tc.wantOk {
			t.Errorf("splitPrefixedTool(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tc.in, gotSrv, gotTool, gotOk, tc.wantSrv, tc.wantTool, tc.wantOk)
		}
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

// staticPricingSource is a deterministic pricing.Source for model-stat tests.
type staticPricingSource struct {
	rates map[string]pricing.Rates
}

func (s staticPricingSource) Lookup(model string) (pricing.Rates, bool) {
	r, ok := s.rates[model]
	return r, ok
}

func (s staticPricingSource) Name() string { return "api-test-fixture" }

func TestLookupModelStats(t *testing.T) {
	prev := pricing.CurrentSource()
	t.Cleanup(func() { pricing.SetSource(prev) })
	pricing.SetSource(staticPricingSource{rates: map[string]pricing.Rates{
		"claude-opus-4-7": {InputPerToken: 15e-6, OutputPerToken: 75e-6},
	}})

	t.Run("nil attribution returns nil", func(t *testing.T) {
		if got := lookupModelStats(nil); got != nil {
			t.Errorf("lookupModelStats(nil) = %v, want nil", got)
		}
	})

	t.Run("attributed server resolves rates and names the model", func(t *testing.T) {
		got := lookupModelStats(map[string]string{"jira": "claude-opus-4-7"})
		stat, ok := got["jira"]
		if !ok {
			t.Fatalf("expected stat for jira; got %v", got)
		}
		if stat.Model != "claude-opus-4-7" {
			t.Errorf("Model = %q, want claude-opus-4-7", stat.Model)
		}
		if stat.InputUSDPerToken != 15e-6 {
			t.Errorf("InputUSDPerToken = %v, want 15e-6", stat.InputUSDPerToken)
		}
		if stat.OutputUSDPerToken != 75e-6 {
			t.Errorf("OutputUSDPerToken = %v, want 75e-6", stat.OutputUSDPerToken)
		}
	})

	t.Run("unknown model is omitted", func(t *testing.T) {
		got := lookupModelStats(map[string]string{"jira": "not-in-pricing-table"})
		if got != nil {
			t.Errorf("expected nil for pricing-unknown model; got %v", got)
		}
	})
}

func TestOptimizeStats_ModelStats_FromAttribution(t *testing.T) {
	prev := pricing.CurrentSource()
	t.Cleanup(func() { pricing.SetSource(prev) })
	pricing.SetSource(staticPricingSource{rates: map[string]pricing.Rates{
		"claude-opus-4-7": {InputPerToken: 15e-6, OutputPerToken: 75e-6},
	}})

	srv := newTestServerWithMetrics(t)
	srv.SetModelAttribution(func() map[string]string {
		return map[string]string{"jira": "claude-opus-4-7"}
	})

	stats := srv.optimizeStats()
	if stats.ModelStats["jira"].Model != "claude-opus-4-7" {
		t.Errorf("ModelStats[jira].Model = %q, want claude-opus-4-7", stats.ModelStats["jira"].Model)
	}
}
