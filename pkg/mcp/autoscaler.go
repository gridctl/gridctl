package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// AutoscalePolicy controls reactive autoscaling for a single ReplicaSet.
// Values are a snapshot; use Autoscaler.UpdatePolicy to swap in a new one
// without restarting the scaler loop.
type AutoscalePolicy struct {
	Min             int           // Minimum healthy replica count (>= 0; 0 only when IdleToZero).
	Max             int           // Upper bound on replica count (>= 1).
	TargetInFlight  int           // Per-replica in-flight request the scaler holds the median at or below.
	ScaleUpAfter    time.Duration // Window median must exceed target for at least this long.
	ScaleDownAfter  time.Duration // Window median must be below the target for at least this long.
	WarmPool        int           // Extra replicas kept above the load-derived target.
	IdleToZero      bool          // When true, Min may be 0; zero-scale reaps happen after ScaleDownAfter of no traffic.
}

// DefaultAutoscalerInterval is the tick cadence Gateway.StartAutoscaler uses
// when callers pass 0. Kept short enough that reap/spawn latencies are
// observable; the per-direction cooldowns (ScaleUpAfter / ScaleDownAfter)
// gate actual actions so a 10s tick does not cause flapping.
const DefaultAutoscalerInterval = 10 * time.Second

// drainTimeout is the per-replica budget for graceful scale-down. Drain waits
// for in-flight to reach zero before closing the client; past this the
// scaler puts the replica back in rotation and aborts the reap.
const drainTimeout = 30 * time.Second

// drainPollInterval is how often Autoscaler.drainAndReap polls InFlight()
// while waiting for requests to finish.
const drainPollInterval = 100 * time.Millisecond

// Decision is the coarse outcome of a scaler tick.
type Decision int

const (
	DecisionNoop      Decision = iota // No action taken (includes cooldown-gated)
	DecisionScaleUp                   // Spawned one or more replicas this tick
	DecisionScaleDown                 // Reaped one replica this tick
)

// String returns the log-friendly label for a decision.
func (d Decision) String() string {
	switch d {
	case DecisionScaleUp:
		return "up"
	case DecisionScaleDown:
		return "down"
	default:
		return "noop"
	}
}

// Spawner launches or reaps one replica for a named server. One implementation
// per transport class lives in pkg/controller. The Gateway constructs the
// right one at RegisterMCPReplicaSet time and hands it to NewAutoscaler.
//
// Spawn MUST return a ready-to-serve AgentClient: Connect/Initialize/RefreshTools
// have completed before the returned value is added to the set, otherwise the
// first tool call routed to the new replica would fail.
// Reap is given the Replica (pre-removed from dispatch) and should close the
// underlying client and free any resources owned by the spawner.
type Spawner interface {
	Spawn(ctx context.Context) (AgentClient, error)
	Reap(ctx context.Context, r *Replica) error
}

// inFlightWindow is a fixed-size circular buffer of timestamped load samples.
// The scaler consults it to answer "has median been above/below target for
// the full cooldown window?" with O(n) scan over a bounded buffer.
type inFlightWindow struct {
	mu      sync.Mutex
	samples []inFlightSample
	size    time.Duration
}

type inFlightSample struct {
	at     time.Time
	median float64
}

// newInFlightWindow returns a rolling window whose retention is at least size.
// Samples older than size are pruned from the head on every Add.
func newInFlightWindow(size time.Duration) *inFlightWindow {
	if size < 30*time.Second {
		size = 30 * time.Second
	}
	return &inFlightWindow{size: size}
}

// Add inserts a sample and prunes entries older than the window.
func (w *inFlightWindow) Add(at time.Time, median float64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.samples = append(w.samples, inFlightSample{at: at, median: median})
	cutoff := at.Add(-w.size)
	drop := 0
	for _, s := range w.samples {
		if s.at.Before(cutoff) {
			drop++
			continue
		}
		break
	}
	if drop > 0 {
		w.samples = append([]inFlightSample(nil), w.samples[drop:]...)
	}
}

