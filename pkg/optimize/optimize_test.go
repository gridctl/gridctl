package optimize

import (
	"strings"
	"testing"
	"time"
)

// fixedNow gives tests a deterministic "now" so freshness windows and
// the health score remain stable across runs.
var fixedNow = time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)

func baseStats() Stats {
	return Stats{
		StackName:        "test-stack",
		ObservationStart: fixedNow.Add(-48 * time.Hour),
		Now:              fixedNow,
	}
}

func TestAnalyze_NeedMoreData(t *testing.T) {
	stats := baseStats()
	stats.ObservationStart = fixedNow.Add(-30 * time.Minute)

	rep := Analyze(stats, Options{})

	if len(rep.Findings) != 1 {
		t.Fatalf("expected exactly one info finding when observation window is short; got %d", len(rep.Findings))
	}
	got := rep.Findings[0]
	if got.Severity != SeverityInfo {
		t.Errorf("expected info severity; got %q", got.Severity)
	}
	if got.Heuristic != "need_more_data" {
		t.Errorf("heuristic = %q, want need_more_data", got.Heuristic)
	}
	if rep.HealthScore != 100 {
		t.Errorf("HealthScore = %d, want 100", rep.HealthScore)
	}
}

func TestAnalyze_UnusedServer_FiresOnZeroTraffic(t *testing.T) {
	stats := baseStats()
	stats.Servers = []ServerInfo{
		{Name: "github", Tools: []string{"create_issue", "list_issues"}, Initialized: true},
		{Name: "filesystem", Tools: []string{"read_file"}, Initialized: true},
	}
	stats.Usage = map[string]ServerUsage{
		"filesystem": {InputTokens: 1000, OutputTokens: 500, TotalTokens: 1500, TotalCostUSD: 0.01},
	}

	rep := Analyze(stats, Options{})

	var fired *Finding
	for i := range rep.Findings {
		if rep.Findings[i].Heuristic == "unused_server" {
			fired = &rep.Findings[i]
			break
		}
	}
	if fired == nil {
		t.Fatal("expected unused_server finding for github")
	}
	if fired.Server != "github" {
		t.Errorf("Server = %q, want github", fired.Server)
	}
	if fired.Severity != SeverityWarn {
		t.Errorf("Severity = %q, want warn", fired.Severity)
	}
	if fired.ImpactUSDPerWeek <= 0 {
		t.Errorf("ImpactUSDPerWeek = %v, want >0", fired.ImpactUSDPerWeek)
	}
	if !strings.Contains(fired.Remediation, "github") {
		t.Errorf("remediation should reference server name; got %q", fired.Remediation)
	}
}

func TestAnalyze_UnusedServer_SkipsActiveServer(t *testing.T) {
	stats := baseStats()
	stats.Servers = []ServerInfo{
		{Name: "github", Tools: []string{"create_issue"}, Initialized: true},
	}
	stats.Usage = map[string]ServerUsage{
		"github": {TotalTokens: 100, TotalCostUSD: 0.001},
	}
	stats.ToolUsage = map[string]map[string]ToolStat{
		"github": {
			"create_issue": {Calls: 4, LastCalledAt: fixedNow.Add(-1 * time.Hour)},
		},
	}

	rep := Analyze(stats, Options{})

	for _, f := range rep.Findings {
		if f.Heuristic == "unused_server" {
			t.Errorf("did not expect unused_server finding for active server; got %+v", f)
		}
	}
}

func TestAnalyze_UnusedServer_SkipsUninitialized(t *testing.T) {
	stats := baseStats()
	stats.Servers = []ServerInfo{
		{Name: "github", Tools: []string{"create_issue"}, Initialized: false},
	}

	rep := Analyze(stats, Options{})

	for _, f := range rep.Findings {
		if f.Heuristic == "unused_server" {
			t.Errorf("did not expect findings for uninitialized server; got %+v", f)
		}
	}
}

