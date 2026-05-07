package metrics

import (
	"encoding/json"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAccumulator_Record(t *testing.T) {
	acc := NewAccumulator(100)

	acc.Record("server-a", 100, 50)
	acc.Record("server-b", 200, 100)
	acc.Record("server-a", 50, 25)

	snap := acc.Snapshot()

	if snap.Session.InputTokens != 350 {
		t.Errorf("session input = %d, want 350", snap.Session.InputTokens)
	}
	if snap.Session.OutputTokens != 175 {
		t.Errorf("session output = %d, want 175", snap.Session.OutputTokens)
	}
	if snap.Session.TotalTokens != 525 {
		t.Errorf("session total = %d, want 525", snap.Session.TotalTokens)
	}

	serverA := snap.PerServer["server-a"]
	if serverA.InputTokens != 150 {
		t.Errorf("server-a input = %d, want 150", serverA.InputTokens)
	}
	if serverA.OutputTokens != 75 {
		t.Errorf("server-a output = %d, want 75", serverA.OutputTokens)
	}

	serverB := snap.PerServer["server-b"]
	if serverB.TotalTokens != 300 {
		t.Errorf("server-b total = %d, want 300", serverB.TotalTokens)
	}
}

func TestAccumulator_Clear(t *testing.T) {
	acc := NewAccumulator(100)
	acc.Record("server-a", 100, 50)
	acc.RecordReplica("server-a", 0, 10, 5)

	acc.Clear()

	snap := acc.Snapshot()
	if snap.Session.TotalTokens != 0 {
		t.Errorf("session total after clear = %d, want 0", snap.Session.TotalTokens)
	}
	if len(snap.PerServer) != 0 {
		t.Errorf("per-server count after clear = %d, want 0", len(snap.PerServer))
	}
	if len(snap.PerReplica) != 0 {
		t.Errorf("per-replica count after clear = %d, want 0", len(snap.PerReplica))
	}
}

func TestAccumulator_RecordReplica(t *testing.T) {
	acc := NewAccumulator(100)

	// Two replicas of the same server + one server without replicas.
	acc.RecordReplica("junos", 0, 100, 50)
	acc.RecordReplica("junos", 0, 40, 20)
	acc.RecordReplica("junos", 1, 60, 30)
	acc.Record("github", 10, 5) // no replica_id — should not produce a per-replica entry

	snap := acc.Snapshot()

	// Per-server aggregates still sum across replicas.
	junos := snap.PerServer["junos"]
	if junos.InputTokens != 200 || junos.OutputTokens != 100 {
		t.Errorf("per-server junos = %+v, want input=200 output=100", junos)
	}

	// Per-replica map is keyed by (server, replica_id).
	replicaMap, ok := snap.PerReplica["junos"]
	if !ok {
		t.Fatalf("expected junos in per_replica; got %+v", snap.PerReplica)
	}
	if got := replicaMap[0].InputTokens; got != 140 {
		t.Errorf("junos replica 0 input = %d, want 140", got)
	}
	if got := replicaMap[1].InputTokens; got != 60 {
		t.Errorf("junos replica 1 input = %d, want 60", got)
	}

	// Servers without replica_id should not appear under per_replica.
	if _, ok := snap.PerReplica["github"]; ok {
		t.Errorf("expected github absent from per_replica when recorded without replica_id")
	}
}

func TestAccumulator_RecordNegativeReplicaIDSkipsReplicaMap(t *testing.T) {
	acc := NewAccumulator(100)
	acc.RecordReplica("server-a", -1, 100, 50)

	snap := acc.Snapshot()
	if _, ok := snap.PerServer["server-a"]; !ok {
		t.Error("expected per-server entry for server-a even when replicaID=-1")
	}
	if _, ok := snap.PerReplica["server-a"]; ok {
		t.Error("expected no per-replica entry when replicaID=-1")
	}
}

func TestAccumulator_Query(t *testing.T) {
	acc := NewAccumulator(100)

	// Record some data
	acc.Record("server-a", 100, 50)
	acc.Record("server-b", 200, 100)

	result := acc.Query(time.Hour)

	if result.Range != "1h" {
		t.Errorf("range = %q, want %q", result.Range, "1h")
	}
	if result.Interval != "1m" {
		t.Errorf("interval = %q, want %q", result.Interval, "1m")
	}
	if len(result.Points) == 0 {
		t.Error("expected at least 1 data point")
	}

	// Aggregate point should have combined tokens
	total := int64(0)
	for _, p := range result.Points {
		total += p.TotalTokens
	}
	if total != 450 {
		t.Errorf("total across points = %d, want 450", total)
	}

	// Per-server should have entries
	if _, ok := result.PerServer["server-a"]; !ok {
		t.Error("expected server-a in per_server")
	}
	if _, ok := result.PerServer["server-b"]; !ok {
		t.Error("expected server-b in per_server")
	}
}

func TestAccumulator_QueryDownsample(t *testing.T) {
	acc := NewAccumulator(100)
	acc.Record("server-a", 100, 50)

	// Query with > 6h to trigger downsampling
	result := acc.Query(24 * time.Hour)

	if result.Interval != "1h" {
		t.Errorf("interval = %q, want %q for 24h range", result.Interval, "1h")
	}
	if result.Range != "24h" {
		t.Errorf("range = %q, want %q", result.Range, "24h")
	}
}

func TestAccumulator_ConcurrentAccess(t *testing.T) {
	acc := NewAccumulator(100)
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			server := "server-a"
			if n%2 == 0 {
				server = "server-b"
			}
			acc.Record(server, 10, 5)
		}(i)
	}

	wg.Wait()

	snap := acc.Snapshot()
	if snap.Session.InputTokens != 1000 {
		t.Errorf("session input after concurrent writes = %d, want 1000", snap.Session.InputTokens)
	}
	if snap.Session.OutputTokens != 500 {
		t.Errorf("session output after concurrent writes = %d, want 500", snap.Session.OutputTokens)
	}
}