// Latest returns the most recently added sample value, or 0 if empty.
func (w *inFlightWindow) Latest() float64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.samples) == 0 {
		return 0
	}
	return w.samples[len(w.samples)-1].median
}

// Size returns the configured window duration.
func (w *inFlightWindow) Size() time.Duration {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.size
}

// Resize updates the retention without dropping existing samples; the next
// Add call prunes to the new size.
func (w *inFlightWindow) Resize(size time.Duration) {
	if size < 30*time.Second {
		size = 30 * time.Second
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.size = size
}

// sampleMedian returns the median of every sample in the window since from
// (inclusive). Returns 0 when the window has no qualifying samples.
func (w *inFlightWindow) sampleMedian(from time.Time) float64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	vals := make([]float64, 0, len(w.samples))
	for _, s := range w.samples {
		if s.at.Before(from) {
			continue
		}
		vals = append(vals, s.median)
	}
	if len(vals) == 0 {
		return 0
	}
	sort.Float64s(vals)
	mid := len(vals) / 2
	if len(vals)%2 == 1 {
		return vals[mid]
	}
	return (vals[mid-1] + vals[mid]) / 2
}

// allSamplesZeroSince reports whether every sample at or after from has
// median == 0. Returns false if the window has no samples since from.
func (w *inFlightWindow) allSamplesZeroSince(from time.Time) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	sawAny := false
	for _, s := range w.samples {
		if s.at.Before(from) {
			continue
		}
		sawAny = true
		if s.median > 0 {
			return false
		}
	}
	return sawAny
}

// oldestAt returns the timestamp of the oldest retained sample, or the zero
// value when the window is empty.
func (w *inFlightWindow) oldestAt() time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.samples) == 0 {
		return time.Time{}
	}
	return w.samples[0].at
}

// Autoscaler runs the scale-up / scale-down decision loop for one ReplicaSet.
// The ReplicaSet and Spawner are both injected so the decision logic can be
// tested against in-memory fakes.
type Autoscaler struct {
	name    string
	set     *ReplicaSet
	spawner Spawner
	policy  atomic.Pointer[AutoscalePolicy] // swapped atomically on hot reload
	logger  *slog.Logger

	mu              sync.Mutex
	lastScaleUpAt   time.Time
	lastScaleDownAt time.Time
	lastDecision    Decision

	window *inFlightWindow

	// spawnMu serialises every Spawn call for this scaler — cold-start from
	// HandleToolsCall, periodic ticks, and warm-pool catch-up all take it.
	// Without this the cold-start path and a concurrent tick could both call
	// spawner.Spawn on an empty set and produce double the intended replicas.
	spawnMu sync.Mutex
}

// NewAutoscaler builds an Autoscaler bound to a ReplicaSet and Spawner. The
// policy is applied immediately; subsequent UpdatePolicy calls swap it
// atomically. The initial rolling window is max(30s, ScaleUpAfter/2).
func NewAutoscaler(name string, set *ReplicaSet, spawner Spawner, policy AutoscalePolicy, logger *slog.Logger) *Autoscaler {
	if logger == nil {
		logger = slog.Default()
	}
	a := &Autoscaler{
		name:    name,
		set:     set,
		spawner: spawner,
		logger:  logger,
		window:  newInFlightWindow(windowSizeFor(policy.ScaleUpAfter)),
	}
	p := policy
	a.policy.Store(&p)
	return a
}

// Name returns the logical server name this scaler manages.
func (a *Autoscaler) Name() string { return a.name }

// Set returns the managed ReplicaSet.
func (a *Autoscaler) Set() *ReplicaSet { return a.set }

// Policy returns the current policy snapshot.
func (a *Autoscaler) Policy() AutoscalePolicy {
	return *a.policy.Load()
}