func TestAnalyze_UnusedTool_FiresWhenToolColdInWindow(t *testing.T) {
	stats := baseStats()
	stats.Servers = []ServerInfo{
		{Name: "github", Tools: []string{"create_issue", "list_issues"}, Initialized: true},
	}
	stats.Usage = map[string]ServerUsage{
		"github": {TotalTokens: 100, TotalCostUSD: 0.001},
	}
	stats.ToolUsage = map[string]map[string]ToolStat{
		"github": {
			"create_issue": {Calls: 3, LastCalledAt: fixedNow.Add(-2 * time.Hour)},
			// list_issues never seen.
		},
	}

	rep := Analyze(stats, Options{})

	var hit *Finding
	for i := range rep.Findings {
		if rep.Findings[i].Heuristic == "unused_tool" && rep.Findings[i].Tool == "list_issues" {
			hit = &rep.Findings[i]
			break
		}
	}
	if hit == nil {
		t.Fatal("expected unused_tool finding for list_issues")
	}
	if hit.Server != "github" {
		t.Errorf("Server = %q, want github", hit.Server)
	}
	if !strings.Contains(hit.Remediation, "list_issues") {
		t.Errorf("remediation should reference tool name; got %q", hit.Remediation)
	}
	if hit.ImpactUSDPerWeek <= 0 {
		t.Errorf("ImpactUSDPerWeek = %v, want >0 (schema-tax impact)", hit.ImpactUSDPerWeek)
	}
}

func TestAnalyze_UnusedTool_ImpactFromMeasuredSchemaTokens(t *testing.T) {
	stats := baseStats()
	stats.Servers = []ServerInfo{
		{Name: "github", Tools: []string{"create_issue", "list_issues"}, Initialized: true},
	}
	// Observed rate = 0.001 / 100 = 1e-5 USD per token.
	stats.Usage = map[string]ServerUsage{
		"github": {TotalTokens: 100, TotalCostUSD: 0.001},
	}
	stats.ToolUsage = map[string]map[string]ToolStat{
		"github": {
			"create_issue": {Calls: 3, LastCalledAt: fixedNow.Add(-2 * time.Hour)},
		},
	}
	stats.ToolSchemaTokens = map[string]map[string]int{
		"github": {"list_issues": 800},
	}

	rep := Analyze(stats, Options{})

	hit := findToolFinding(t, rep, "unused_tool", "list_issues")
	// 800 schema tokens × 500 prompts/week × 1e-5 USD/token = $4.00/week.
	want := 800.0 * estimatedPromptsPerWeek * 1e-5
	if diff := hit.ImpactUSDPerWeek - want; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("ImpactUSDPerWeek = %v, want %v", hit.ImpactUSDPerWeek, want)
	}
}

func TestAnalyze_UnusedTool_ImpactFallsBackWithoutSchemaTokens(t *testing.T) {
	stats := baseStats()
	stats.Servers = []ServerInfo{
		{Name: "github", Tools: []string{"create_issue", "list_issues"}, Initialized: true},
	}
	stats.Usage = map[string]ServerUsage{
		"github": {TotalTokens: 100, TotalCostUSD: 0.001},
	}
	stats.ToolUsage = map[string]map[string]ToolStat{
		"github": {
			"create_issue": {Calls: 3, LastCalledAt: fixedNow.Add(-2 * time.Hour)},
		},
	}
	// stats.ToolSchemaTokens intentionally nil — no live tools/list measurement.

	rep := Analyze(stats, Options{})

	hit := findToolFinding(t, rep, "unused_tool", "list_issues")
	// estimatedToolSchemaTokens × 500 prompts/week × observed 1e-5 USD/token.
	want := float64(estimatedToolSchemaTokens) * estimatedPromptsPerWeek * 1e-5
	if diff := hit.ImpactUSDPerWeek - want; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("ImpactUSDPerWeek = %v, want %v (conservative fallback)", hit.ImpactUSDPerWeek, want)
	}
}