func TestAccumulator_DefaultMaxSize(t *testing.T) {
	acc := NewAccumulator(0)
	if acc.maxSize != 10000 {
		t.Errorf("default maxSize = %d, want 10000", acc.maxSize)
	}

	acc = NewAccumulator(-1)
	if acc.maxSize != 10000 {
		t.Errorf("negative maxSize = %d, want 10000", acc.maxSize)
	}
}

func TestAccumulator_FormatSavingsZero(t *testing.T) {
	acc := NewAccumulator(100)
	acc.Record("server-a", 100, 50)

	snap := acc.Snapshot()
	if snap.FormatSavings.SavingsPercent != 0 {
		t.Errorf("savings percent = %f, want 0", snap.FormatSavings.SavingsPercent)
	}
}

func TestAccumulator_RecordFormatSavings(t *testing.T) {
	acc := NewAccumulator(100)

	// Record savings: 1000 original tokens → 600 formatted tokens
	acc.RecordFormatSavings("server-a", 1000, 600)

	snap := acc.Snapshot()

	// Normal token tracking should be unaffected (savings-only method)
	if snap.Session.InputTokens != 0 {
		t.Errorf("session input = %d, want 0", snap.Session.InputTokens)
	}

	// Format savings should be populated
	if snap.FormatSavings.OriginalTokens != 1000 {
		t.Errorf("original tokens = %d, want 1000", snap.FormatSavings.OriginalTokens)
	}
	if snap.FormatSavings.FormattedTokens != 600 {
		t.Errorf("formatted tokens = %d, want 600", snap.FormatSavings.FormattedTokens)
	}
	if snap.FormatSavings.SavedTokens != 400 {
		t.Errorf("saved tokens = %d, want 400", snap.FormatSavings.SavedTokens)
	}
	if snap.FormatSavings.SavingsPercent != 40.0 {
		t.Errorf("savings percent = %f, want 40.0", snap.FormatSavings.SavingsPercent)
	}
}

