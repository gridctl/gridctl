//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/mcp"
)

// TestReplicas_RestartStorm verifies that a replica whose backing process
// exits immediately does not trigger unbounded Reconnect attempts. The
// exponential backoff state must gate retries so the loop cannot spin.
//
// The test simulates the gateway health loop directly: tick at 50ms (well
// below the 1s backoff floor), ask ShouldTry before each Reconnect, and
// Advance on failure. Over a 3-second window the backoff schedule (1s, 2s,
// 4s … ±25% jitter) caps the number of Reconnect attempts to a small
// constant — far below the ~60 attempts an unthrottled loop would produce.
func TestReplicas_RestartStorm(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Immediate-exit binary. Reconnect starts it, sends initialize, and waits
	// for a response that never comes — drainPendingRequests fails the pending
	// call as soon as stdout EOFs, so each attempt returns quickly.
	client := mcp.NewProcessClient("flaky", []string{"/bin/sh", "-c", "exit 1"}, "", nil)
	t.Cleanup(func() { client.Close() }) //nolint:errcheck

	set := mcp.NewReplicaSet("flaky", mcp.ReplicaPolicyRoundRobin, []mcp.AgentClient{client})
	reps := set.Replicas()
	if len(reps) != 1 {
		t.Fatalf("expected 1 replica in set, got %d", len(reps))
	}
	replica := reps[0]
	replica.SetHealthy(false)

	const window = 3 * time.Second
	const tick = 50 * time.Millisecond

	attempts := 0
	advances := make([]time.Time, 0, 8)
	start := time.Now()
	for time.Since(start) < window {
		now := time.Now()
		if replica.Restart().ShouldTry(now) {
			reconnCtx, reconnCancel := context.WithTimeout(ctx, 500*time.Millisecond)
			err := client.Reconnect(reconnCtx)
			reconnCancel()
			if err == nil {
				t.Fatalf("expected Reconnect to fail on immediate-exit binary, got nil")
			}
			replica.Restart().Advance(time.Now())
			advances = append(advances, time.Now())
			attempts++
		}
		time.Sleep(tick)
	}

	// Without backoff this loop would run ~60 times (3s / 50ms). Backoff with
	// 1s floor, doubling, ±25% jitter caps us at roughly 2–4 attempts.
	if attempts < 2 {
		t.Errorf("expected at least 2 reconnect attempts in %v, got %d", window, attempts)
	}
	if attempts > 5 {
		t.Errorf("expected backoff to cap reconnect attempts in %v; got %d (restart storm not throttled)", window, attempts)
	}

	// Inter-attempt spacing should grow roughly monotonically — the second
	// gap must be at least as large as the minimum-jittered 1s floor.
	if len(advances) >= 2 {
		firstGap := advances[1].Sub(advances[0])
		// 1s base with ±25% jitter → minimum ≈ 750ms.
		if firstGap < 600*time.Millisecond {
			t.Errorf("expected first backoff gap ≥ ~750ms (jittered 1s), got %v", firstGap)
		}
	}

	// Restart state should record the attempts and a future NextRetryAt.
	if got := replica.Restart().Attempts(); got < uint32(attempts) {
		t.Errorf("expected Attempts ≥ %d, got %d", attempts, got)
	}
	if !replica.Restart().NextAt().After(start) {
		t.Errorf("expected NextAt to be scheduled after test start; got %v", replica.Restart().NextAt())
	}
}
