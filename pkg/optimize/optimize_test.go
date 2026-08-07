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
		"filesystem": {InputTokens: 1000, OutputTokens: 500, TotalTokens: 1500},
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
	if fired.ImpactTokensPerWeek <= 0 {
		t.Errorf("ImpactTokensPerWeek = %v, want >0", fired.ImpactTokensPerWeek)
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
		"github": {TotalTokens: 100},
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
		"github": {TotalTokens: 100},
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
	if hit.ImpactTokensPerWeek <= 0 {
		t.Errorf("ImpactTokensPerWeek = %v, want >0 (schema-tax impact)", hit.ImpactTokensPerWeek)
	}
}

func TestAnalyze_UnusedTool_ImpactFromMeasuredSchemaTokens(t *testing.T) {
	stats := baseStats()
	stats.Servers = []ServerInfo{
		{Name: "github", Tools: []string{"create_issue", "list_issues"}, Initialized: true},
	}
	stats.Usage = map[string]ServerUsage{
		"github": {TotalTokens: 100},
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
	// 800 schema tokens × 500 prompts/week = 400,000 tokens/week.
	want := int64(800) * estimatedPromptsPerWeek
	if hit.ImpactTokensPerWeek != want {
		t.Errorf("ImpactTokensPerWeek = %v, want %v", hit.ImpactTokensPerWeek, want)
	}
}

func TestAnalyze_UnusedTool_ImpactFallsBackWithoutSchemaTokens(t *testing.T) {
	stats := baseStats()
	stats.Servers = []ServerInfo{
		{Name: "github", Tools: []string{"create_issue", "list_issues"}, Initialized: true},
	}
	stats.Usage = map[string]ServerUsage{
		"github": {TotalTokens: 100},
	}
	stats.ToolUsage = map[string]map[string]ToolStat{
		"github": {
			"create_issue": {Calls: 3, LastCalledAt: fixedNow.Add(-2 * time.Hour)},
		},
	}
	// stats.ToolSchemaTokens intentionally nil — no live tools/list measurement.

	rep := Analyze(stats, Options{})

	hit := findToolFinding(t, rep, "unused_tool", "list_issues")
	// estimatedToolSchemaTokens × 500 prompts/week.
	want := int64(estimatedToolSchemaTokens) * estimatedPromptsPerWeek
	if hit.ImpactTokensPerWeek != want {
		t.Errorf("ImpactTokensPerWeek = %v, want %v (conservative fallback)", hit.ImpactTokensPerWeek, want)
	}
}

func TestAnalyze_UnusedTool_ImpactCappedAtServerEstimate(t *testing.T) {
	stats := baseStats()
	stats.Servers = []ServerInfo{
		{Name: "github", Tools: []string{"create_issue", "list_issues"}, Initialized: true},
	}
	stats.Usage = map[string]ServerUsage{
		"github": {TotalTokens: 100},
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
	want := int64(estimatedSchemaOverheadTokens) * estimatedPromptsPerWeek
	if hit.ImpactTokensPerWeek != want {
		t.Errorf("ImpactTokensPerWeek = %v, want %v (capped)", hit.ImpactTokensPerWeek, want)
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
		"github": {TotalTokens: 100},
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
		"github": {TotalTokens: 100},
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
		"active": {TotalTokens: 1000},
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

	rep := Analyze(stats, Options{MinImpactTokensPerWeek: 1_000_000})
	for _, f := range rep.Findings {
		if f.Severity != SeverityInfo && f.ImpactTokensPerWeek < 1_000_000 {
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
		"a": {TotalTokens: 100},
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
		"fat-schema": {OutputTokens: 5_000, TotalTokens: 5_000},
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
	if hit.ImpactTokensPerWeek <= 0 {
		t.Errorf("expected non-zero impact; got %v", hit.ImpactTokensPerWeek)
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
		"lean": {OutputTokens: 100_000, TotalTokens: 100_000},
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
		"tiny": {OutputTokens: 100, TotalTokens: 100},
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
		"any": {OutputTokens: 100, TotalTokens: 100},
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
		"raw-json": {OutputTokens: 50_000, TotalTokens: 50_000},
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
	// Observed savings (50,000 × 30% = 15,000 tokens over a 48h window)
	// normalize to a weekly rate: 15,000 × 7d/2d = 52,500 tokens/week.
	if want := int64(52_500); hit.ImpactTokensPerWeek != want {
		t.Errorf("ImpactTokensPerWeek = %v, want %v (weekly-normalized)", hit.ImpactTokensPerWeek, want)
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
		"toon-server": {OutputTokens: 50_000, TotalTokens: 50_000},
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
		"raw-json": {OutputTokens: 100_000, TotalTokens: 100_000},
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
		"small": {OutputTokens: 1_000, TotalTokens: 1_000},
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

// TestAnalyze_ExpensiveModel_NamesDominantHistogramModel verifies the finding
// names the model that priced the most cost (from the histogram) even when no
// declared ModelStat exists, and labels its provenance declared.

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

func TestWeeklyRate(t *testing.T) {
	now := fixedNow
	cases := []struct {
		name     string
		observed int64
		start    time.Time
		want     int64
	}{
		{"two-day window scales up 3.5x", 15_000, now.Add(-48 * time.Hour), 52_500},
		{"two-week window scales down", 14_000, now.Add(-14 * 24 * time.Hour), 7_000},
		{"exactly one week is identity", 9_000, now.Add(-7 * 24 * time.Hour), 9_000},
		{"zero start passes through unscaled", 5_000, time.Time{}, 5_000},
		{"non-positive window passes through", 5_000, now.Add(time.Hour), 5_000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := weeklyRate(tc.observed, tc.start, now); got != tc.want {
				t.Errorf("weeklyRate(%d) = %d, want %d", tc.observed, got, tc.want)
			}
		})
	}
}