func TestAccumulator_RecordFormatSavings_Cumulative(t *testing.T) {
	acc := NewAccumulator(100)

	acc.RecordFormatSavings("server-a", 500, 300)
	acc.RecordFormatSavings("server-b", 500, 300)

	snap := acc.Snapshot()
	if snap.FormatSavings.OriginalTokens != 1000 {
		t.Errorf("cumulative original = %d, want 1000", snap.FormatSavings.OriginalTokens)
	}
	if snap.FormatSavings.FormattedTokens != 600 {
		t.Errorf("cumulative formatted = %d, want 600", snap.FormatSavings.FormattedTokens)
	}
	if snap.FormatSavings.SavedTokens != 400 {
		t.Errorf("cumulative saved = %d, want 400", snap.FormatSavings.SavedTokens)
	}
}

func TestAccumulator_RecordFormatSavings_ClearResets(t *testing.T) {
	acc := NewAccumulator(100)
	acc.RecordFormatSavings("server-a", 1000, 600)

	acc.Clear()

	snap := acc.Snapshot()
	if snap.FormatSavings.OriginalTokens != 0 {
		t.Errorf("original after clear = %d, want 0", snap.FormatSavings.OriginalTokens)
	}
	if snap.FormatSavings.SavingsPercent != 0 {
		t.Errorf("savings percent after clear = %f, want 0", snap.FormatSavings.SavingsPercent)
	}
}

func TestAccumulator_RecordFormatSavings_Concurrent(t *testing.T) {
	acc := NewAccumulator(100)
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			acc.RecordFormatSavings("server-a", 100, 60)
		}()
	}

	wg.Wait()

	snap := acc.Snapshot()
	if snap.FormatSavings.OriginalTokens != 10000 {
		t.Errorf("concurrent original = %d, want 10000", snap.FormatSavings.OriginalTokens)
	}
	if snap.FormatSavings.FormattedTokens != 6000 {
		t.Errorf("concurrent formatted = %d, want 6000", snap.FormatSavings.FormattedTokens)
	}
}

func TestAccumulator_RecordFormatSavings_IndependentFromRecord(t *testing.T) {
	acc := NewAccumulator(100)

	// Normal tracking via Record
	acc.Record("server-a", 100, 50)
	// Format savings via RecordFormatSavings
	acc.RecordFormatSavings("server-a", 500, 300)

	snap := acc.Snapshot()

	// Session totals should only include Record() data
	if snap.Session.InputTokens != 100 {
		t.Errorf("session input = %d, want 100 (only from Record)", snap.Session.InputTokens)
	}
	if snap.Session.OutputTokens != 50 {
		t.Errorf("session output = %d, want 50 (only from Record)", snap.Session.OutputTokens)
	}

	// Format savings should be independent
	if snap.FormatSavings.OriginalTokens != 500 {
		t.Errorf("original = %d, want 500", snap.FormatSavings.OriginalTokens)
	}
	if snap.FormatSavings.SavedTokens != 200 {
		t.Errorf("saved = %d, want 200", snap.FormatSavings.SavedTokens)
	}
}