func TestAnalyze_UnusedTool_ImpactCappedAtServerEstimate(t *testing.T) {
	stats := baseStats()
	stats.Servers = []ServerInfo{
		{Name: "github", Tools: []string{"create_issue", "list_issues"}, Initialized: true},
	}
	stats.Usage = map[string]ServerUsage{
		"github": {TotalTokens: 100, TotalCostUSD: 0.001},
	}
	stats.ToolUsage = map[string]map[string]ToolStat{
		"github": {
			"create_issue": {Calls: 3, LastCalledAt: fixedNow.Add(-2 * time.Hour)},
		},
	}
	// An oversized single-tool schema must not over-promise.
	stats.ToolSchemaTokens = map[string]map[string]int{
		"github": {"list_issues": 50_000},
	}

	rep := Analyze(stats, Options{})

	hit := findToolFinding(t, rep, "unused_tool", "list_issues")
	want := float64(estimatedSchemaOverheadTokens) * estimatedPromptsPerWeek * 1e-5
	if diff := hit.ImpactUSDPerWeek - want; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("ImpactUSDPerWeek = %v, want %v (capped)", hit.ImpactUSDPerWeek, want)
	}
}

// findToolFinding returns the finding for (heuristic, tool) or fails the test.
func findToolFinding(t *testing.T, rep OptimizeReport, heuristic, tool string) *Finding {
	t.Helper()
	for i := range rep.Findings {
		if rep.Findings[i].Heuristic == heuristic && rep.Findings[i].Tool == tool {
			return &rep.Findings[i]
		}
	}
	t.Fatalf("expected %s finding for %s; findings: %+v", heuristic, tool, rep.Findings)
	return nil
}

func TestAnalyze_UnusedTool_HonorsWhitelist(t *testing.T) {
	stats := baseStats()
	stats.Servers = []ServerInfo{
		{
			Name:          "github",
			Tools:         []string{"create_issue", "list_issues", "delete_repo"},
			ToolWhitelist: []string{"create_issue"},
			Initialized:   true,
		},
	}
	stats.Usage = map[string]ServerUsage{
		"github": {TotalTokens: 100, TotalCostUSD: 0.001},
	}
	stats.ToolUsage = map[string]map[string]ToolStat{
		"github": {
			"create_issue": {Calls: 3, LastCalledAt: fixedNow.Add(-1 * time.Hour)},
		},
	}

	rep := Analyze(stats, Options{})

	for _, f := range rep.Findings {
		if f.Heuristic == "unused_tool" {
			t.Errorf("did not expect unused_tool finding when tool already excluded by whitelist; got %+v", f)
		}
	}
}

func TestAnalyze_UnusedTool_SkippedWithoutPerToolData(t *testing.T) {
	stats := baseStats()
	stats.Servers = []ServerInfo{
		{Name: "github", Tools: []string{"create_issue", "list_issues"}, Initialized: true},
	}
	stats.Usage = map[string]ServerUsage{
		"github": {TotalTokens: 100, TotalCostUSD: 0.001},
	}
	// stats.ToolUsage intentionally nil — legacy gateway with no per-tool tracking.

	rep := Analyze(stats, Options{})

	for _, f := range rep.Findings {
		if f.Heuristic == "unused_tool" {
			t.Errorf("did not expect unused_tool finding without per-tool data; got %+v", f)
		}
	}
}

func TestAnalyze_FindingsSortedBySeverityThenImpact(t *testing.T) {
	stats := baseStats()
	stats.Servers = []ServerInfo{
		{Name: "small", Tools: []string{"a"}, Initialized: true},
		{Name: "large", Tools: []string{"a", "b", "c"}, Initialized: true},
		{Name: "active", Tools: []string{"x"}, Initialized: true},
	}
	stats.Usage = map[string]ServerUsage{
		"active": {TotalTokens: 1000, TotalCostUSD: 0.01},
	}
	stats.ToolUsage = map[string]map[string]ToolStat{
		"active": {
			"x": {Calls: 1, LastCalledAt: fixedNow.Add(-1 * time.Hour)},
		},
	}

	rep := Analyze(stats, Options{})

	// Both unused_server findings are warn; large should sort before small (more tools → higher impact).
	if len(rep.Findings) < 2 {
		t.Fatalf("expected at least 2 findings; got %d", len(rep.Findings))
	}
	if rep.Findings[0].Server != "large" {
		t.Errorf("expected 'large' first by impact; got %q", rep.Findings[0].Server)
	}
	if rep.Findings[1].Server != "small" {
		t.Errorf("expected 'small' second; got %q", rep.Findings[1].Server)
	}
}

