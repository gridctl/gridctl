//go:build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/controller"
	"github.com/gridctl/gridctl/pkg/mcp"
)

// newProcessTemplate builds an MCPServerConfig template the ProcessSpawner
// will clone every time it spawns a replica. Uses the mock stdio binary
// built by TestMain so autoscaler integration tests don't need a full
// container runtime.
func newProcessTemplate(name string) mcp.MCPServerConfig {
	return mcp.MCPServerConfig{
		Name:         name,
		LocalProcess: true,
		Command:      []string{mockStdioBin},
	}
}

// waitFor polls cond every 50ms until it returns true or deadline expires.
// Returns the final value of cond() so callers can fail with context.
func waitFor(t *testing.T, deadline time.Duration, cond func() bool) bool {
	t.Helper()
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		if cond() {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return cond()
}

// TestAutoscaler_ColdStart_IdleToZero verifies that a server registered with
// `min: 0, idle_to_zero: true` starts with zero replicas, then a tool call
// synchronously spawns the first one.
func TestAutoscaler_ColdStart_IdleToZero(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if mockStdioBin == "" {
		t.Skip("mock stdio server binary not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	gw := mcp.NewGateway()
	t.Cleanup(gw.Close)

	template := newProcessTemplate("test-autoscale-cold")
	spawner := controller.NewProcessSpawner(gw, template)
	policy := mcp.AutoscalePolicy{
		Min: 0, Max: 2, TargetInFlight: 3, IdleToZero: true,
		ScaleUpAfter: 30 * time.Second, ScaleDownAfter: 60 * time.Second,
	}
	if err := gw.RegisterAutoscaler(ctx, template, mcp.ReplicaPolicyRoundRobin, spawner, policy); err != nil {
		t.Fatalf("RegisterAutoscaler: %v", err)
	}

	set := gw.Router().GetReplicaSet(template.Name)
	if set == nil {
		t.Fatal("no replica set registered")
	}
	if got := set.Size(); got != 0 {
		t.Fatalf("initial size = %d, want 0 (idle_to_zero, min 0, warm_pool 0)", got)
	}

	// A tool call against an empty set should trigger a synchronous cold start.
	result, err := gw.HandleToolsCall(ctx, mcp.ToolCallParams{
		Name:      mcp.PrefixTool(template.Name, "echo"),
		Arguments: map[string]any{"message": "wake up"},
	})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool call returned IsError; content=%v", result.Content)
	}
	if got := set.Size(); got != 1 {
		t.Errorf("after cold start: size = %d, want 1", got)
	}
	t.Cleanup(func() {
		// Reap whatever the scaler spawned so we don't leak processes.
		for _, r := range set.Replicas() {
			if _, err := set.RemoveReplica(r.ID()); err == nil {
				_ = spawner.Reap(context.Background(), r)
			}
		}
	})
}

// TestAutoscaler_TickScalesUp_SustainedLoad verifies that when the rolling
// window shows sustained high median in-flight, Tick spawns replicas up to
// target. We drive the window manually to avoid depending on the mock
// server's request latency.
func TestAutoscaler_TickScalesUp_SustainedLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if mockStdioBin == "" {
		t.Skip("mock stdio server binary not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	gw := mcp.NewGateway()
	t.Cleanup(gw.Close)

	template := newProcessTemplate("test-autoscale-up")
	spawner := controller.NewProcessSpawner(gw, template)
	policy := mcp.AutoscalePolicy{
		Min: 1, Max: 4, TargetInFlight: 3,
		ScaleUpAfter:   10 * time.Second,
		ScaleDownAfter: 1 * time.Minute,
	}
	if err := gw.RegisterAutoscaler(ctx, template, mcp.ReplicaPolicyRoundRobin, spawner, policy); err != nil {
		t.Fatalf("RegisterAutoscaler: %v", err)
	}

	set := gw.Router().GetReplicaSet(template.Name)
	t.Cleanup(func() {
		for _, r := range set.Replicas() {
			if _, err := set.RemoveReplica(r.ID()); err == nil {
				_ = spawner.Reap(context.Background(), r)
			}
		}
	})

	// Wait for the initial tick's spawn (Min=1) to complete.
	if !waitFor(t, 10*time.Second, func() bool { return set.Size() >= 1 }) {
		t.Fatalf("initial replica not spawned: size = %d", set.Size())
	}

	// Pin high in-flight on replica-0 so the scaler sees sustained load.
	scaler := gw.GetAutoscaler(template.Name)
	if scaler == nil {
		t.Fatal("no scaler registered")
	}
	for i := 0; i < 9; i++ {
		set.Replicas()[0].IncInFlight()
	}

	// Jump logical time far past the initial-tick cooldown window, then feed
	// a sample immediately before "now" so the sustained-signal check passes.
	now := time.Now().Add(1 * time.Hour)
	if _, err := scaler.Tick(ctx, now.Add(-5*time.Second)); err != nil {
		t.Fatalf("priming Tick: %v", err)
	}
	if _, err := scaler.Tick(ctx, now); err != nil {
		t.Fatalf("scale-up Tick: %v", err)
	}

	if !waitFor(t, 10*time.Second, func() bool { return set.Size() >= 2 }) {
		t.Fatalf("scale-up did not add replicas: size = %d, want >= 2 (policy target ~3)", set.Size())
	}
	if got := set.Size(); got > policy.Max {
		t.Errorf("size = %d, exceeds Max %d", got, policy.Max)
	}
}

// TestAutoscaler_TickScalesDown_IdleShrinksToMin verifies that after a
// sustained-idle window, Tick reaps replicas back toward Min.
func TestAutoscaler_TickScalesDown_IdleShrinksToMin(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if mockStdioBin == "" {
		t.Skip("mock stdio server binary not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	gw := mcp.NewGateway()
	t.Cleanup(gw.Close)

	template := newProcessTemplate("test-autoscale-down")
	spawner := controller.NewProcessSpawner(gw, template)
	policy := mcp.AutoscalePolicy{
		Min: 1, Max: 4, TargetInFlight: 3,
		ScaleUpAfter:   10 * time.Second,
		ScaleDownAfter: 1 * time.Minute,
	}
	if err := gw.RegisterAutoscaler(ctx, template, mcp.ReplicaPolicyRoundRobin, spawner, policy); err != nil {
		t.Fatalf("RegisterAutoscaler: %v", err)
	}
	set := gw.Router().GetReplicaSet(template.Name)
	t.Cleanup(func() {
		for _, r := range set.Replicas() {
			if _, err := set.RemoveReplica(r.ID()); err == nil {
				_ = spawner.Reap(context.Background(), r)
			}
		}
	})

	if !waitFor(t, 10*time.Second, func() bool { return set.Size() >= 1 }) {
		t.Fatalf("initial replica not spawned: size = %d", set.Size())
	}

	// Manually seed 3 replicas so we have something to reap.
	for i := 0; i < 2; i++ {
		client, err := spawner.Spawn(ctx)
		if err != nil {
			t.Fatalf("seed spawn %d: %v", i, err)
		}
		set.AddReplica(client)
	}
	if set.Size() < 3 {
		t.Fatalf("seed failed: size = %d, want 3", set.Size())
	}

	scaler := gw.GetAutoscaler(template.Name)
	// Zero in-flight on all replicas; feed an idle window.
	now := time.Now()
	for offset := 120 * time.Second; offset >= 0; offset -= 5 * time.Second {
		if _, err := scaler.Tick(ctx, now.Add(-offset)); err != nil {
			t.Fatalf("Tick(offset=%v): %v", offset, err)
		}
	}

	if !waitFor(t, 5*time.Second, func() bool { return set.Size() < 3 }) {
		t.Errorf("scale-down did not reap any replica after sustained idle; size = %d", set.Size())
	}
}

// TestAutoscaler_UpdatePolicy_TakesEffectOnNextTick verifies that a hot
// policy update is applied without restarting the scaler.
func TestAutoscaler_UpdatePolicy_TakesEffectOnNextTick(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if mockStdioBin == "" {
		t.Skip("mock stdio server binary not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	gw := mcp.NewGateway()
	t.Cleanup(gw.Close)

	template := newProcessTemplate("test-autoscale-reload")
	spawner := controller.NewProcessSpawner(gw, template)
	policy := mcp.AutoscalePolicy{
		Min: 1, Max: 4, TargetInFlight: 10, // high target -> no scale-up under normal load
		ScaleUpAfter:   10 * time.Second,
		ScaleDownAfter: 1 * time.Minute,
	}
	if err := gw.RegisterAutoscaler(ctx, template, mcp.ReplicaPolicyRoundRobin, spawner, policy); err != nil {
		t.Fatalf("RegisterAutoscaler: %v", err)
	}
	set := gw.Router().GetReplicaSet(template.Name)
	t.Cleanup(func() {
		for _, r := range set.Replicas() {
			if _, err := set.RemoveReplica(r.ID()); err == nil {
				_ = spawner.Reap(context.Background(), r)
			}
		}
	})

	if !waitFor(t, 10*time.Second, func() bool { return set.Size() >= 1 }) {
		t.Fatalf("initial replica not spawned: size = %d", set.Size())
	}
	scaler := gw.GetAutoscaler(template.Name)

	// Pin in-flight at 5; with target=10 that's not enough to scale.
	for i := 0; i < 5; i++ {
		set.Replicas()[0].IncInFlight()
	}
	// Logical time far past the initial-tick cooldown.
	now := time.Now().Add(1 * time.Hour)
	if _, err := scaler.Tick(ctx, now); err != nil {
		t.Fatalf("pre-reload Tick: %v", err)
	}
	if set.Size() != 1 {
		t.Errorf("pre-reload: size = %d, want 1 (target too high)", set.Size())
	}

	// Tighten the target.
	scaler.UpdatePolicy(mcp.AutoscalePolicy{
		Min: 1, Max: 4, TargetInFlight: 1, // very low target
		ScaleUpAfter:   10 * time.Second,
		ScaleDownAfter: 1 * time.Minute,
	})

	// Advance time past the cooldown and tick again — new policy takes effect.
	newNow := now.Add(30 * time.Second)
	if _, err := scaler.Tick(ctx, newNow.Add(-5*time.Second)); err != nil {
		t.Fatalf("priming Tick: %v", err)
	}
	if _, err := scaler.Tick(ctx, newNow); err != nil {
		t.Fatalf("post-update Tick: %v", err)
	}

	if !waitFor(t, 10*time.Second, func() bool { return set.Size() >= 2 }) {
		t.Errorf("post-reload: size = %d, want >= 2 after tightening target", set.Size())
	}
}

// faultySpawner wraps a Spawner and fails the first N Spawn calls before
// delegating. Used to verify the error path does not panic and eventually
// recovers.
type faultySpawner struct {
	inner     mcp.Spawner
	failsLeft atomic.Int32
	mu        sync.Mutex
	spawns    int
	reaps     int
}

func (f *faultySpawner) Spawn(ctx context.Context) (mcp.AgentClient, error) {
	f.mu.Lock()
	f.spawns++
	f.mu.Unlock()
	if f.failsLeft.Add(-1) >= 0 {
		return nil, errors.New("synthetic spawn failure")
	}
	return f.inner.Spawn(ctx)
}
func (f *faultySpawner) Reap(ctx context.Context, r *mcp.Replica) error {
	f.mu.Lock()
	f.reaps++
	f.mu.Unlock()
	return f.inner.Reap(ctx, r)
}

// TestAutoscaler_SpawnerError_Recovers verifies that a spawner that fails
// its first N calls does not crash the scaler, logs WARN, and eventually
// succeeds without the cooldown blocking it.
func TestAutoscaler_SpawnerError_Recovers(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if mockStdioBin == "" {
		t.Skip("mock stdio server binary not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	gw := mcp.NewGateway()
	t.Cleanup(gw.Close)

	template := newProcessTemplate("test-autoscale-err")
	real := controller.NewProcessSpawner(gw, template)
	fs := &faultySpawner{inner: real}
	fs.failsLeft.Store(2) // first two spawns fail

	policy := mcp.AutoscalePolicy{
		Min: 1, Max: 2, TargetInFlight: 3,
		ScaleUpAfter:   10 * time.Second,
		ScaleDownAfter: 1 * time.Minute,
	}
	// Register; the synchronous initial Tick in RegisterAutoscaler will
	// trigger the first failing spawn but not propagate (best-effort).
	if err := gw.RegisterAutoscaler(ctx, template, mcp.ReplicaPolicyRoundRobin, fs, policy); err != nil {
		t.Fatalf("RegisterAutoscaler: %v", err)
	}

	set := gw.Router().GetReplicaSet(template.Name)
	t.Cleanup(func() {
		for _, r := range set.Replicas() {
			if _, err := set.RemoveReplica(r.ID()); err == nil {
				_ = fs.Reap(context.Background(), r)
			}
		}
	})
	scaler := gw.GetAutoscaler(template.Name)

	// After the failures budget is exhausted, further ticks should succeed.
	// Warm-pool catch-up means cooldown does not apply until Min is met.
	if !waitFor(t, 10*time.Second, func() bool {
		_, _ = scaler.Tick(ctx, time.Now())
		return set.Size() >= 1
	}) {
		t.Errorf("scaler never recovered from spawn failures: size = %d, spawns attempted = %d",
			set.Size(), fs.spawns)
	}
	if fs.spawns < 3 {
		t.Errorf("expected at least 3 spawn attempts (2 fails + 1 success), got %d", fs.spawns)
	}
}

// dummy so the file compiles cleanly when -short is used everywhere.
var _ = fmt.Sprintf