// UpdatePolicy swaps the active policy atomically and resizes the rolling
// window to match the new ScaleUpAfter. Called by the reload handler on
// policy-only changes so the next tick applies the new shape.
func (a *Autoscaler) UpdatePolicy(p AutoscalePolicy) {
	a.policy.Store(&p)
	a.window.Resize(windowSizeFor(p.ScaleUpAfter))
	a.logger.Info("autoscale policy updated",
		"server", a.name,
		"min", p.Min, "max", p.Max,
		"target_in_flight", p.TargetInFlight,
		"warm_pool", p.WarmPool,
		"idle_to_zero", p.IdleToZero,
	)
}

// Status returns a snapshot of scaler state for the /api/stack/health payload.
// Returns zeroed timestamps until the corresponding action has occurred.
func (a *Autoscaler) Status() AutoscaleStatus {
	p := a.Policy()
	a.mu.Lock()
	lastUp, lastDown, lastDecision := a.lastScaleUpAt, a.lastScaleDownAt, a.lastDecision
	a.mu.Unlock()

	current := a.set.HealthyCount()
	median := a.set.MedianInFlight()
	target := a.computeTarget(current, median, p)

	st := AutoscaleStatus{
		Min:            p.Min,
		Max:            p.Max,
		Current:        current,
		Target:         target,
		TargetInFlight: p.TargetInFlight,
		MedianInFlight: int64(math.Round(median)),
		LastDecision:   lastDecision.String(),
		WarmPool:       p.WarmPool,
		IdleToZero:     p.IdleToZero,
	}
	if !lastUp.IsZero() {
		t := lastUp
		st.LastScaleUpAt = &t
	}
	if !lastDown.IsZero() {
		t := lastDown
		st.LastScaleDownAt = &t
	}
	return st
}

// windowSizeFor returns the rolling-window retention for a given
// ScaleUpAfter: max(30s, ScaleUpAfter/2). The /2 gives the scaler enough
// history to distinguish a sustained spike from a single noisy sample.
func windowSizeFor(scaleUpAfter time.Duration) time.Duration {
	half := scaleUpAfter / 2
	if half < 30*time.Second {
		return 30 * time.Second
	}
	return half
}

// Tick runs one evaluation cycle. Safe to call concurrently with tool dispatch.
// Returns the decision and any spawn/reap error. Errors are also logged at
// WARN so operators see them in the structured log stream.
func (a *Autoscaler) Tick(ctx context.Context, now time.Time) (Decision, error) {
	p := a.Policy()

	// 1. Sample median in-flight and feed the rolling window.
	median := a.set.MedianInFlight()
	a.window.Add(now, median)

	current := a.set.HealthyCount()

	// 2. Determine raw target and apply warm pool + clamps.
	target := a.computeTarget(current, median, p)

	// 3. Handle idle-to-zero path first: aggressive reap only after
	//    ScaleDownAfter of sustained zero-traffic windows.
	if p.IdleToZero && p.Min == 0 && current > 0 {
		sinceWindow := now.Add(-p.ScaleDownAfter)
		// Require the rolling window to cover the full cooldown — otherwise
		// a freshly-started scaler would scale to zero on its first tick.
		if oldest := a.window.oldestAt(); !oldest.IsZero() && !oldest.After(sinceWindow) && a.window.allSamplesZeroSince(sinceWindow) {
			return a.scaleDown(ctx, now, current, target, p, "idle_to_zero")
		}
	}

	switch {
	case target > current:
		return a.scaleUp(ctx, now, current, target, p, median)
	case target < current:
		return a.scaleDown(ctx, now, current, target, p, "load")
	default:
		return a.noop(now, current, target, median, "load")
	}
}