func TestAnalyze_MinImpactFilter_RetainsInfo(t *testing.T) {
	stats := baseStats()
	stats.Servers = []ServerInfo{
		{Name: "small", Tools: []string{"a"}, Initialized: true},
	}

	rep := Analyze(stats, Options{MinImpactUSDPerWeek: 1_000_000})
	for _, f := range rep.Findings {
		if f.Severity != SeverityInfo && f.ImpactUSDPerWeek < 1_000_000 {
			t.Errorf("min-impact filter let through low-impact non-info finding %+v", f)
		}
	}
}

func TestAnalyze_HealthScore_DropsOnWarn(t *testing.T) {
	stats := baseStats()
	stats.Servers = []ServerInfo{
		{Name: "a", Tools: []string{"x"}, Initialized: true},
		{Name: "b", Tools: []string{"y"}, Initialized: true},
	}

	rep := Analyze(stats, Options{})

	// Two unused_server warnings → 100 - 20 = 80.
	if rep.HealthScore != 80 {
		t.Errorf("HealthScore = %d, want 80", rep.HealthScore)
	}
}

func TestAnalyze_NoFindings_HealthScore100(t *testing.T) {
	stats := baseStats()
	stats.Servers = []ServerInfo{
		{Name: "a", Tools: []string{"x"}, Initialized: true},
	}
	stats.Usage = map[string]ServerUsage{
		"a": {TotalTokens: 100, TotalCostUSD: 0.001},
	}
	stats.ToolUsage = map[string]map[string]ToolStat{
		"a": {"x": {Calls: 1, LastCalledAt: fixedNow.Add(-1 * time.Hour)}},
	}

	rep := Analyze(stats, Options{})

	if rep.HealthScore != 100 {
		t.Errorf("HealthScore = %d, want 100", rep.HealthScore)
	}
	if len(rep.Findings) != 0 {
		t.Errorf("expected zero findings; got %d", len(rep.Findings))
	}
}

func TestAnalyze_SchemaOverhead_FiresOnLowRatio(t *testing.T) {
	stats := baseStats()
	stats.Servers = []ServerInfo{
		{Name: "fat-schema", Tools: []string{"a", "b", "c"}, Initialized: true},
	}
	stats.Usage = map[string]ServerUsage{
		"fat-schema": {OutputTokens: 5_000, TotalTokens: 5_000, TotalCostUSD: 0.015},
	}
	stats.PinStats = map[string]PinStat{
		"fat-schema": {SchemaTokens: 8_000},
	}
	stats.ToolUsage = map[string]map[string]ToolStat{
		"fat-schema": {"a": {Calls: 1, LastCalledAt: fixedNow.Add(-1 * time.Hour)}},
	}

	rep := Analyze(stats, Options{})

	var hit *Finding
	for i := range rep.Findings {
		if rep.Findings[i].Heuristic == "schema_overhead" {
			hit = &rep.Findings[i]
			break
		}
	}
	if hit == nil {
		t.Fatal("expected schema_overhead finding")
	}
	if hit.Server != "fat-schema" {
		t.Errorf("Server = %q, want fat-schema", hit.Server)
	}
	if hit.ImpactUSDPerWeek <= 0 {
		t.Errorf("expected non-zero impact; got %v", hit.ImpactUSDPerWeek)
	}
	if !strings.Contains(hit.Remediation, "tools:") {
		t.Errorf("remediation should suggest pruning tools; got %q", hit.Remediation)
	}
}