func TestFormatRange(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Minute, "30m"},
		{time.Hour, "1h"},
		{6 * time.Hour, "6h"},
		{24 * time.Hour, "24h"},
		{7 * 24 * time.Hour, "7d"},
	}
	for _, tt := range tests {
		got := formatRange(tt.d)
		if got != tt.want {
			t.Errorf("formatRange(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestDownsampleToHour(t *testing.T) {
	now := time.Now().Truncate(time.Hour)

	buckets := []bucket{
		{timestamp: now, inputTokens: 100, outputTokens: 50},
		{timestamp: now.Add(time.Minute), inputTokens: 200, outputTokens: 100},
		{timestamp: now.Add(time.Hour), inputTokens: 300, outputTokens: 150},
	}

	result := downsampleToHour(buckets)

	if len(result) != 2 {
		t.Fatalf("expected 2 hourly buckets, got %d", len(result))
	}

	// First hour: 100+200=300 input, 50+100=150 output
	if result[0].InputTokens != 300 {
		t.Errorf("hour 1 input = %d, want 300", result[0].InputTokens)
	}
	if result[0].OutputTokens != 150 {
		t.Errorf("hour 1 output = %d, want 150", result[0].OutputTokens)
	}

	// Second hour: 300 input, 150 output
	if result[1].InputTokens != 300 {
		t.Errorf("hour 2 input = %d, want 300", result[1].InputTokens)
	}
}

// --- Cost layer tests ---

func TestAccumulator_RecordCost_SessionAndPerServer(t *testing.T) {
	acc := NewAccumulator(100)

	acc.RecordCost("server-a", -1, CostBreakdown{
		Input: 0.10, Output: 0.20, CacheRead: 0.05, CacheWrite: 0.01,
	})
	acc.RecordCost("server-a", -1, CostBreakdown{Input: 0.05, Output: 0.10})
	acc.RecordCost("server-b", -1, CostBreakdown{Input: 0.30, Output: 0.40})

	snap := acc.CostSnapshot()
	if !approxCostEq(snap.Session.InputUSD, 0.45) {
		t.Errorf("session input = %v, want 0.45", snap.Session.InputUSD)
	}
	if !approxCostEq(snap.Session.OutputUSD, 0.70) {
		t.Errorf("session output = %v, want 0.70", snap.Session.OutputUSD)
	}
	if !approxCostEq(snap.Session.CacheReadUSD, 0.05) {
		t.Errorf("session cache-read = %v, want 0.05", snap.Session.CacheReadUSD)
	}
	if !approxCostEq(snap.Session.CacheWriteUSD, 0.01) {
		t.Errorf("session cache-write = %v, want 0.01", snap.Session.CacheWriteUSD)
	}
	if !approxCostEq(snap.Session.TotalUSD, 1.21) {
		t.Errorf("session total = %v, want 1.21", snap.Session.TotalUSD)
	}

	a := snap.PerServer["server-a"]
	if !approxCostEq(a.TotalUSD, 0.51) {
		t.Errorf("server-a total = %v, want 0.51", a.TotalUSD)
	}
	b := snap.PerServer["server-b"]
	if !approxCostEq(b.TotalUSD, 0.70) {
		t.Errorf("server-b total = %v, want 0.70", b.TotalUSD)
	}
}

func TestAccumulator_RecordCost_PerReplica(t *testing.T) {
	acc := NewAccumulator(100)

	acc.RecordCost("multi", 0, CostBreakdown{Input: 0.10, Output: 0.20})
	acc.RecordCost("multi", 1, CostBreakdown{Input: 0.05, Output: 0.05})
	acc.RecordCost("multi", 1, CostBreakdown{Input: 0.05, Output: 0.05})

	snap := acc.CostSnapshot()
	replicas, ok := snap.PerReplica["multi"]
	if !ok {
		t.Fatalf("expected per-replica cost map; got %+v", snap.PerReplica)
	}
	if !approxCostEq(replicas[0].TotalUSD, 0.30) {
		t.Errorf("replica 0 total = %v, want 0.30", replicas[0].TotalUSD)
	}
	if !approxCostEq(replicas[1].TotalUSD, 0.20) {
		t.Errorf("replica 1 total = %v, want 0.20", replicas[1].TotalUSD)
	}
	server := snap.PerServer["multi"]
	if !approxCostEq(server.TotalUSD, replicas[0].TotalUSD+replicas[1].TotalUSD) {
		t.Errorf("server total %v != replica sum %v",
			server.TotalUSD, replicas[0].TotalUSD+replicas[1].TotalUSD)
	}
}

func TestAccumulator_RecordCost_RejectsInvalidValues(t *testing.T) {
	cases := []CostBreakdown{
		{Input: math.NaN()},
		{Output: math.Inf(1)},
		{CacheRead: -1.0},
		{CacheWrite: math.Inf(-1)},
	}
	for _, c := range cases {
		acc := NewAccumulator(100)
		acc.RecordCost("server", -1, c)
		snap := acc.CostSnapshot()
		if snap.Session.TotalUSD != 0 {
			t.Errorf("invalid breakdown %+v should be dropped; got total=%v", c, snap.Session.TotalUSD)
		}
	}
}

func TestAccumulator_RecordCost_ZeroIsNoop(t *testing.T) {
	acc := NewAccumulator(100)
	acc.RecordCost("server", -1, CostBreakdown{})
	snap := acc.CostSnapshot()
	if snap.Session.TotalUSD != 0 {
		t.Errorf("zero cost record should be a no-op; got total=%v", snap.Session.TotalUSD)
	}
	if _, ok := snap.PerServer["server"]; ok {
		t.Error("zero cost record should not create per-server entry")
	}
}

func TestAccumulator_QueryCost(t *testing.T) {
	acc := NewAccumulator(100)
	acc.RecordCost("server-a", -1, CostBreakdown{Input: 0.10, Output: 0.20})
	acc.RecordCost("server-b", -1, CostBreakdown{Input: 0.30, Output: 0.40})

	resp := acc.QueryCost(time.Hour)
	if resp.Range != "1h" {
		t.Errorf("range = %q, want 1h", resp.Range)
	}
	if resp.Interval != "1m" {
		t.Errorf("interval = %q, want 1m", resp.Interval)
	}
	if len(resp.Points) == 0 {
		t.Fatal("expected at least one cost data point")
	}
	var total float64
	for _, p := range resp.Points {
		total += p.USD
	}
	if !approxCostEq(total, 1.0) {
		t.Errorf("aggregate USD = %v, want 1.00", total)
	}
	if _, ok := resp.PerServer["server-a"]; !ok {
		t.Error("expected per-server time-series for server-a")
	}
}

func TestAccumulator_RecordCost_Concurrent(t *testing.T) {
	acc := NewAccumulator(1000)
	var wg sync.WaitGroup
	const goroutines = 50
	const calls = 50
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < calls; j++ {
				acc.RecordCost("server", j%4, CostBreakdown{Input: 0.001, Output: 0.001})
			}
		}()
	}
	wg.Wait()

	snap := acc.CostSnapshot()
	want := 0.002 * float64(goroutines*calls)
	if !approxCostEq(snap.Session.TotalUSD, want) {
		t.Errorf("session total under concurrent writes = %v, want %v",
			snap.Session.TotalUSD, want)
	}
}

