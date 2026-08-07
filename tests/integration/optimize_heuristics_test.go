//go:build integration

package integration

import (
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/metrics"
	"github.com/gridctl/gridctl/pkg/optimize"
)

// TestOptimize_AllHeuristicsFireTogether exercises the heuristics
// (schema_overhead, format_savings_shortfall) end-to-end through the
// metrics.Accumulator: per-server tokens, per-tool counts, and
// session format-savings totals all flow into the optimize.Stats
// shape, then optimize.Analyze returns the expected findings.
//
// The test does not spin up a gateway because pkg/optimize is a pure
// reducer — its inputs are already the decoded shape the API handler
// passes in. The integration value here is verifying that the
// metrics.Accumulator's exported snapshots carry the data the
// heuristics need (tool counts, format savings) without further
// transformation, which is the contract the API handler relies on.
func TestOptimize_AllHeuristicsFireTogether(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	acc := metrics.NewAccumulator(100)

	// schema_overhead candidate: lots of schema, few output tokens.
	acc.RecordReplica("fat-schema", -1, 500, 1_000)
	acc.RecordToolCall("fat-schema", "rarely_used")

	// format_savings_shortfall candidate: lots of output tokens, no
	// format conversion, but the session has demonstrated >10% savings
	// from another server using TOON.
	acc.RecordReplica("raw-json", -1, 1_000, 60_000)
	acc.RecordToolCall("raw-json", "list_things")
	acc.RecordReplica("toon-server", -1, 500, 7_000)
	acc.RecordFormatSavings("toon-server", 10_000, 7_000)

	now := time.Now()
	stats := optimize.Stats{
		StackName:        "integration",
		ObservationStart: now.Add(-48 * time.Hour),
		Now:              now,
		Servers: []optimize.ServerInfo{
			{Name: "fat-schema", Tools: []string{"rarely_used", "another", "third"}, Initialized: true},
			{Name: "raw-json", Tools: []string{"list_things"}, Initialized: true},
			{Name: "toon-server", Tools: []string{"emit"}, Initialized: true, OutputFormat: "toon"},
		},
		Usage:          usageFromAccumulator(acc),
		ToolUsage:      toolUsageFromAccumulator(acc),
		PinStats:       map[string]optimize.PinStat{"fat-schema": {SchemaTokens: 8_000}},
		FormatBaseline: formatBaselineFromAccumulator(acc),
	}

	rep := optimize.Analyze(stats, optimize.Options{})

	want := map[string]bool{
		"schema_overhead":          false,
		"format_savings_shortfall": false,
	}
	for _, f := range rep.Findings {
		if _, ok := want[f.Heuristic]; ok {
			want[f.Heuristic] = true
		}
	}
	for h, fired := range want {
		if !fired {
			t.Errorf("expected %q finding to fire; report findings: %s", h, summarizeHeuristics(rep.Findings))
		}
	}
}

func usageFromAccumulator(acc *metrics.Accumulator) map[string]optimize.ServerUsage {
	tokens := acc.Snapshot()
	out := make(map[string]optimize.ServerUsage, len(tokens.PerServer))
	for name, counts := range tokens.PerServer {
		out[name] = optimize.ServerUsage{
			InputTokens:  counts.InputTokens,
			OutputTokens: counts.OutputTokens,
			TotalTokens:  counts.TotalTokens,
		}
	}
	return out
}

func toolUsageFromAccumulator(acc *metrics.Accumulator) map[string]map[string]optimize.ToolStat {
	in := acc.ToolUsageSnapshot()
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]map[string]optimize.ToolStat, len(in))
	for server, tools := range in {
		inner := make(map[string]optimize.ToolStat, len(tools))
		for name, stat := range tools {
			inner[name] = optimize.ToolStat{Calls: stat.Calls, LastCalledAt: stat.LastCalledAt}
		}
		out[server] = inner
	}
	return out
}

func formatBaselineFromAccumulator(acc *metrics.Accumulator) optimize.FormatBaseline {
	saved := acc.Snapshot().FormatSavings
	return optimize.FormatBaseline{
		OriginalTokens:  saved.OriginalTokens,
		FormattedTokens: saved.FormattedTokens,
		SavingsPercent:  saved.SavingsPercent,
	}
}

func summarizeHeuristics(findings []optimize.Finding) string {
	if len(findings) == 0 {
		return "(empty)"
	}
	var s string
	for _, f := range findings {
		if s != "" {
			s += ", "
		}
		s += f.Heuristic + "=" + f.Server
	}
	return s
}