func TestAnalyze_SchemaOverhead_SkipsHighRatio(t *testing.T) {
	stats := baseStats()
	stats.Servers = []ServerInfo{
		{Name: "lean", Tools: []string{"a"}, Initialized: true},
	}
	stats.Usage = map[string]ServerUsage{
		// Output tokens >> schema tokens — the server delivers
		// real value relative to its schema size.
		"lean": {OutputTokens: 100_000, TotalTokens: 100_000, TotalCostUSD: 0.30},
	}
	stats.PinStats = map[string]PinStat{
		"lean": {SchemaTokens: 3_000},
	}
	stats.ToolUsage = map[string]map[string]ToolStat{
		"lean": {"a": {Calls: 50, LastCalledAt: fixedNow.Add(-1 * time.Hour)}},
	}

	rep := Analyze(stats, Options{})

	for _, f := range rep.Findings {
		if f.Heuristic == "schema_overhead" {
			t.Errorf("did not expect schema_overhead finding for high-ratio server; got %+v", f)
		}
	}
}

func TestAnalyze_SchemaOverhead_SkipsBelowFloor(t *testing.T) {
	stats := baseStats()
	stats.Servers = []ServerInfo{
		{Name: "tiny", Tools: []string{"a"}, Initialized: true},
	}
	stats.Usage = map[string]ServerUsage{
		"tiny": {OutputTokens: 100, TotalTokens: 100, TotalCostUSD: 0.0003},
	}
	stats.PinStats = map[string]PinStat{
		// Below schemaOverheadMinSchemaTokens — heuristic stays silent.
		"tiny": {SchemaTokens: 500},
	}

	rep := Analyze(stats, Options{})

	for _, f := range rep.Findings {
		if f.Heuristic == "schema_overhead" {
			t.Errorf("did not expect schema_overhead finding below schema floor; got %+v", f)
		}
	}
}

func TestAnalyze_SchemaOverhead_NilPinStatsIsSilent(t *testing.T) {
	stats := baseStats()
	stats.Servers = []ServerInfo{
		{Name: "any", Tools: []string{"a"}, Initialized: true},
	}
	stats.Usage = map[string]ServerUsage{
		"any": {OutputTokens: 100, TotalTokens: 100, TotalCostUSD: 0.0003},
	}

	rep := Analyze(stats, Options{})

	for _, f := range rep.Findings {
		if f.Heuristic == "schema_overhead" {
			t.Errorf("schema_overhead must skip when PinStats is nil; got %+v", f)
		}
	}
}

func TestAnalyze_FormatShortfall_FiresWhenBaselineDemonstrated(t *testing.T) {
	stats := baseStats()
	stats.Servers = []ServerInfo{
		{Name: "raw-json", Tools: []string{"a"}, Initialized: true, OutputFormat: ""},
	}
	stats.Usage = map[string]ServerUsage{
		"raw-json": {OutputTokens: 50_000, TotalTokens: 50_000, TotalCostUSD: 0.15},
	}
	stats.FormatBaseline = FormatBaseline{
		OriginalTokens:  10_000,
		FormattedTokens: 7_000,
		SavingsPercent:  30.0,
	}
	stats.ToolUsage = map[string]map[string]ToolStat{
		"raw-json": {"a": {Calls: 5, LastCalledAt: fixedNow.Add(-1 * time.Hour)}},
	}

	rep := Analyze(stats, Options{})

	var hit *Finding
	for i := range rep.Findings {
		if rep.Findings[i].Heuristic == "format_savings_shortfall" {
			hit = &rep.Findings[i]
			break
		}
	}
	if hit == nil {
		t.Fatal("expected format_savings_shortfall finding")
	}
	if hit.Server != "raw-json" {
		t.Errorf("Server = %q, want raw-json", hit.Server)
	}
	if hit.ImpactUSDPerWeek <= 0 {
		t.Errorf("expected positive impact; got %v", hit.ImpactUSDPerWeek)
	}
	if !strings.Contains(hit.Remediation, "output_format") {
		t.Errorf("remediation should mention output_format; got %q", hit.Remediation)
	}
}

