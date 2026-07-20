package limits

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/mcp"
)

func githubCall(client string) mcp.GateCall {
	return mcp.GateCall{
		PrefixedTool:   "github__search_code",
		ServerName:     "github",
		ClientAccessID: client,
	}
}

func newTestPolicy(t *testing.T, cfg *config.LimitsConfig, ledgerPath string) *Policy {
	t.Helper()
	p := NewPolicy(cfg, ledgerPath, nil)
	if p == nil {
		t.Fatal("expected non-nil policy")
	}
	return p
}

func TestNewPolicy_NilAndEmpty(t *testing.T) {
	if p := NewPolicy(nil, "", nil); p != nil {
		t.Error("nil config should compile to nil policy")
	}
	if p := NewPolicy(&config.LimitsConfig{}, "", nil); p != nil {
		t.Error("empty config should compile to nil policy")
	}
	// Nil policy methods are all safe and permissive.
	var p *Policy
	if gates := p.Gates(); gates != nil {
		t.Error("nil policy should return nil gates")
	}
	p.SettleToolCallCost(context.Background(), githubCall("cursor"), 1.0)
	p.Start(context.Background())
	p.Stop()
	p.Flush(context.Background())
	if st := p.Status(); st.Configured || st.Entries == nil || len(st.Entries) != 0 {
		t.Errorf("nil policy status = %+v", st)
	}
}

func TestGates_OrderAndKinds(t *testing.T) {
	p := newTestPolicy(t, &config.LimitsConfig{
		Budgets:    []config.BudgetLimit{{Server: "github", MaxUSD: 5, Period: "daily"}},
		RateLimits: []config.RateLimit{{Server: "github", CallsPerMinute: 30}},
	}, "")
	gates := p.Gates()
	if len(gates) != 2 {
		t.Fatalf("expected 2 gates, got %d", len(gates))
	}
	if gates[0].Name() != "rate-limits" || gates[1].Name() != "budgets" {
		t.Errorf("canonical order violated: %s, %s", gates[0].Name(), gates[1].Name())
	}
}

func TestRateGate_BurstThenDeny(t *testing.T) {
	p := newTestPolicy(t, &config.LimitsConfig{
		RateLimits: []config.RateLimit{{Server: "github", CallsPerMinute: 6, Burst: 3}},
	}, "")
	gate := p.Gates()[0]
	ctx := context.Background()

	for i := range 3 {
		if d := gate.CheckToolCall(ctx, githubCall("cursor")); !d.Allow {
			t.Fatalf("burst call %d denied: %s", i, d.Message)
		}
	}
	d := gate.CheckToolCall(ctx, githubCall("cursor"))
	if d.Allow {
		t.Fatal("call past burst should be denied")
	}
	for _, want := range []string{`server "github"`, "6 calls/min", "Retry after"} {
		if !strings.Contains(d.Message, want) {
			t.Errorf("denial message missing %q: %s", want, d.Message)
		}
	}

	// A call that matches no entry is unaffected.
	other := mcp.GateCall{PrefixedTool: "gitlab__list", ServerName: "gitlab", ClientAccessID: "cursor"}
	if d := gate.CheckToolCall(ctx, other); !d.Allow {
		t.Errorf("unmatched call denied: %s", d.Message)
	}
}

func TestRateGate_ClientScopeNormalizes(t *testing.T) {
	p := newTestPolicy(t, &config.LimitsConfig{
		RateLimits: []config.RateLimit{{Client: "claude-code", CallsPerMinute: 6, Burst: 1}},
	}, "")
	gate := p.Gates()[0]
	ctx := context.Background()

	// "Claude Code" normalizes to "claude-code" and must share the bucket.
	if d := gate.CheckToolCall(ctx, githubCall("Claude Code")); !d.Allow {
		t.Fatalf("first call denied: %s", d.Message)
	}
	if d := gate.CheckToolCall(ctx, githubCall("claude-code")); d.Allow {
		t.Fatal("alias variant should hit the same bucket and be denied")
	}
	// A different client is unaffected.
	if d := gate.CheckToolCall(ctx, githubCall("cursor")); !d.Allow {
		t.Errorf("other client denied: %s", d.Message)
	}
}