// TriggerColdStart spawns the first replica synchronously. Used by
// HandleToolsCall when a tool call arrives while the set is at zero replicas
// under an idle-to-zero policy. Returns nil once at least one replica has
// been added (including concurrently by another caller that won the race).
// Holds a.spawnMu so a racing periodic Tick cannot spawn in parallel.
func (a *Autoscaler) TriggerColdStart(ctx context.Context) error {
	a.spawnMu.Lock()
	defer a.spawnMu.Unlock()

	if a.set.HealthyCount() > 0 {
		return nil
	}
	p := a.Policy()
	if p.Max < 1 {
		return errors.New("autoscale policy max < 1; cold start refused")
	}

	tracer := otel.Tracer("gridctl.autoscaler")
	ctx, span := tracer.Start(ctx, "mcp.autoscale.cold_start")
	span.SetAttributes(
		attribute.String("server.name", a.name),
		attribute.String("mcp.autoscale.direction", "up"),
		attribute.String("mcp.autoscale.reason", "cold_start"),
	)
	defer span.End()

	client, err := a.spawner.Spawn(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		a.logger.Warn("autoscale cold-start spawn failed",
			"server", a.name, "direction", "up", "error", err,
		)
		return fmt.Errorf("cold start %s: %w", a.name, err)
	}
	// Record the cooldown timestamp BEFORE adding the replica so a concurrent
	// tick that observes current==1 right after AddReplica cannot decide to
	// spawn again (its cooldown check now sees lastScaleUpAt populated).
	a.mu.Lock()
	a.lastScaleUpAt = time.Now()
	a.lastDecision = DecisionScaleUp
	a.mu.Unlock()
	id := a.set.AddReplica(client)
	a.logger.Info("autoscale decision",
		"server", a.name,
		"direction", "up",
		"current", a.set.HealthyCount(),
		"target", 1,
		"median_in_flight", float64(0),
		"reason", "cold_start",
		"replica_id", id,
	)
	return nil
}

// computeTarget returns the clamped replica target for the current sample.
// The load-derived figure is floored at Min, then WarmPool is added on top
// so the warm pool always sits above the load floor (not merely above Min).
// current == 0 under idle-to-zero bypasses load-derived math (the scaler is
// driven by the cold-start trigger in that state).
func (a *Autoscaler) computeTarget(current int, median float64, p AutoscalePolicy) int {
	loadTarget := 0
	if current > 0 && p.TargetInFlight > 0 {
		loadTarget = int(math.Ceil(float64(current) * median / float64(p.TargetInFlight)))
	}
	if loadTarget < p.Min {
		loadTarget = p.Min
	}
	target := loadTarget + p.WarmPool
	if target > p.Max {
		target = p.Max
	}
	if target < 0 {
		target = 0
	}
	return target
}

// scaleUp spawns up to target-current replicas. Cooldown applies unless
// current < Min + WarmPool (the pool is eager). On any spawn failure the
// lastScaleUpAt timestamp is NOT updated so the next tick retries.
func (a *Autoscaler) scaleUp(ctx context.Context, now time.Time, current, target int, p AutoscalePolicy, median float64) (Decision, error) {
	a.mu.Lock()
	lastUp := a.lastScaleUpAt
	a.mu.Unlock()

	poolFloor := p.Min + p.WarmPool
	eager := current < poolFloor
	if !eager && !lastUp.IsZero() && now.Sub(lastUp) < p.ScaleUpAfter {
		a.logDecision("up-noop", current, target, median, "cooldown")
		return a.recordDecision(DecisionNoop)
	}

	// Only require the sustained-signal condition on ordinary load-driven
	// scale-ups; warm-pool catch-up is eager.
	if !eager {
		sinceWindow := now.Add(-p.ScaleUpAfter)
		if a.window.sampleMedian(sinceWindow) < float64(p.TargetInFlight) {
			a.logDecision("up-noop", current, target, median, "insufficient_signal")
			return a.recordDecision(DecisionNoop)
		}
	}

	need := target - current
	if need <= 0 {
		return a.noop(now, current, target, median, "load")
	}

	// Spawn each replica under its own span so a slow spawn doesn't stall the
	// rest of the tick. spawnMu is shared with TriggerColdStart so a concurrent
	// cold-start caller cannot double-spawn on a near-empty set.
	a.spawnMu.Lock()
	defer a.spawnMu.Unlock()

	tracer := otel.Tracer("gridctl.autoscaler")
	reason := "load"
	if eager {
		reason = "warm_pool"
	}
	var spawnErr error
	added := 0
	for i := 0; i < need; i++ {
		ctx, span := tracer.Start(ctx, "mcp.autoscale.spawn")
		span.SetAttributes(
			attribute.String("server.name", a.name),
			attribute.String("mcp.autoscale.direction", "up"),
			attribute.String("mcp.autoscale.reason", reason),
		)

		client, err := a.spawner.Spawn(ctx)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			span.End()
			spawnErr = err
			a.logger.Warn("autoscale spawn failed",
				"server", a.name,
				"direction", "up",
				"current", current+added,
				"target", target,
				"error", err,
			)
			break
		}
		id := a.set.AddReplica(client)
		added++
		span.SetAttributes(attribute.Int("mcp.replica.id", id))
		span.End()
	}

	if added > 0 {
		a.mu.Lock()
		a.lastScaleUpAt = now
		a.lastDecision = DecisionScaleUp
		a.mu.Unlock()
	}

	// Use a representative reason for the rollup log. The per-spawn warnings
	// above already carry error detail.
	a.logger.Info("autoscale decision",
		"server", a.name,
		"direction", "up",
		"current", current+added,
		"target", target,
		"median_in_flight", median,
		"reason", reason,
		"spawned", added,
	)

	if spawnErr != nil {
		return DecisionScaleUp, spawnErr
	}
	return DecisionScaleUp, nil
}