func TestAnalyze_FormatShortfall_SkipsServerAlreadyConverting(t *testing.T) {
	stats := baseStats()
	stats.Servers = []ServerInfo{
		{Name: "toon-server", Tools: []string{"a"}, Initialized: true, OutputFormat: "toon"},
	}
	stats.Usage = map[string]ServerUsage{
		"toon-server": {OutputTokens: 50_000, TotalTokens: 50_000, TotalCostUSD: 0.15},
	}
	stats.FormatBaseline = FormatBaseline{SavingsPercent: 30.0}
	stats.ToolUsage = map[string]map[string]ToolStat{
		"toon-server": {"a": {Calls: 5, LastCalledAt: fixedNow.Add(-1 * time.Hour)}},
	}

	rep := Analyze(stats, Options{})

	for _, f := range rep.Findings {
		if f.Heuristic == "format_savings_shortfall" {
			t.Errorf("did not expect finding for server already using output_format; got %+v", f)
		}
	}
}

func TestAnalyze_FormatShortfall_SilentWithoutBaseline(t *testing.T) {
	stats := baseStats()
	stats.Servers = []ServerInfo{
		{Name: "raw-json", Tools: []string{"a"}, Initialized: true},
	}
	stats.Usage = map[string]ServerUsage{
		"raw-json": {OutputTokens: 100_000, TotalTokens: 100_000, TotalCostUSD: 0.30},
	}
	// FormatBaseline left at zero — no demonstrated savings.
	stats.ToolUsage = map[string]map[string]ToolStat{
		"raw-json": {"a": {Calls: 5, LastCalledAt: fixedNow.Add(-1 * time.Hour)}},
	}

	rep := Analyze(stats, Options{})

	for _, f := range rep.Findings {
		if f.Heuristic == "format_savings_shortfall" {
			t.Errorf("must stay silent without a baseline; got %+v", f)
		}
	}
}

func TestAnalyze_FormatShortfall_SkipsBelowOutputFloor(t *testing.T) {
	stats := baseStats()
	stats.Servers = []ServerInfo{
		{Name: "small", Tools: []string{"a"}, Initialized: true},
	}
	stats.Usage = map[string]ServerUsage{
		// Below formatShortfallMinOutputTokens.
		"small": {OutputTokens: 1_000, TotalTokens: 1_000, TotalCostUSD: 0.003},
	}
	stats.FormatBaseline = FormatBaseline{SavingsPercent: 30.0}
	stats.ToolUsage = map[string]map[string]ToolStat{
		"small": {"a": {Calls: 5, LastCalledAt: fixedNow.Add(-1 * time.Hour)}},
	}

	rep := Analyze(stats, Options{})

	for _, f := range rep.Findings {
		if f.Heuristic == "format_savings_shortfall" {
			t.Errorf("must skip server below output floor; got %+v", f)
		}
	}
}

func TestAnalyze_ExpensiveModel_FiresOnHighRateLowAvgTokens(t *testing.T) {
	stats := baseStats()
	stats.Servers = []ServerInfo{
		{Name: "lookup", Tools: []string{"get_user"}, Initialized: true},
	}
	stats.Usage = map[string]ServerUsage{
		// 50 calls × 20 tokens each = 1000 tokens. At Opus rate that's a
		// non-trivial cost, but each call is cheap-task small.
		"lookup": {OutputTokens: 500, TotalTokens: 1_000, TotalCostUSD: 0.015},
	}
	stats.ServerCallCount = map[string]int64{"lookup": 50}
	stats.ModelStats = map[string]ModelStat{
		"lookup": {Model: "claude-opus-4-7", InputUSDPerToken: 15.0 / 1_000_000.0},
	}
	stats.ToolUsage = map[string]map[string]ToolStat{
		"lookup": {"get_user": {Calls: 50, LastCalledAt: fixedNow.Add(-1 * time.Hour)}},
	}

	rep := Analyze(stats, Options{})

	var hit *Finding
	for i := range rep.Findings {
		if rep.Findings[i].Heuristic == "expensive_model_on_cheap_task" {
			hit = &rep.Findings[i]
			break
		}
	}
	if hit == nil {
		t.Fatal("expected expensive_model_on_cheap_task finding")
	}
	if hit.Severity != SeverityInfo {
		t.Errorf("Severity = %q, want info (informational only in v1)", hit.Severity)
	}
	if !strings.Contains(hit.Summary, "claude-opus-4-7") {
		t.Errorf("summary should name the model; got %q", hit.Summary)
	}
	if hit.Model != "claude-opus-4-7" {
		t.Errorf("finding Model = %q, want claude-opus-4-7", hit.Model)
	}
	if hit.Provenance != "declared" {
		t.Errorf("finding Provenance = %q, want declared", hit.Provenance)
	}
	if hit.ImpactUSDPerWeek != 0 {
		t.Errorf("impact must be zero (informational only); got %v", hit.ImpactUSDPerWeek)
	}
}