func TestBudget_CheckSettleDeny(t *testing.T) {
	p := newTestPolicy(t, &config.LimitsConfig{
		Budgets: []config.BudgetLimit{{Client: "claude-code", MaxUSD: 0.10, Period: "daily"}},
	}, "")
	gate := p.Gates()[0]
	ctx := context.Background()
	call := githubCall("claude-code")

	if d := gate.CheckToolCall(ctx, call); !d.Allow {
		t.Fatalf("under-budget call denied: %s", d.Message)
	}
	p.SettleToolCallCost(ctx, call, 0.06)
	if d := gate.CheckToolCall(ctx, call); !d.Allow {
		t.Fatalf("still under budget, denied: %s", d.Message)
	}
	p.SettleToolCallCost(ctx, call, 0.06) // 0.12 total: overshoot then block
	d := gate.CheckToolCall(ctx, call)
	if d.Allow {
		t.Fatal("over-budget call should be denied")
	}
	for _, want := range []string{`client "claude-code"`, "$0.12 of $0.10", "daily", "Resets", "Do not retry"} {
		if !strings.Contains(d.Message, want) {
			t.Errorf("denial message missing %q: %s", want, d.Message)
		}
	}

	// Another client is not charged against this budget.
	if d := gate.CheckToolCall(ctx, githubCall("cursor")); !d.Allow {
		t.Errorf("non-matching client denied: %s", d.Message)
	}
}

func TestBudget_WindowRollResets(t *testing.T) {
	p := newTestPolicy(t, &config.LimitsConfig{
		Budgets: []config.BudgetLimit{{Server: "github", MaxUSD: 0.01, Period: "daily"}},
	}, "")
	now := time.Date(2026, 7, 20, 22, 0, 0, 0, time.Local)
	p.now = func() time.Time { return now }
	// Re-anchor the compiled entry to the fake clock's window.
	p.budgets[0].windowStart = windowStart(PeriodDaily, now)

	ctx := context.Background()
	call := githubCall("cursor")
	gate := p.Gates()[0]

	p.SettleToolCallCost(ctx, call, 0.02)
	if d := gate.CheckToolCall(ctx, call); d.Allow {
		t.Fatal("should be over budget before midnight")
	}
	now = now.Add(3 * time.Hour) // past local midnight
	if d := gate.CheckToolCall(ctx, call); !d.Allow {
		t.Fatalf("new window should reset spend: %s", d.Message)
	}
	if st := p.Status().Entries[0]; st.Budget.SpentUSD != 0 {
		t.Errorf("spend after roll = %v, want 0", st.Budget.SpentUSD)
	}
}

func TestBudget_UnpricedCallSettlesNothing(t *testing.T) {
	p := newTestPolicy(t, &config.LimitsConfig{
		Budgets: []config.BudgetLimit{{Server: "github", MaxUSD: 1, Period: "daily"}},
	}, "")
	p.SettleToolCallCost(context.Background(), githubCall("cursor"), 0)
	if st := p.Status().Entries[0]; st.Budget.SpentUSD != 0 {
		t.Errorf("zero-cost settle changed spend: %v", st.Budget.SpentUSD)
	}
}

func TestStatus_StatesAndPercent(t *testing.T) {
	p := newTestPolicy(t, &config.LimitsConfig{
		Budgets: []config.BudgetLimit{
			{Client: "claude-code", MaxUSD: 1.00, Period: "daily", WarnAtPercent: 50},
		},
		RateLimits: []config.RateLimit{{Server: "github", CallsPerMinute: 60, Burst: 1}},
	}, "")
	ctx := context.Background()

	st := p.Status()
	if !st.Configured || len(st.Entries) != 2 {
		t.Fatalf("status = %+v", st)
	}
	if st.Entries[0].State != "ok" || st.Entries[1].State != "ok" {
		t.Errorf("initial states: %s, %s", st.Entries[0].State, st.Entries[1].State)
	}
	if st.Entries[0].Budget == nil || st.Entries[0].Budget.WindowStart.IsZero() || st.Entries[0].Budget.WindowEnd.IsZero() {
		t.Error("budget entry missing window bounds")
	}

	p.SettleToolCallCost(ctx, githubCall("claude-code"), 0.60)
	if got := p.Status().Entries[0]; got.State != "warn" || got.Budget.Percent != 60 {
		t.Errorf("after warn threshold: state=%s percent=%v", got.State, got.Budget.Percent)
	}
	p.SettleToolCallCost(ctx, githubCall("claude-code"), 0.50)
	if got := p.Status().Entries[0]; got.State != "exceeded" {
		t.Errorf("after cap: state=%s", got.State)
	}

	// Drain the rate bucket; status flips to exceeded.
	p.Gates()[0].CheckToolCall(ctx, githubCall("cursor"))
	if got := p.Status().Entries[1]; got.State != "exceeded" {
		t.Errorf("drained bucket state = %s", got.State)
	}
}