// scaleDown reaps at most one replica per tick, picking the replica with
// lowest in-flight (tie-broken by oldest). Cooldown is tracked per-direction
// so a burst that just scaled up does not block a later scale-down.
func (a *Autoscaler) scaleDown(ctx context.Context, now time.Time, current, target int, p AutoscalePolicy, reason string) (Decision, error) {
	a.mu.Lock()
	lastDown := a.lastScaleDownAt
	a.mu.Unlock()

	median := a.window.Latest()

	// Refuse to cross floor even if target says we should.
	floor := p.Min + p.WarmPool
	if p.IdleToZero && reason == "idle_to_zero" {
		floor = 0
	}
	if current <= floor {
		a.logDecision("down-noop", current, target, median, "at_floor")
		return a.recordDecision(DecisionNoop)
	}

	if !lastDown.IsZero() && now.Sub(lastDown) < p.ScaleDownAfter {
		a.logDecision("down-noop", current, target, median, "cooldown")
		return a.recordDecision(DecisionNoop)
	}

	// Only require the sustained-below signal on ordinary load-driven
	// downs; the idle_to_zero path was already gated by allSamplesZeroSince.
	if reason == "load" {
		sinceWindow := now.Add(-p.ScaleDownAfter)
		if threshold := float64(p.TargetInFlight) / 2; a.window.sampleMedian(sinceWindow) > threshold {
			a.logDecision("down-noop", current, target, median, "insufficient_signal")
			return a.recordDecision(DecisionNoop)
		}
	}

	victim := a.pickReapTarget()
	if victim == nil {
		a.logDecision("down-noop", current, target, median, "no_candidate")
		return a.recordDecision(DecisionNoop)
	}

	tracer := otel.Tracer("gridctl.autoscaler")
	ctx, span := tracer.Start(ctx, "mcp.autoscale.reap")
	span.SetAttributes(
		attribute.String("server.name", a.name),
		attribute.String("mcp.autoscale.direction", "down"),
		attribute.String("mcp.autoscale.reason", reason),
		attribute.Int("mcp.replica.id", victim.ID()),
	)
	defer span.End()

	if err := a.drainAndReap(ctx, victim); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		a.logger.Warn("autoscale reap failed; re-adding replica",
			"server", a.name,
			"direction", "down",
			"replica_id", victim.ID(),
			"current", current,
			"target", target,
			"error", err,
		)
		// On any drain failure — deadline exceeded or ctx cancelled — put
		// the original *Replica pointer back so in-flight counters and
		// restart bookkeeping stay consistent. Next tick may retry.
		a.set.ReinsertReplica(victim)
		// Do not update lastScaleDownAt on failure — next tick may retry.
		return DecisionScaleDown, err
	}

	a.mu.Lock()
	a.lastScaleDownAt = now
	a.lastDecision = DecisionScaleDown
	a.mu.Unlock()
	a.logger.Info("autoscale decision",
		"server", a.name,
		"direction", "down",
		"current", a.set.HealthyCount(),
		"target", target,
		"median_in_flight", median,
		"reason", reason,
		"replica_id", victim.ID(),
	)
	return DecisionScaleDown, nil
}

