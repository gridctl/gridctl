package mcp

import (
	"errors"
	"sync"
	"testing"

	"go.uber.org/mock/gomock"
)

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