func TestLedger_PersistAndReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test-stack.json")
	cfg := &config.LimitsConfig{
		Budgets: []config.BudgetLimit{{Client: "claude-code", MaxUSD: 5, Period: "daily"}},
	}
	ctx := context.Background()

	p1 := newTestPolicy(t, cfg, path)
	p1.SettleToolCallCost(ctx, githubCall("claude-code"), 1.25)
	p1.Flush(ctx)

	// Same window: spend resumes.
	p2 := newTestPolicy(t, cfg, path)
	if got := p2.Status().Entries[0].Budget.SpentUSD; got != 1.25 {
		t.Errorf("reloaded spend = %v, want 1.25", got)
	}

	// Stale window on disk: starts at zero.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var lf ledgerFile
	if err := json.Unmarshal(raw, &lf); err != nil {
		t.Fatal(err)
	}
	for k, row := range lf.Entries {
		row.WindowStart = row.WindowStart.AddDate(0, 0, -1)
		lf.Entries[k] = row
	}
	stale, _ := json.Marshal(lf)
	if err := os.WriteFile(path, stale, 0o600); err != nil {
		t.Fatal(err)
	}
	p3 := newTestPolicy(t, cfg, path)
	if got := p3.Status().Entries[0].Budget.SpentUSD; got != 0 {
		t.Errorf("stale-window spend = %v, want 0", got)
	}
}

func TestLedger_CorruptStartsFresh(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.LimitsConfig{
		Budgets: []config.BudgetLimit{{Server: "github", MaxUSD: 5, Period: "daily"}},
	}
	p := newTestPolicy(t, cfg, path) // must not panic or fail
	if got := p.Status().Entries[0].Budget.SpentUSD; got != 0 {
		t.Errorf("spend after corrupt ledger = %v, want 0", got)
	}
	// A flush repairs the file.
	p.SettleToolCallCost(context.Background(), githubCall("cursor"), 0.5)
	p.Flush(context.Background())
	p2 := newTestPolicy(t, cfg, path)
	if got := p2.Status().Entries[0].Budget.SpentUSD; got != 0.5 {
		t.Errorf("spend after repair = %v, want 0.5", got)
	}
}

func TestFlusher_DebounceAndStop(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flush.json")
	cfg := &config.LimitsConfig{
		Budgets: []config.BudgetLimit{{Server: "github", MaxUSD: 5, Period: "daily"}},
	}
	p := newTestPolicy(t, cfg, path)
	p.Start(context.Background())
	p.SettleToolCallCost(context.Background(), githubCall("cursor"), 0.25)
	p.Stop() // final flush must land even before the debounce fires

	p2 := newTestPolicy(t, cfg, path)
	if got := p2.Status().Entries[0].Budget.SpentUSD; got != 0.25 {
		t.Errorf("spend after stop-flush = %v, want 0.25", got)
	}
	p.Stop() // idempotent
}

func TestCarryOver(t *testing.T) {
	ctx := context.Background()
	oldCfg := &config.LimitsConfig{
		Budgets: []config.BudgetLimit{
			{Client: "claude-code", MaxUSD: 1, Period: "daily"},
			{Server: "github", MaxUSD: 2, Period: "weekly"},
		},
	}
	oldP := newTestPolicy(t, oldCfg, "")
	oldP.SettleToolCallCost(ctx, githubCall("claude-code"), 0.75)
	oldP.SettleToolCallCost(ctx, githubCall("cursor"), 0.30) // server budget only

	newCfg := &config.LimitsConfig{
		Budgets: []config.BudgetLimit{
			// Same scope+key+period, raised cap: spend carries.
			{Client: "claude-code", MaxUSD: 10, Period: "daily"},
			// Period changed: fresh counter.
			{Server: "github", MaxUSD: 2, Period: "monthly"},
		},
	}
	newP := newTestPolicy(t, newCfg, "")
	newP.CarryOver(oldP)

	entries := newP.Status().Entries
	if got := entries[0].Budget.SpentUSD; got != 0.75 {
		t.Errorf("carried spend = %v, want 0.75 (cap raise must not refill)", got)
	}
	if got := entries[1].Budget.SpentUSD; got != 0 {
		t.Errorf("period-changed spend = %v, want 0", got)
	}
}