// noop logs a single-line decision and records it without taking any action.
func (a *Autoscaler) noop(_ time.Time, current, target int, median float64, reason string) (Decision, error) {
	a.logDecision("noop", current, target, median, reason)
	a.mu.Lock()
	a.lastDecision = DecisionNoop
	a.mu.Unlock()
	return DecisionNoop, nil
}

// logDecision emits the single structured log line the spec mandates.
func (a *Autoscaler) logDecision(direction string, current, target int, median float64, reason string) {
	a.logger.Info("autoscale decision",
		"server", a.name,
		"direction", direction,
		"current", current,
		"target", target,
		"median_in_flight", median,
		"reason", reason,
	)
}

// recordDecision stores the last decision label for Status() and returns.
func (a *Autoscaler) recordDecision(d Decision) (Decision, error) {
	a.mu.Lock()
	a.lastDecision = d
	a.mu.Unlock()
	return d, nil
}

// pickReapTarget selects the replica to reap: lowest in-flight, oldest on tie.
// Returns nil if the set is empty.
func (a *Autoscaler) pickReapTarget() *Replica {
	replicas := a.set.Replicas()
	if len(replicas) == 0 {
		return nil
	}
	chosen := replicas[0]
	for _, r := range replicas[1:] {
		cInFlight := chosen.InFlight()
		rInFlight := r.InFlight()
		switch {
		case rInFlight < cInFlight:
			chosen = r
		case rInFlight == cInFlight && r.StartedAt().Before(chosen.StartedAt()):
			chosen = r
		}
	}
	return chosen
}

// errDrainTimeout signals that a reap's drain deadline was exceeded so the
// scaler can put the replica back in rotation.
var errDrainTimeout = errors.New("drain deadline exceeded")

// drainAndReap removes the replica from dispatch immediately, waits up to
// drainTimeout for its in-flight counter to reach 0, then asks the spawner
// to close the underlying client. When the drain deadline is exceeded the
// replica is NOT closed so the caller can put it back in rotation.
//
// An unconditional grace poll runs before checking InFlight so that a
// concurrent Pick caller who already resolved the replica but has not yet
// called IncInFlight has time to do so. Without this window, a drain could
// see InFlight==0 immediately after RemoveReplica and close a client that a
// racing tool call is about to use.
func (a *Autoscaler) drainAndReap(ctx context.Context, r *Replica) error {
	if _, err := a.set.RemoveReplica(r.ID()); err != nil {
		return fmt.Errorf("remove replica %d: %w", r.ID(), err)
	}

	deadline := time.Now().Add(drainTimeout)
	for {
		// Pick-vs-reap grace window: wait one poll before declaring idle.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(drainPollInterval):
		}
		if r.InFlight() == 0 {
			break
		}
		if time.Now().After(deadline) {
			return errDrainTimeout
		}
	}
	return a.spawner.Reap(ctx, r)
}

// AutoscaleStatus is the serialisable snapshot included in MCPServerStatus
// when a server has autoscale configured. Fields mirror the spec in
// new_feature.md § "Observability surface".
type AutoscaleStatus struct {
	Min             int        `json:"min"`
	Max             int        `json:"max"`
	Current         int        `json:"current"`
	Target          int        `json:"target"`
	TargetInFlight  int        `json:"targetInFlight"`
	MedianInFlight  int64      `json:"medianInFlight"`
	LastScaleUpAt   *time.Time `json:"lastScaleUpAt,omitempty"`
	LastScaleDownAt *time.Time `json:"lastScaleDownAt,omitempty"`
	LastDecision    string     `json:"lastDecision"`
	WarmPool        int        `json:"warmPool,omitempty"`
	IdleToZero      bool       `json:"idleToZero,omitempty"`
}