// TestAnalyze_ExpensiveModel_NamesDominantHistogramModel verifies the finding
// names the model that priced the most cost (from the histogram) even when no
// declared ModelStat exists, and labels its provenance declared.
func TestAnalyze_ExpensiveModel_NamesDominantHistogramModel(t *testing.T) {
	stats := baseStats()
	stats.Servers = []ServerInfo{
		{Name: "lookup", Tools: []string{"get_user"}, Initialized: true},
	}
	// Effective rate = 0.0005 / 10 = 5e-5 = $50/M, above threshold.
	stats.Usage = map[string]ServerUsage{
		"lookup": {OutputTokens: 5, TotalTokens: 10, TotalCostUSD: 0.0005},
	}
	stats.ServerCallCount = map[string]int64{"lookup": 50}
	stats.ToolUsage = map[string]map[string]ToolStat{
		"lookup": {"get_user": {Calls: 50, LastCalledAt: fixedNow.Add(-1 * time.Hour)}},
	}
	// No declared ModelStat; the histogram names the dominant model exactly.
	stats.ModelHistograms = map[string]map[string]float64{
		"lookup": {"claude-opus-4-7": 0.0004, "claude-haiku-4-5": 0.0001},
	}

	rep := Analyze(stats, Options{})
	var hit *Finding
	for i := range rep.Findings {
		if rep.Findings[i].Heuristic == "expensive_model_on_cheap_task" {
			hit = &rep.Findings[i]
			break
		}
	}
	if hit == nil {
		t.Fatal("expected expensive_model_on_cheap_task finding")
	}
	if hit.Model != "claude-opus-4-7" {
		t.Errorf("finding should name dominant histogram model; got Model=%q", hit.Model)
	}
	if hit.Provenance != "declared" {
		t.Errorf("provenance = %q, want declared", hit.Provenance)
	}
	if !strings.Contains(hit.Summary, "claude-opus-4-7") {
		t.Errorf("summary should name the dominant model; got %q", hit.Summary)
	}
}

func TestAnalyze_ExpensiveModel_InfersRateFromCostWhenModelStatsAbsent(t *testing.T) {
	stats := baseStats()
	stats.Servers = []ServerInfo{
		{Name: "lookup", Tools: []string{"get_user"}, Initialized: true},
	}
	// Effective rate = 0.0001 / 10 = 1e-5 = $10/M, well above threshold.
	stats.Usage = map[string]ServerUsage{
		"lookup": {OutputTokens: 5, TotalTokens: 10, TotalCostUSD: 0.0001},
	}
	stats.ServerCallCount = map[string]int64{"lookup": 10}
	stats.ToolUsage = map[string]map[string]ToolStat{
		"lookup": {"get_user": {Calls: 10, LastCalledAt: fixedNow.Add(-1 * time.Hour)}},
	}

	rep := Analyze(stats, Options{})

	var hit *Finding
	for i := range rep.Findings {
		if rep.Findings[i].Heuristic == "expensive_model_on_cheap_task" {
			hit = &rep.Findings[i]
			break
		}
	}
	if hit == nil {
		t.Fatal("expected expensive_model_on_cheap_task finding via inferred rate")
	}
}

