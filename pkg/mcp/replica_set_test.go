package mcp

import (
	"errors"
	"sync"
	"testing"
	"time"

	"go.uber.org/mock/gomock"
)

func TestBackoff_ProgressionAndCap(t *testing.T) {
	// Expected unjittered bases for attempts 0..7: 1s, 2s, 4s, 8s, 16s, 30s (cap), 30s, 30s.
	expected := []time.Duration{
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		30 * time.Second,
		30 * time.Second,
		30 * time.Second,
	}

	b := &backoffState{}
	for i, want := range expected {
		got := computeBackoff(b.Attempts())
		low := time.Duration(float64(want) * (1 - restartBackoffJitterFrac))
		high := time.Duration(float64(want) * (1 + restartBackoffJitterFrac))
		if got < low || got > high {
			t.Errorf("attempt %d: delay %v not in jitter envelope [%v, %v] around base %v", i, got, low, high, want)
		}
		b.Advance(time.Now())
	}
}

func TestBackoff_ResetReturnsToInitial(t *testing.T) {
	b := &backoffState{}
	for i := 0; i < 5; i++ {
		b.Advance(time.Now())
	}
	if b.Attempts() != 5 {
		t.Fatalf("expected 5 attempts, got %d", b.Attempts())
	}
	b.Reset()
	if got := b.Attempts(); got != 0 {
		t.Errorf("after Reset: attempts = %d, want 0", got)
	}
	if !b.NextAt().IsZero() {
		t.Error("after Reset: NextAt should be zero")
	}
	if !b.ShouldTry(time.Now()) {
		t.Error("after Reset: ShouldTry should be true")
	}
}

func TestBackoff_ShouldTry(t *testing.T) {
	b := &backoffState{}

	now := time.Now()
	if !b.ShouldTry(now) {
		t.Error("fresh backoff: ShouldTry should be true")
	}

	b.Advance(now)
	if b.ShouldTry(now) {
		t.Error("immediately after Advance: ShouldTry should be false")
	}
	// The delay at attempt 0 is 1s ± 25%, so at now+2s it must be eligible.
	if !b.ShouldTry(now.Add(2 * time.Second)) {
		t.Error("2s after Advance on attempt 0: ShouldTry should be true")
	}
}

func TestBackoff_JitterEnvelope(t *testing.T) {
	// With attempts=0 the base is 1s; jitter should keep delays in [0.75s, 1.25s].
	for i := 0; i < 200; i++ {
		got := computeBackoff(0)
		if got < 750*time.Millisecond || got > 1250*time.Millisecond {
			t.Fatalf("jitter escaped envelope: got %v", got)
		}
	}
}

func TestBackoff_Concurrent(t *testing.T) {
	b := &backoffState{}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = b.ShouldTry(time.Now())
				b.Advance(time.Now())
			}
		}()
	}
	wg.Wait()
	// If we get here without -race flagging, we're good.
}

func newTestReplicaSet(t *testing.T, policy string, names ...string) *ReplicaSet {
	t.Helper()
	ctrl := gomock.NewController(t)
	clients := make([]AgentClient, 0, len(names))
	for _, n := range names {
		clients = append(clients, setupMockAgentClient(ctrl, n, nil))
	}
	return NewReplicaSet("svc", policy, clients)
}

func TestReplicaSet_RoundRobin_AllHealthy(t *testing.T) {
	set := newTestReplicaSet(t, ReplicaPolicyRoundRobin, "r0", "r1", "r2")

	var picks []int
	for i := 0; i < 6; i++ {
		r, err := set.Pick()
		if err != nil {
			t.Fatalf("Pick %d: %v", i, err)
		}
		picks = append(picks, r.ID())
	}
	// Cursor advances once per Pick, so across 6 picks every replica
	// appears exactly twice (distribution is uniform, order is fixed).
	counts := [3]int{}
	for _, id := range picks {
		counts[id]++
	}
	if counts != [3]int{2, 2, 2} {
		t.Errorf("round-robin distribution = %v, want [2 2 2] (picks: %v)", counts, picks)
	}
}

func TestReplicaSet_RoundRobin_SkipsUnhealthy(t *testing.T) {
	set := newTestReplicaSet(t, ReplicaPolicyRoundRobin, "r0", "r1", "r2")
	set.Replicas()[1].SetHealthy(false)

	for i := 0; i < 10; i++ {
		r, err := set.Pick()
		if err != nil {
			t.Fatalf("Pick %d: %v", i, err)
		}
		if r.ID() == 1 {
			t.Fatalf("Pick %d returned unhealthy replica 1", i)
		}
	}
}