func TestAccumulator_ClearCost_LeavesTokensUntouched(t *testing.T) {
	acc := NewAccumulator(100)
	acc.Record("server-a", 100, 50)
	acc.RecordCost("server-a", -1, CostBreakdown{Input: 0.10, Output: 0.20})

	acc.ClearCost()

	tokens := acc.Snapshot()
	if tokens.Session.TotalTokens != 150 {
		t.Errorf("ClearCost should not touch token counters; got %d", tokens.Session.TotalTokens)
	}

	cost := acc.CostSnapshot()
	if cost.Session.TotalUSD != 0 {
		t.Errorf("expected zero cost after ClearCost; got %v", cost.Session.TotalUSD)
	}
	if entry, ok := cost.PerServer["server-a"]; ok && entry.TotalUSD != 0 {
		t.Errorf("expected zero per-server cost after ClearCost; got %v", entry.TotalUSD)
	}
}

func TestAccumulator_Clear_ResetsCost(t *testing.T) {
	acc := NewAccumulator(100)
	acc.RecordCost("s", -1, CostBreakdown{Input: 1.0})
	acc.Clear()

	snap := acc.CostSnapshot()
	if snap.Session.TotalUSD != 0 {
		t.Errorf("Clear() should reset cost; got %v", snap.Session.TotalUSD)
	}
}

