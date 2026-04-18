package metrics

import (
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