func TestReplicaSet_LeastConnections(t *testing.T) {
	set := newTestReplicaSet(t, ReplicaPolicyLeastConnections, "r0", "r1", "r2")
	reps := set.Replicas()
	reps[0].inFlight.Store(2)
	reps[1].inFlight.Store(1)
	reps[2].inFlight.Store(0)

	r, err := set.Pick()
	if err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if r.ID() != 2 {
		t.Errorf("least-connections picked replica %d, want 2", r.ID())
	}
}

func TestReplicaSet_LeastConnections_TieBreakLowestID(t *testing.T) {
	set := newTestReplicaSet(t, ReplicaPolicyLeastConnections, "r0", "r1", "r2")
	// All replicas idle.
	r, err := set.Pick()
	if err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if r.ID() != 0 {
		t.Errorf("tie-break picked replica %d, want 0", r.ID())
	}
}

func TestReplicaSet_LeastConnections_SkipsUnhealthy(t *testing.T) {
	set := newTestReplicaSet(t, ReplicaPolicyLeastConnections, "r0", "r1", "r2")
	reps := set.Replicas()
	reps[0].SetHealthy(false) // would be the natural tie-break winner
	reps[1].inFlight.Store(3)
	reps[2].inFlight.Store(1)

	r, err := set.Pick()
	if err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if r.ID() != 2 {
		t.Errorf("picked replica %d, want 2 (skipping unhealthy 0, preferring lower inflight over 1)", r.ID())
	}
}

func TestReplicaSet_NoHealthy(t *testing.T) {
	set := newTestReplicaSet(t, ReplicaPolicyRoundRobin, "r0", "r1")
	for _, r := range set.Replicas() {
		r.SetHealthy(false)
	}

	if _, err := set.Pick(); !errors.Is(err, ErrNoHealthyReplicas) {
		t.Errorf("Pick returned %v, want ErrNoHealthyReplicas", err)
	}
	if got := set.Client(); got != nil {
		t.Errorf("Client returned %v, want nil", got)
	}
}

func TestReplicaSet_Empty(t *testing.T) {
	set := NewReplicaSet("svc", ReplicaPolicyRoundRobin, nil)
	if _, err := set.Pick(); !errors.Is(err, ErrNoHealthyReplicas) {
		t.Errorf("Pick on empty set returned %v, want ErrNoHealthyReplicas", err)
	}
}

func TestReplicaSet_UnknownPolicyFallsBackToRoundRobin(t *testing.T) {
	set := newTestReplicaSet(t, "does-not-exist", "r0", "r1")
	if set.Policy() != ReplicaPolicyRoundRobin {
		t.Errorf("unknown policy = %q, want fallback %q", set.Policy(), ReplicaPolicyRoundRobin)
	}
}

func TestReplicaSet_ConcurrentPick(t *testing.T) {
	set := newTestReplicaSet(t, ReplicaPolicyRoundRobin, "r0", "r1", "r2", "r3")

	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		counts [4]int
	)
	const workers = 16
	const picksPerWorker = 250
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < picksPerWorker; i++ {
				r, err := set.Pick()
				if err != nil {
					t.Errorf("concurrent Pick: %v", err)
					return
				}
				mu.Lock()
				counts[r.ID()]++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	// Every replica must have been chosen at least once (roughly balanced,
	// but we don't assert exact counts under concurrency).
	for id, c := range counts {
		if c == 0 {
			t.Errorf("replica %d was never picked (counts=%v)", id, counts)
		}
	}
}

func TestReplicaSet_ClientIdentity(t *testing.T) {
	ctrl := gomock.NewController(t)
	c0 := setupMockAgentClient(ctrl, "r0", nil)
	c1 := setupMockAgentClient(ctrl, "r1", nil)
	set := NewReplicaSet("svc", ReplicaPolicyRoundRobin, []AgentClient{c0, c1})

	// First Pick advances the cursor to 1, so it returns replica-0 (index
	// (1-1)%2 = 0). This test asserts that the AgentClient pointer the
	// caller gets back is the one we registered, not a wrapper or a copy.
	r, err := set.Pick()
	if err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if r.Client() != c0 {
		t.Errorf("Pick returned client %p, want c0 %p", r.Client(), c0)
	}
}

func TestReplica_InFlightCounters(t *testing.T) {
	ctrl := gomock.NewController(t)
	set := NewReplicaSet("svc", ReplicaPolicyLeastConnections,
		[]AgentClient{setupMockAgentClient(ctrl, "r0", nil)})
	r := set.Replicas()[0]

	if got := r.InFlight(); got != 0 {
		t.Errorf("initial InFlight = %d, want 0", got)
	}
	r.IncInFlight()
	r.IncInFlight()
	if got := r.InFlight(); got != 2 {
		t.Errorf("after 2 Inc: InFlight = %d, want 2", got)
	}
	r.DecInFlight()
	if got := r.InFlight(); got != 1 {
		t.Errorf("after Dec: InFlight = %d, want 1", got)
	}
}