func TestBudget_WarnLogsOncePerWindow(t *testing.T) {
	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	p := NewPolicy(&config.LimitsConfig{
		Budgets: []config.BudgetLimit{
			{Client: "claude-code", MaxUSD: 1.00, Period: "daily", WarnAtPercent: 50},
		},
	}, "", logger)
	if p == nil {
		t.Fatal("expected policy")
	}
	ctx := context.Background()
	call := githubCall("claude-code")

	p.SettleToolCallCost(ctx, call, 0.55)
	p.SettleToolCallCost(ctx, call, 0.10) // still past threshold, must not re-warn
	if got := strings.Count(buf.String(), "budget warn threshold crossed"); got != 1 {
		t.Errorf("warn log count = %d, want 1\nlogs: %s", got, buf.String())
	}
	p.SettleToolCallCost(ctx, call, 0.50) // crosses the cap
	p.SettleToolCallCost(ctx, call, 0.10) // already over, must not re-log
	if got := strings.Count(buf.String(), "budget exceeded"); got != 1 {
		t.Errorf("exceeded log count = %d, want 1\nlogs: %s", got, buf.String())
	}
}

func TestCarryOver_RateBucketsSurviveReload(t *testing.T) {
	mk := func() *Policy {
		return newTestPolicy(t, &config.LimitsConfig{
			RateLimits: []config.RateLimit{{Server: "github", CallsPerMinute: 6, Burst: 2}},
		}, "")
	}
	ctx := context.Background()
	oldP := mk()
	gate := oldP.Gates()[0]
	gate.CheckToolCall(ctx, githubCall("cursor"))
	gate.CheckToolCall(ctx, githubCall("cursor")) // bucket drained

	// Unrelated reload rebuilds the policy with an identical rate entry:
	// the drained bucket must carry, not refill.
	newP := mk()
	newP.CarryOver(oldP)
	if d := newP.Gates()[0].CheckToolCall(ctx, githubCall("cursor")); d.Allow {
		t.Fatal("reload refilled a drained rate bucket")
	}

	// A changed rate gets a fresh bucket by design.
	changed := newTestPolicy(t, &config.LimitsConfig{
		RateLimits: []config.RateLimit{{Server: "github", CallsPerMinute: 12, Burst: 2}},
	}, "")
	changed.CarryOver(newP)
	if d := changed.Gates()[0].CheckToolCall(ctx, githubCall("cursor")); !d.Allow {
		t.Fatalf("changed rate should start fresh: %s", d.Message)
	}
}

func TestCarryOver_RetiredPolicyForwardsSettles(t *testing.T) {
	cfg := &config.LimitsConfig{
		Budgets: []config.BudgetLimit{{Server: "github", MaxUSD: 5, Period: "daily"}},
	}
	ctx := context.Background()
	oldP := newTestPolicy(t, cfg, "")
	oldP.SettleToolCallCost(ctx, githubCall("cursor"), 1.00)

	newP := newTestPolicy(t, cfg, "")
	newP.CarryOver(oldP)

	// An in-flight call that captured the old settler pointer lands after
	// the swap: the cost must reach the new policy, not vanish.
	oldP.SettleToolCallCost(ctx, githubCall("cursor"), 0.50)
	if got := newP.Status().Entries[0].Budget.SpentUSD; got != 1.50 {
		t.Errorf("spend after forwarded settle = %v, want 1.50", got)
	}
}