// TestAccumulator_TokenJSONShapeUnchanged covers Acceptance Criterion 3:
// the JSON representation of the token-side Snapshot has not changed.
// Existing /api/metrics/tokens consumers parse this shape; any drift here
// is a backward-incompatible regression.
func TestAccumulator_TokenJSONShapeUnchanged(t *testing.T) {
	usage := TokenUsage{
		Session:   TokenCounts{InputTokens: 1, OutputTokens: 1, TotalTokens: 2},
		PerServer: map[string]TokenCounts{"a": {InputTokens: 1, OutputTokens: 1, TotalTokens: 2}},
	}
	payload, err := json.Marshal(usage)
	if err != nil {
		t.Fatal(err)
	}
	body := string(payload)

	for _, key := range []string{`"session"`, `"per_server"`, `"format_savings"`} {
		if !strings.Contains(body, key) {
			t.Errorf("expected field %s in TokenUsage JSON; got %s", key, body)
		}
	}
	// Cost-related field names must not have leaked into TokenUsage.
	for _, forbidden := range []string{`"cost"`, `"session_usd"`, `"input_usd"`, `"total_usd"`} {
		if strings.Contains(body, forbidden) {
			t.Errorf("TokenUsage unexpectedly carries %s field; got %s", forbidden, body)
		}
	}
}

func approxCostEq(a, b float64) bool {
	const eps = 1e-6 // micro-USD precision
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < eps
}

// --- Per-client attribution tests (PR 2) ---

func TestAccumulator_RecordReplicaWithClient_TokenAttribution(t *testing.T) {
	acc := NewAccumulator(100)

	acc.RecordReplicaWithClient("server-a", -1, "claude-code", 100, 50)
	acc.RecordReplicaWithClient("server-a", -1, "cursor", 30, 10)
	acc.RecordReplicaWithClient("server-b", 0, "claude-code", 20, 5)

	snap := acc.Snapshot()

	if snap.Session.TotalTokens != 215 {
		t.Errorf("session total = %d, want 215", snap.Session.TotalTokens)
	}
	if got := snap.PerClient["claude-code"].TotalTokens; got != 175 {
		t.Errorf("claude-code total = %d, want 175 (100+50 + 20+5)", got)
	}
	if got := snap.PerClient["cursor"].TotalTokens; got != 40 {
		t.Errorf("cursor total = %d, want 40", got)
	}
	// per-server aggregates must still cover both clients combined.
	if snap.PerServer["server-a"].TotalTokens != 190 {
		t.Errorf("server-a total = %d, want 190", snap.PerServer["server-a"].TotalTokens)
	}
}

func TestAccumulator_RecordReplicaWithClient_EmptyClientSkipsClientMap(t *testing.T) {
	acc := NewAccumulator(100)
	acc.RecordReplicaWithClient("server-a", -1, "", 10, 5)

	snap := acc.Snapshot()
	if len(snap.PerClient) != 0 {
		t.Errorf("expected no per-client entries with empty clientID, got %v", snap.PerClient)
	}
	// Session totals must still reflect the call.
	if snap.Session.TotalTokens != 15 {
		t.Errorf("session total = %d, want 15", snap.Session.TotalTokens)
	}
}

func TestAccumulator_RecordCostWithClient_CostAttribution(t *testing.T) {
	acc := NewAccumulator(100)

	acc.RecordCostWithClient("server-a", -1, "claude-code", CostBreakdown{Input: 0.10, Output: 0.20})
	acc.RecordCostWithClient("server-a", -1, "cursor", CostBreakdown{Input: 0.05, Output: 0.05})

	snap := acc.CostSnapshot()
	if !approxCostEq(snap.Session.TotalUSD, 0.40) {
		t.Errorf("session total = %v, want 0.40", snap.Session.TotalUSD)
	}
	if !approxCostEq(snap.PerClient["claude-code"].TotalUSD, 0.30) {
		t.Errorf("claude-code = %v, want 0.30", snap.PerClient["claude-code"].TotalUSD)
	}
	if !approxCostEq(snap.PerClient["cursor"].TotalUSD, 0.10) {
		t.Errorf("cursor = %v, want 0.10", snap.PerClient["cursor"].TotalUSD)
	}
}

