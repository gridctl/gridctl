package mcp

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/mock/gomock"
)

// fakeSpawner records Spawn / Reap calls and returns pre-seeded clients. Tests
// construct one per autoscaler and inspect CountSpawn / CountReap afterwards.
type fakeSpawner struct {
	mu           sync.Mutex
	spawned      int32
	reaped       int32
	spawnErr     error
	errOnce      bool // if true, spawnErr is cleared after first failure
	spawnErrFor  int  // number of spawn attempts that fail before succeeding (0 = none)
	newClient    func(id int) AgentClient
	nextClientID atomic.Int32
}

func newFakeSpawner(t *testing.T) *fakeSpawner {
	t.Helper()
	ctrl := gomock.NewController(t)
	return &fakeSpawner{
		newClient: func(id int) AgentClient {
			name := "replica-" + itoa(id)
			return setupMockAgentClient(ctrl, name, []Tool{{Name: "echo"}})
		},
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := "0123456789"
	neg := n < 0
	if neg {
		n = -n
	}
	var out []byte
	for n > 0 {
		out = append([]byte{digits[n%10]}, out...)
		n /= 10
	}
	if neg {
		out = append([]byte{'-'}, out...)
	}
	return string(out)
}

func (f *fakeSpawner) Spawn(_ context.Context) (AgentClient, error) {
	atomic.AddInt32(&f.spawned, 1)

	f.mu.Lock()
	err := f.spawnErr
	switch {
	case f.spawnErrFor > 0:
		// Scheduled failure budget — fail and decrement. Clear the error
		// once the budget is exhausted so later ticks succeed.
		f.spawnErrFor--
		if f.spawnErrFor == 0 {
			f.spawnErr = nil
		}
		f.mu.Unlock()
		return nil, err
	case f.errOnce && err != nil:
		f.spawnErr = nil
		f.mu.Unlock()
		return nil, err
	case err != nil:
		f.mu.Unlock()
		return nil, err
	}
	f.mu.Unlock()

	id := int(f.nextClientID.Add(1) - 1)
	return f.newClient(id), nil
}

func (f *fakeSpawner) Reap(_ context.Context, _ *Replica) error {
	atomic.AddInt32(&f.reaped, 1)
	return nil
}

func (f *fakeSpawner) CountSpawn() int { return int(atomic.LoadInt32(&f.spawned)) }
func (f *fakeSpawner) CountReap() int  { return int(atomic.LoadInt32(&f.reaped)) }

// setInitialReplicas seeds a fresh set with N healthy replicas via AddReplica,
// matching how a warm scaler would have done it. Returns the replica ids.
func setInitialReplicas(t *testing.T, set *ReplicaSet, sp *fakeSpawner, n int) []int {
	t.Helper()
	ids := make([]int, 0, n)
	for i := 0; i < n; i++ {
		client, err := sp.Spawn(context.Background())
		if err != nil {
			t.Fatalf("seed spawn: %v", err)
		}
		ids = append(ids, set.AddReplica(client))
	}
	return ids
}

// newTestAutoscaler returns an Autoscaler wired to an empty ReplicaSet + the
// given policy. Initial replicas are pre-seeded via setInitialReplicas.
func newTestAutoscaler(t *testing.T, policy AutoscalePolicy, initial int) (*Autoscaler, *ReplicaSet, *fakeSpawner) {
	t.Helper()
	set := NewReplicaSet("junos", ReplicaPolicyRoundRobin, nil)
	sp := newFakeSpawner(t)
	setInitialReplicas(t, set, sp, initial)
	logger := slog.New(slog.NewTextHandler(discardWriter{}, &slog.HandlerOptions{Level: slog.LevelDebug}))
	a := NewAutoscaler("junos", set, sp, policy, logger)
	return a, set, sp
}

// discardWriter silences slog output in tests.
type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestAutoscaler_Noop_MedianBelowTarget(t *testing.T) {
	policy := AutoscalePolicy{Min: 2, Max: 6, TargetInFlight: 3, ScaleUpAfter: 30 * time.Second, ScaleDownAfter: 5 * time.Minute}
	a, set, sp := newTestAutoscaler(t, policy, 2)

	// Median 1 with target 3 → ceil(2*1/3) = 1 + warm 0 = 1, clamped to Min 2. target == current: noop.
	set.Replicas()[0].inFlight.Store(1)
	set.Replicas()[1].inFlight.Store(1)
	d, err := a.Tick(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if d != DecisionNoop {
		t.Errorf("decision = %v, want Noop", d)
	}
	if sp.CountSpawn() != 2 {
		t.Errorf("spawn count = %d, want 2 (seed only)", sp.CountSpawn())
	}
}

func TestAutoscaler_ScaleUp_AfterSustainedHighLoad(t *testing.T) {
	policy := AutoscalePolicy{Min: 1, Max: 4, TargetInFlight: 3, ScaleUpAfter: 30 * time.Second, ScaleDownAfter: 5 * time.Minute}
	a, set, sp := newTestAutoscaler(t, policy, 1)

	// Median 9 vs target 3 → ceil(1*9/3) = 3. Warm+0 = 3. Clamped Min 1, Max 4 = 3.
	set.Replicas()[0].inFlight.Store(9)

	// Feed a window's worth of high-load samples so scale-up passes the
	// sustained-signal gate.
	now := time.Now()
	for offset := 60 * time.Second; offset >= 0; offset -= 5 * time.Second {
		a.window.Add(now.Add(-offset), 9)
	}

	d, err := a.Tick(context.Background(), now)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if d != DecisionScaleUp {
		t.Fatalf("decision = %v, want ScaleUp", d)
	}
	if got := set.Size(); got != 3 {
		t.Errorf("replica count = %d, want 3", got)
	}
	if sp.CountSpawn() != 3 { // 1 seed + 2 scale-up (target 3 from current 1 = spawn 2)
		t.Errorf("spawn count = %d, want 3", sp.CountSpawn())
	}
}

func TestAutoscaler_ScaleUp_ClampedAtMax(t *testing.T) {
	policy := AutoscalePolicy{Min: 1, Max: 3, TargetInFlight: 2, ScaleUpAfter: 30 * time.Second, ScaleDownAfter: 5 * time.Minute}
	a, set, sp := newTestAutoscaler(t, policy, 1)

	set.Replicas()[0].inFlight.Store(50) // absurd pressure
	now := time.Now()
	for offset := 60 * time.Second; offset >= 0; offset -= 5 * time.Second {
		a.window.Add(now.Add(-offset), 50)
	}

	if _, err := a.Tick(context.Background(), now); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if got := set.Size(); got != policy.Max {
		t.Errorf("replica count = %d, want Max %d", got, policy.Max)
	}
	_ = sp
}

func TestAutoscaler_ScaleUp_CooldownBlocksSecondTick(t *testing.T) {
	policy := AutoscalePolicy{Min: 1, Max: 6, TargetInFlight: 3, ScaleUpAfter: 30 * time.Second, ScaleDownAfter: 5 * time.Minute}
	a, set, _ := newTestAutoscaler(t, policy, 1)

	set.Replicas()[0].inFlight.Store(9)
	now := time.Now()
	for offset := 60 * time.Second; offset >= 0; offset -= 5 * time.Second {
		a.window.Add(now.Add(-offset), 9)
	}

	// First tick scales up.
	if _, err := a.Tick(context.Background(), now); err != nil {
		t.Fatalf("tick 1: %v", err)
	}
	size1 := set.Size()

	// Second tick 5s later is within ScaleUpAfter cooldown → must be a noop.
	d, err := a.Tick(context.Background(), now.Add(5*time.Second))
	if err != nil {
		t.Fatalf("tick 2: %v", err)
	}
	if d != DecisionNoop {
		t.Errorf("tick 2 decision = %v, want Noop", d)
	}
	if set.Size() != size1 {
		t.Errorf("tick 2 added replicas despite cooldown (size %d → %d)", size1, set.Size())
	}
}

func TestAutoscaler_WarmPool_SkipsCooldown(t *testing.T) {
	policy := AutoscalePolicy{Min: 1, Max: 6, WarmPool: 2, TargetInFlight: 3, ScaleUpAfter: 30 * time.Second, ScaleDownAfter: 5 * time.Minute}
	a, _, _ := newTestAutoscaler(t, policy, 1)

	// Record a recent lastScaleUpAt so the cooldown would normally block.
	a.mu.Lock()
	a.lastScaleUpAt = time.Now().Add(-1 * time.Second)
	a.mu.Unlock()

	// current (1) < Min+WarmPool (3) triggers eager scale-up ignoring cooldown.
	d, err := a.Tick(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if d != DecisionScaleUp {
		t.Errorf("decision = %v, want ScaleUp (warm-pool catch-up)", d)
	}
}

func TestAutoscaler_ScaleDown_AfterSustainedIdle(t *testing.T) {
	policy := AutoscalePolicy{Min: 1, Max: 4, TargetInFlight: 3, ScaleUpAfter: 30 * time.Second, ScaleDownAfter: 60 * time.Second}
	a, set, _ := newTestAutoscaler(t, policy, 3)

	// No load: all replicas idle, median 0.
	now := time.Now()
	for offset := 120 * time.Second; offset >= 0; offset -= 5 * time.Second {
		a.window.Add(now.Add(-offset), 0)
	}

	d, err := a.Tick(context.Background(), now)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if d != DecisionScaleDown {
		t.Fatalf("decision = %v, want ScaleDown", d)
	}
	if got := set.Size(); got != 2 {
		t.Errorf("replica count = %d, want 2 (one reaped per tick)", got)
	}
}

func TestAutoscaler_ScaleDown_FlooredByMin(t *testing.T) {
	policy := AutoscalePolicy{Min: 2, Max: 4, TargetInFlight: 3, ScaleUpAfter: 30 * time.Second, ScaleDownAfter: 60 * time.Second}
	a, set, _ := newTestAutoscaler(t, policy, 2) // exactly at Min

	now := time.Now()
	for offset := 120 * time.Second; offset >= 0; offset -= 5 * time.Second {
		a.window.Add(now.Add(-offset), 0)
	}

	d, _ := a.Tick(context.Background(), now)
	if d != DecisionNoop {
		t.Errorf("decision = %v, want Noop (at Min floor)", d)
	}
	if got := set.Size(); got != 2 {
		t.Errorf("replica count = %d, want 2 (Min floor)", got)
	}
}

func TestAutoscaler_ScaleDown_FlooredByWarmPool(t *testing.T) {
	policy := AutoscalePolicy{Min: 1, Max: 4, WarmPool: 1, TargetInFlight: 3, ScaleUpAfter: 30 * time.Second, ScaleDownAfter: 60 * time.Second}
	a, _, _ := newTestAutoscaler(t, policy, 2) // Min 1 + WarmPool 1 floor

	now := time.Now()
	for offset := 120 * time.Second; offset >= 0; offset -= 5 * time.Second {
		a.window.Add(now.Add(-offset), 0)
	}

	d, _ := a.Tick(context.Background(), now)
	if d != DecisionNoop {
		t.Errorf("decision = %v, want Noop (warm-pool floor)", d)
	}
}

func TestAutoscaler_ScaleDown_CooldownBlocksSecondTick(t *testing.T) {
	policy := AutoscalePolicy{Min: 1, Max: 4, TargetInFlight: 3, ScaleUpAfter: 30 * time.Second, ScaleDownAfter: 60 * time.Second}
	a, set, _ := newTestAutoscaler(t, policy, 3)

	now := time.Now()
	for offset := 120 * time.Second; offset >= 0; offset -= 5 * time.Second {
		a.window.Add(now.Add(-offset), 0)
	}
	// First reap.
	if _, err := a.Tick(context.Background(), now); err != nil {
		t.Fatalf("tick 1: %v", err)
	}
	if set.Size() != 2 {
		t.Fatalf("after tick 1: size = %d, want 2", set.Size())
	}

	// Immediate second tick is inside the cooldown.
	d, _ := a.Tick(context.Background(), now.Add(5*time.Second))
	if d != DecisionNoop {
		t.Errorf("tick 2 decision = %v, want Noop", d)
	}
	if set.Size() != 2 {
		t.Errorf("tick 2 reaped despite cooldown: size = %d, want 2", set.Size())
	}
}

func TestAutoscaler_ScaleDown_OnePerTick(t *testing.T) {
	policy := AutoscalePolicy{Min: 1, Max: 10, TargetInFlight: 3, ScaleUpAfter: 30 * time.Second, ScaleDownAfter: 60 * time.Second}
	a, set, _ := newTestAutoscaler(t, policy, 6)

	now := time.Now()
	for offset := 120 * time.Second; offset >= 0; offset -= 5 * time.Second {
		a.window.Add(now.Add(-offset), 0)
	}

	// Even if the load-derived target is 1 (big drop), a single tick reaps at most one.
	if _, err := a.Tick(context.Background(), now); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if got := set.Size(); got != 5 {
		t.Errorf("size after one tick = %d, want 5 (one-per-tick)", got)
	}
}

func TestAutoscaler_WarmPool_KeepsAboveLoadTarget(t *testing.T) {
	policy := AutoscalePolicy{Min: 1, Max: 6, WarmPool: 2, TargetInFlight: 3, ScaleUpAfter: 30 * time.Second, ScaleDownAfter: 60 * time.Second}
	a, set, _ := newTestAutoscaler(t, policy, 1)

	// Tick immediately: warm-pool catch-up should scale eagerly to 1+2=3.
	d, err := a.Tick(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if d != DecisionScaleUp {
		t.Errorf("decision = %v, want ScaleUp", d)
	}
	if got := set.Size(); got != 3 {
		t.Errorf("size = %d, want 3 (Min 1 + WarmPool 2)", got)
	}
}

func TestAutoscaler_IdleToZero_ReapsToZero(t *testing.T) {
	policy := AutoscalePolicy{Min: 0, Max: 2, TargetInFlight: 3, IdleToZero: true, ScaleUpAfter: 30 * time.Second, ScaleDownAfter: 60 * time.Second}
	a, set, sp := newTestAutoscaler(t, policy, 1)

	// Sustained idle across the full ScaleDownAfter window.
	now := time.Now()
	for offset := 120 * time.Second; offset >= 0; offset -= 5 * time.Second {
		a.window.Add(now.Add(-offset), 0)
	}

	// First tick reaps one (the only remaining replica). The idle_to_zero path
	// drains aggressively past the normal warm-pool floor.
	d, err := a.Tick(context.Background(), now)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if d != DecisionScaleDown {
		t.Fatalf("decision = %v, want ScaleDown", d)
	}
	if set.Size() != 0 {
		t.Errorf("size = %d, want 0 after idle-to-zero reap", set.Size())
	}
	if sp.CountReap() != 1 {
		t.Errorf("reap count = %d, want 1", sp.CountReap())
	}
}

func TestAutoscaler_SpawnFailure_NoCooldownUpdate(t *testing.T) {
	policy := AutoscalePolicy{Min: 1, Max: 4, TargetInFlight: 3, ScaleUpAfter: 30 * time.Second, ScaleDownAfter: 60 * time.Second}
	a, set, sp := newTestAutoscaler(t, policy, 1)

	// One failing spawn, then success. scaleUp needs 2 spawns to reach target
	// 3 from current 1 — the first fails (loop breaks), the tick records no
	// cooldown. Tick 2 retries both spawns successfully.
	sp.mu.Lock()
	sp.spawnErr = errors.New("kaboom")
	sp.spawnErrFor = 1
	sp.mu.Unlock()

	set.Replicas()[0].inFlight.Store(9)
	now := time.Now()
	for offset := 60 * time.Second; offset >= 0; offset -= 5 * time.Second {
		a.window.Add(now.Add(-offset), 9)
	}

	// Tick 1: every spawn fails -> warm scale-up fails, lastScaleUpAt stays zero.
	if _, err := a.Tick(context.Background(), now); err == nil {
		t.Fatal("expected spawn error on tick 1")
	}
	a.mu.Lock()
	lastUp := a.lastScaleUpAt
	a.mu.Unlock()
	if !lastUp.IsZero() {
		t.Errorf("lastScaleUpAt updated after failed spawn: %v", lastUp)
	}

	// Tick 2: spawns succeed; lastScaleUpAt now moves.
	if _, err := a.Tick(context.Background(), now.Add(1*time.Second)); err != nil {
		t.Fatalf("tick 2 unexpected error: %v", err)
	}
	a.mu.Lock()
	lastUp = a.lastScaleUpAt
	a.mu.Unlock()
	if lastUp.IsZero() {
		t.Error("lastScaleUpAt still zero after successful scale-up")
	}
}

func TestAutoscaler_ReapDrainTimeout_RestoresReplica(t *testing.T) {
	policy := AutoscalePolicy{Min: 1, Max: 4, TargetInFlight: 3, ScaleUpAfter: 30 * time.Second, ScaleDownAfter: 60 * time.Second}
	a, set, _ := newTestAutoscaler(t, policy, 3)

	// Peg every replica above zero so whichever the scaler picks as victim,
	// its drain cannot complete.
	for _, r := range set.Replicas() {
		r.inFlight.Store(1)
	}

	now := time.Now()
	for offset := 120 * time.Second; offset >= 0; offset -= 5 * time.Second {
		a.window.Add(now.Add(-offset), 0)
	}

	// Use a fast-deadline context to force the drain to fail without waiting 30s.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := a.Tick(ctx, now)
	if err == nil {
		t.Fatal("expected drain/context error")
	}
	// Replica count should be unchanged (victim re-added after the failure).
	if got := set.Size(); got != 3 {
		t.Errorf("size = %d, want 3 (reap aborted)", got)
	}
	// lastScaleDownAt must NOT be updated on failure.
	a.mu.Lock()
	lastDown := a.lastScaleDownAt
	a.mu.Unlock()
	if !lastDown.IsZero() {
		t.Errorf("lastScaleDownAt updated after failed reap: %v", lastDown)
	}
}

func TestAutoscaler_UpdatePolicy_AppliesOnNextTick(t *testing.T) {
	policy := AutoscalePolicy{Min: 1, Max: 4, TargetInFlight: 5, ScaleUpAfter: 30 * time.Second, ScaleDownAfter: 60 * time.Second}
	a, set, _ := newTestAutoscaler(t, policy, 1)

	set.Replicas()[0].inFlight.Store(5)
	now := time.Now()
	for offset := 60 * time.Second; offset >= 0; offset -= 5 * time.Second {
		a.window.Add(now.Add(-offset), 5)
	}

	// Under target=5, current-target math = ceil(1*5/5) = 1 → noop.
	d, _ := a.Tick(context.Background(), now)
	if d != DecisionNoop {
		t.Fatalf("pre-update decision = %v, want Noop", d)
	}

	// Tighten target to 2 → ceil(1*5/2)=3 replicas.
	a.UpdatePolicy(AutoscalePolicy{Min: 1, Max: 4, TargetInFlight: 2, ScaleUpAfter: 30 * time.Second, ScaleDownAfter: 60 * time.Second})

	d, _ = a.Tick(context.Background(), now.Add(1*time.Second))
	if d != DecisionScaleUp {
		t.Errorf("post-update decision = %v, want ScaleUp", d)
	}
	if set.Size() < 3 {
		t.Errorf("size after tighter policy = %d, want >= 3", set.Size())
	}
}

func TestAutoscaler_ColdStart_SpawnsOnZeroReplicas(t *testing.T) {
	policy := AutoscalePolicy{Min: 0, Max: 3, TargetInFlight: 3, IdleToZero: true}
	a, set, sp := newTestAutoscaler(t, policy, 0)

	if set.Size() != 0 {
		t.Fatalf("precondition: expected empty set, got size %d", set.Size())
	}

	if err := a.TriggerColdStart(context.Background()); err != nil {
		t.Fatalf("cold start: %v", err)
	}
	if set.Size() != 1 {
		t.Errorf("size after cold start = %d, want 1", set.Size())
	}
	if sp.CountSpawn() != 1 {
		t.Errorf("spawn count = %d, want 1", sp.CountSpawn())
	}
}

func TestAutoscaler_ColdStart_NoopWhenReplicasExist(t *testing.T) {
	policy := AutoscalePolicy{Min: 0, Max: 3, TargetInFlight: 3, IdleToZero: true}
	a, set, sp := newTestAutoscaler(t, policy, 1)

	before := sp.CountSpawn()
	if err := a.TriggerColdStart(context.Background()); err != nil {
		t.Fatalf("cold start: %v", err)
	}
	if sp.CountSpawn() != before {
		t.Errorf("spawn count changed: before %d, after %d", before, sp.CountSpawn())
	}
	if set.Size() != 1 {
		t.Errorf("size = %d, want 1 (no new spawn)", set.Size())
	}
}

func TestAutoscaler_Status_ReflectsState(t *testing.T) {
	policy := AutoscalePolicy{Min: 1, Max: 4, TargetInFlight: 3, WarmPool: 1, IdleToZero: false, ScaleUpAfter: 30 * time.Second, ScaleDownAfter: 60 * time.Second}
	a, set, _ := newTestAutoscaler(t, policy, 2)

	set.Replicas()[0].inFlight.Store(2)
	set.Replicas()[1].inFlight.Store(4)

	st := a.Status()
	if st.Min != 1 || st.Max != 4 || st.TargetInFlight != 3 {
		t.Errorf("status bounds = {Min:%d Max:%d Target:%d}, want {1 4 3}", st.Min, st.Max, st.TargetInFlight)
	}
	if st.Current != 2 {
		t.Errorf("current = %d, want 2", st.Current)
	}
	if st.MedianInFlight != 3 { // median of [2,4] = 3
		t.Errorf("median = %d, want 3", st.MedianInFlight)
	}
	if st.WarmPool != 1 {
		t.Errorf("warm pool = %d, want 1", st.WarmPool)
	}
	if st.LastDecision != "noop" {
		t.Errorf("last decision = %q, want noop", st.LastDecision)
	}
}

func TestInFlightWindow_PrunesOldSamples(t *testing.T) {
	w := newInFlightWindow(30 * time.Second)
	now := time.Now()
	w.Add(now.Add(-2*time.Minute), 5)
	w.Add(now.Add(-45*time.Second), 7)
	w.Add(now.Add(-5*time.Second), 3)
	// The trigger pruning happens on the next Add.
	w.Add(now, 1)

	// Oldest retained must be within size (30s + jitter on Add timing).
	if oldest := w.oldestAt(); oldest.Before(now.Add(-30 * time.Second)) {
		t.Errorf("oldest sample at %v, should be within 30s of now (%v)", oldest, now)
	}
}

func TestInFlightWindow_Median(t *testing.T) {
	w := newInFlightWindow(60 * time.Second)
	now := time.Now()
	w.Add(now.Add(-30*time.Second), 1)
	w.Add(now.Add(-20*time.Second), 5)
	w.Add(now.Add(-10*time.Second), 9)
	// Odd length: median=5.
	if got := w.sampleMedian(now.Add(-60 * time.Second)); got != 5 {
		t.Errorf("odd median = %v, want 5", got)
	}
	// Narrow the cutoff: only 9 qualifies → median=9.
	if got := w.sampleMedian(now.Add(-15 * time.Second)); got != 9 {
		t.Errorf("narrow median = %v, want 9", got)
	}
}

func TestInFlightWindow_AllZeroSince(t *testing.T) {
	w := newInFlightWindow(60 * time.Second)
	now := time.Now()
	// Seed the window with recent zero samples only.
	for offset := 60 * time.Second; offset >= 0; offset -= 5 * time.Second {
		w.Add(now.Add(-offset), 0)
	}
	from := now.Add(-30 * time.Second)
	if !w.allSamplesZeroSince(from) {
		t.Error("expected allSamplesZeroSince(true) for all-idle window")
	}

	w.Add(now, 2) // one non-zero sample
	if w.allSamplesZeroSince(from) {
		t.Error("expected allSamplesZeroSince(false) after a non-zero sample")
	}
}