func TestBudget_BackwardClockKeepsSpend(t *testing.T) {
	p := newTestPolicy(t, &config.LimitsConfig{
		Budgets: []config.BudgetLimit{{Server: "github", MaxUSD: 1, Period: "daily"}},
	}, "")
	now := time.Date(2026, 7, 20, 0, 30, 0, 0, time.Local)
	p.now = func() time.Time { return now }
	p.budgets[0].windowStart = windowStart(PeriodDaily, now)

	ctx := context.Background()
	p.SettleToolCallCost(ctx, githubCall("cursor"), 0.40)

	// Clock steps back across midnight (NTP correction): spend must hold.
	now = now.Add(-time.Hour)
	if got := p.Status().Entries[0].Budget.SpentUSD; got != 0.40 {
		t.Errorf("backward clock reset spend: %v, want 0.40", got)
	}
	// Clock recovers past midnight again: still the same window, same spend.
	now = now.Add(2 * time.Hour)
	if got := p.Status().Entries[0].Budget.SpentUSD; got != 0.40 {
		t.Errorf("clock recovery reset spend: %v, want 0.40", got)
	}
}

func TestLedger_RemovedEntryRowSurvivesForReAdd(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "orphan.json")
	ctx := context.Background()
	two := &config.LimitsConfig{
		Budgets: []config.BudgetLimit{
			{Client: "claude-code", MaxUSD: 5, Period: "daily"},
			{Server: "github", MaxUSD: 5, Period: "daily"},
		},
	}
	p1 := newTestPolicy(t, two, path)
	p1.SettleToolCallCost(ctx, githubCall("claude-code"), 1.00) // hits both entries
	p1.Flush(ctx)

	// Remove the client budget; the surviving policy's flush must preserve
	// the removed entry's row as an orphan.
	one := &config.LimitsConfig{
		Budgets: []config.BudgetLimit{{Server: "github", MaxUSD: 5, Period: "daily"}},
	}
	p2 := newTestPolicy(t, one, path)
	p2.SettleToolCallCost(ctx, githubCall("cursor"), 0.10)
	p2.Flush(ctx)

	// Re-add the client budget in the same window: prior spend resumes.
	p3 := newTestPolicy(t, two, path)
	if got := p3.Status().Entries[0].Budget.SpentUSD; got != 1.00 {
		t.Errorf("re-added budget spend = %v, want 1.00 (orphan row lost)", got)
	}
}

func TestNewPolicy_DuplicateClientAfterNormalization(t *testing.T) {
	p := newTestPolicy(t, &config.LimitsConfig{
		Budgets: []config.BudgetLimit{
			{Client: "Claude Code", MaxUSD: 5, Period: "daily"},
			{Client: "claude-code", MaxUSD: 9, Period: "daily"},
		},
	}, "")
	if len(p.budgets) != 1 {
		t.Fatalf("compiled %d budget entries, want 1 (duplicates fold)", len(p.budgets))
	}
	// A single settle must charge once, not twice.
	p.SettleToolCallCost(context.Background(), githubCall("claude-code"), 1.00)
	if got := p.Status().Entries[0].Budget.SpentUSD; got != 1.00 {
		t.Errorf("spend = %v, want 1.00 (double-settle)", got)
	}
}

func TestStop_WithoutStartDoesNotHang(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nostart.json")
	cfg := &config.LimitsConfig{
		Budgets: []config.BudgetLimit{{Server: "github", MaxUSD: 5, Period: "daily"}},
	}
	p := newTestPolicy(t, cfg, path)
	p.SettleToolCallCost(context.Background(), githubCall("cursor"), 0.30)

	done := make(chan struct{})
	go func() {
		p.Stop() // never Started; must flush inline and return
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop without Start hung")
	}
	p2 := newTestPolicy(t, cfg, path)
	if got := p2.Status().Entries[0].Budget.SpentUSD; got != 0.30 {
		t.Errorf("inline stop-flush lost spend: %v, want 0.30", got)
	}
}

func TestDefaultBurst(t *testing.T) {
	tests := []struct{ rate, want int }{
		{1, 5}, {6, 5}, {30, 5}, {60, 10}, {600, 100},
	}
	for _, tc := range tests {
		if got := DefaultBurst(tc.rate); got != tc.want {
			t.Errorf("DefaultBurst(%d) = %d, want %d", tc.rate, got, tc.want)
		}
	}
}
