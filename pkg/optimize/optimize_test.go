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