func TestAccumulator_QueryCostByClient_GroupsByClient(t *testing.T) {
	acc := NewAccumulator(100)

	acc.RecordCostWithClient("server-a", -1, "claude-code", CostBreakdown{Input: 0.50, Output: 0.50})
	acc.RecordCostWithClient("server-a", -1, "cursor", CostBreakdown{Input: 0.10})

	withClients := acc.QueryCostByClient(time.Hour)
	if withClients.PerClient == nil {
		t.Fatal("expected non-nil PerClient when querying with client grouping")
	}
	if len(withClients.PerClient) != 2 {
		t.Errorf("expected 2 per-client series, got %d", len(withClients.PerClient))
	}

	// QueryCost (no client grouping) must still leave PerClient nil so
	// existing consumers see the same JSON shape.
	withoutClients := acc.QueryCost(time.Hour)
	if withoutClients.PerClient != nil {
		t.Errorf("expected nil PerClient on QueryCost; got %v", withoutClients.PerClient)
	}
	if len(withoutClients.PerServer) == 0 {
		t.Error("QueryCost should still surface PerServer time-series")
	}
}

func TestAccumulator_TokenUsage_PerClient_OmitemptyWhenAbsent(t *testing.T) {
	usage := TokenUsage{
		Session:   TokenCounts{InputTokens: 1, OutputTokens: 1, TotalTokens: 2},
		PerServer: map[string]TokenCounts{"a": {InputTokens: 1, OutputTokens: 1, TotalTokens: 2}},
	}
	payload, err := json.Marshal(usage)
	if err != nil {
		t.Fatal(err)
	}
	body := string(payload)
	if strings.Contains(body, `"per_client"`) {
		t.Errorf("expected per_client field omitted when absent; got %s", body)
	}
}

func TestAccumulator_CostUsage_PerClient_OmitemptyWhenAbsent(t *testing.T) {
	usage := CostUsage{
		Session: CostCounts{InputUSD: 1, TotalUSD: 1},
	}
	payload, err := json.Marshal(usage)
	if err != nil {
		t.Fatal(err)
	}
	body := string(payload)
	if strings.Contains(body, `"per_client"`) {
		t.Errorf("expected per_client field omitted when absent; got %s", body)
	}
}

func TestAccumulator_ClearCost_AlsoClearsPerClient(t *testing.T) {
	acc := NewAccumulator(100)
	acc.RecordReplicaWithClient("s", -1, "client-a", 10, 5)
	acc.RecordCostWithClient("s", -1, "client-a", CostBreakdown{Input: 1.0})

	acc.ClearCost()

	snap := acc.CostSnapshot()
	if got := snap.PerClient["client-a"].TotalUSD; got != 0 {
		t.Errorf("ClearCost should reset per-client cost; got %v", got)
	}
	// Token-side per-client should remain intact (ClearCost does not touch tokens).
	tokens := acc.Snapshot()
	if tokens.PerClient["client-a"].TotalTokens != 15 {
		t.Errorf("ClearCost should not touch token counters; got %d", tokens.PerClient["client-a"].TotalTokens)
	}
}

func TestAccumulator_Clear_ResetsPerClient(t *testing.T) {
	acc := NewAccumulator(100)
	acc.RecordReplicaWithClient("s", -1, "client-a", 10, 5)
	acc.RecordCostWithClient("s", -1, "client-a", CostBreakdown{Input: 1.0})

	acc.Clear()

	tokens := acc.Snapshot()
	if len(tokens.PerClient) != 0 {
		t.Errorf("Clear should drop per-client tokens; got %v", tokens.PerClient)
	}
	cost := acc.CostSnapshot()
	if len(cost.PerClient) != 0 {
		t.Errorf("Clear should drop per-client cost; got %v", cost.PerClient)
	}
}