func TestAnalyze_ExpensiveModel_SkipsLargeAvgCalls(t *testing.T) {
	stats := baseStats()
	stats.Servers = []ServerInfo{
		{Name: "verbose", Tools: []string{"summarize"}, Initialized: true},
	}
	// Avg tokens per call = 5000 — well above expensiveModelMaxAvgTokensPerCall.
	stats.Usage = map[string]ServerUsage{
		"verbose": {OutputTokens: 25_000, TotalTokens: 50_000, TotalCostUSD: 0.75},
	}
	stats.ServerCallCount = map[string]int64{"verbose": 10}
	stats.ModelStats = map[string]ModelStat{
		"verbose": {Model: "claude-opus-4-7", InputUSDPerToken: 15.0 / 1_000_000.0},
	}
	stats.ToolUsage = map[string]map[string]ToolStat{
		"verbose": {"summarize": {Calls: 10, LastCalledAt: fixedNow.Add(-1 * time.Hour)}},
	}

	rep := Analyze(stats, Options{})

	for _, f := range rep.Findings {
		if f.Heuristic == "expensive_model_on_cheap_task" {
			t.Errorf("must skip when avg tokens per call is large; got %+v", f)
		}
	}
}

func TestAnalyze_ExpensiveModel_SkipsCheapModel(t *testing.T) {
	stats := baseStats()
	stats.Servers = []ServerInfo{
		{Name: "lookup", Tools: []string{"get_user"}, Initialized: true},
	}
	stats.Usage = map[string]ServerUsage{
		"lookup": {OutputTokens: 500, TotalTokens: 1_000, TotalCostUSD: 0.0003},
	}
	stats.ServerCallCount = map[string]int64{"lookup": 50}
	// Haiku-tier rate — below the threshold.
	stats.ModelStats = map[string]ModelStat{
		"lookup": {Model: "claude-haiku-4-5", InputUSDPerToken: 0.25 / 1_000_000.0},
	}
	stats.ToolUsage = map[string]map[string]ToolStat{
		"lookup": {"get_user": {Calls: 50, LastCalledAt: fixedNow.Add(-1 * time.Hour)}},
	}

	rep := Analyze(stats, Options{})

	for _, f := range rep.Findings {
		if f.Heuristic == "expensive_model_on_cheap_task" {
			t.Errorf("must skip cheap-model traffic; got %+v", f)
		}
	}
}

func TestAnalyze_ExpensiveModel_SkipsBelowMinCalls(t *testing.T) {
	stats := baseStats()
	stats.Servers = []ServerInfo{
		{Name: "lookup", Tools: []string{"get_user"}, Initialized: true},
	}
	stats.Usage = map[string]ServerUsage{
		"lookup": {OutputTokens: 30, TotalTokens: 60, TotalCostUSD: 0.001},
	}
	stats.ServerCallCount = map[string]int64{"lookup": 3} // < expensiveModelMinCalls
	stats.ModelStats = map[string]ModelStat{
		"lookup": {Model: "claude-opus-4-7", InputUSDPerToken: 15.0 / 1_000_000.0},
	}

	rep := Analyze(stats, Options{})

	for _, f := range rep.Findings {
		if f.Heuristic == "expensive_model_on_cheap_task" {
			t.Errorf("must skip when call count is below threshold; got %+v", f)
		}
	}
}

func TestSeverity_IsActionable(t *testing.T) {
	cases := []struct {
		s    Severity
		want bool
	}{
		{SeverityInfo, false},
		{SeverityWarn, true},
		{SeverityCritical, true},
	}
	for _, tc := range cases {
		if got := tc.s.IsActionable(); got != tc.want {
			t.Errorf("Severity(%q).IsActionable() = %v, want %v", tc.s, got, tc.want)
		}
	}
}
