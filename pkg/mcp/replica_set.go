package mcp

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Restart-backoff constants for a failing replica.
const (
	restartBackoffInitial    = 1 * time.Second
	restartBackoffCap        = 30 * time.Second
	restartBackoffJitterFrac = 0.25 // ±25%
)

// Dispatch policies for a ReplicaSet.
const (
	ReplicaPolicyRoundRobin       = "round-robin"
	ReplicaPolicyLeastConnections = "least-connections"
)

// ErrNoHealthyReplicas is returned by ReplicaSet.Pick when every replica in
// the set is marked unhealthy.
var ErrNoHealthyReplicas = errors.New("no healthy replicas")

// backoffState tracks exponential-backoff bookkeeping for a failing replica.
// Delays start at restartBackoffInitial, double on each Advance, and cap at
// restartBackoffCap. Every delay is jittered by ±restartBackoffJitterFrac so a
// fleet of replicas that all fail together do not resynchronize their retries.
type backoffState struct {
	mu       sync.Mutex
	attempts uint32
	nextAt   time.Time // zero value = eligible now
}

// Attempts returns the number of consecutive failures observed.
func (b *backoffState) Attempts() uint32 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.attempts
}

// ShouldTry reports whether now is at or past the scheduled next attempt.
// A zero nextAt (fresh state or after Reset) is always eligible.
func (b *backoffState) ShouldTry(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.nextAt.IsZero() {
		return true
	}
	return !now.Before(b.nextAt)
}

// Advance records a failed attempt, computes the next delay (capped, jittered),
// and schedules the next eligible try. Returns the chosen delay so callers can
// log the retry window. Attempts saturates at math.MaxUint32.
func (b *backoffState) Advance(now time.Time) time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	delay := computeBackoff(b.attempts)
	if b.attempts < ^uint32(0) {
		b.attempts++
	}
	b.nextAt = now.Add(delay)
	return delay
}

// Reset clears the backoff: next attempt is eligible immediately.
func (b *backoffState) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.attempts = 0
	b.nextAt = time.Time{}
}

// NextAt returns the scheduled next attempt time. Zero value means "now".
func (b *backoffState) NextAt() time.Time {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.nextAt
}

// computeBackoff returns the jittered delay for the given attempt index.
// attempts=0 yields the initial delay; attempts=1 yields 2× initial; etc.
// Capped at restartBackoffCap before jitter is applied.
func computeBackoff(attempts uint32) time.Duration {
	base := restartBackoffInitial
	for i := uint32(0); i < attempts && base < restartBackoffCap; i++ {
		base *= 2
	}
	if base > restartBackoffCap {
		base = restartBackoffCap
	}
	// Symmetric jitter: delay in [base*(1-frac), base*(1+frac)].
	// Non-cryptographic RNG is deliberate — this is retry-timing jitter.
	span := float64(base) * restartBackoffJitterFrac
	offset := (rand.Float64()*2 - 1) * span //nolint:gosec // retry jitter, not security-sensitive
	return base + time.Duration(offset)
}

// Replica is a single member of a ReplicaSet. It wraps one AgentClient (the
// concrete transport — ProcessClient, StdioClient, Client, etc.) and tracks
// liveness and in-flight request count for dispatch decisions.
type Replica struct {
	id       int
	client   AgentClient
	healthy  atomic.Bool
	inFlight atomic.Int64
	restart  *backoffState

	startedMu sync.Mutex
	startedAt time.Time
}

// ID returns the zero-indexed replica id within its ReplicaSet.
func (r *Replica) ID() int { return r.id }

// Client returns the underlying AgentClient.
func (r *Replica) Client() AgentClient { return r.client }

// Healthy reports whether this replica is eligible for dispatch.
func (r *Replica) Healthy() bool { return r.healthy.Load() }

// SetHealthy marks this replica healthy or unhealthy.
func (r *Replica) SetHealthy(h bool) { r.healthy.Store(h) }

// IncInFlight increments the in-flight request count.
func (r *Replica) IncInFlight() { r.inFlight.Add(1) }

// DecInFlight decrements the in-flight request count.
func (r *Replica) DecInFlight() { r.inFlight.Add(-1) }

// InFlight returns the current in-flight request count.
func (r *Replica) InFlight() int64 { return r.inFlight.Load() }

// Restart returns the replica's restart-backoff state. Never nil.
func (r *Replica) Restart() *backoffState { return r.restart }

// StartedAt returns the time this replica was initialized or most recently
// restarted. Zero value means the replica has not yet started.
func (r *Replica) StartedAt() time.Time {
	r.startedMu.Lock()
	defer r.startedMu.Unlock()
	return r.startedAt
}

// MarkStarted records the current time as the replica's start time. Called
// from NewReplicaSet and from the reconnect path so uptime reflects the
// lifetime of the currently-running backing process.
func (r *Replica) MarkStarted(now time.Time) {
	r.startedMu.Lock()
	defer r.startedMu.Unlock()
	r.startedAt = now
}

// ReplicaSet is a pool of AgentClient replicas for a single logical MCP server.
// Dispatch is determined by the set's policy. A single-replica set behaves
// identically to a direct AgentClient (its Pick always returns that one
// replica when healthy).
type ReplicaSet struct {
	name     string
	policy   string
	mu       sync.RWMutex
	replicas []*Replica
	rrCursor atomic.Int64

	// nextID is the next replica id to hand out. Monotonically increasing;
	// ids are never reused, so removal never invalidates a stored id.
	// Seeded to len(clients) by NewReplicaSet so existing static ids (0..n-1)
	// remain untouched and dynamic adds pick up from n.
	nextID atomic.Int64

	// toolCache caches the last known tool surface (keyed by the canonical
	// server name) so Router.AggregatedTools can still advertise tools when
	// every replica has been reaped (e.g. scale-to-zero). A nil or empty
	// value means the cache is cold.
	toolCacheMu sync.RWMutex
	toolCache   []Tool
}

// NewReplicaSet builds a ReplicaSet from an ordered slice of AgentClients. The
// first client becomes replica-0, and so on. All replicas start healthy. An
// unknown policy falls back to round-robin. The id counter is seeded to
// len(clients) so dynamic adds never collide with static ids.
func NewReplicaSet(name, policy string, clients []AgentClient) *ReplicaSet {
	if policy != ReplicaPolicyRoundRobin && policy != ReplicaPolicyLeastConnections {
		policy = ReplicaPolicyRoundRobin
	}
	set := &ReplicaSet{
		name:     name,
		policy:   policy,
		replicas: make([]*Replica, 0, len(clients)),
	}
	now := time.Now()
	for i, c := range clients {
		r := &Replica{
			id:        i,
			client:    c,
			restart:   &backoffState{},
			startedAt: now,
		}
		r.healthy.Store(true)
		set.replicas = append(set.replicas, r)
	}
	set.nextID.Store(int64(len(clients)))
	// Seed the tool cache from replica-0 so even an immediate scale-to-zero
	// preserves the tool surface advertised to clients.
	if len(clients) > 0 {
		set.SetToolCache(clients[0].Tools())
	}
	return set
}

// Name returns the logical server name.
func (s *ReplicaSet) Name() string { return s.name }

// Policy returns the dispatch policy in effect.
func (s *ReplicaSet) Policy() string { return s.policy }

// Size returns the number of replicas in the set.
func (s *ReplicaSet) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.replicas)
}

// Replicas returns a snapshot slice of the replicas. Callers may iterate the
// snapshot safely; the underlying *Replica values are shared.
func (s *ReplicaSet) Replicas() []*Replica {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Replica, len(s.replicas))
	copy(out, s.replicas)
	return out
}

// Pick chooses a healthy replica according to the set's policy. Returns
// ErrNoHealthyReplicas if every replica is currently marked unhealthy.
func (s *ReplicaSet) Pick() (*Replica, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	n := len(s.replicas)
	if n == 0 {
		return nil, ErrNoHealthyReplicas
	}

	switch s.policy {
	case ReplicaPolicyLeastConnections:
		return s.pickLeastConnectionsLocked()
	default:
		return s.pickRoundRobinLocked()
	}
}

// Client is a convenience that calls Pick and returns the chosen replica's
// AgentClient. Returns nil if no replica is pickable.
func (s *ReplicaSet) Client() AgentClient {
	r, err := s.Pick()
	if err != nil || r == nil {
		return nil
	}
	return r.client
}

// pickRoundRobinLocked assumes s.mu is held. It advances the cursor and scans
// forward at most N positions to find a healthy replica.
func (s *ReplicaSet) pickRoundRobinLocked() (*Replica, error) {
	n := len(s.replicas)
	// Advance once per Pick so fresh callers see a new slot.
	start := int(s.rrCursor.Add(1) - 1)
	for i := 0; i < n; i++ {
		r := s.replicas[((start+i)%n+n)%n]
		if r.Healthy() {
			return r, nil
		}
	}
	return nil, ErrNoHealthyReplicas
}

// pickLeastConnectionsLocked assumes s.mu is held. It returns the healthy
// replica with the lowest in-flight count, breaking ties by lowest id.
func (s *ReplicaSet) pickLeastConnectionsLocked() (*Replica, error) {
	var chosen *Replica
	var chosenInFlight int64
	for _, r := range s.replicas {
		if !r.Healthy() {
			continue
		}
		inFlight := r.InFlight()
		if chosen == nil || inFlight < chosenInFlight ||
			(inFlight == chosenInFlight && r.id < chosen.id) {
			chosen = r
			chosenInFlight = inFlight
		}
	}
	if chosen == nil {
		return nil, ErrNoHealthyReplicas
	}
	return chosen, nil
}

// AddReplica appends a new replica with a monotonically increasing id and
// marks it healthy. Returns the new replica's id. Ids are never reused so
// handles stored by observers (health monitor, status endpoints) remain
// stable across scale events.
func (s *ReplicaSet) AddReplica(client AgentClient) int {
	s.mu.Lock()
	id := int(s.nextID.Add(1) - 1)
	r := &Replica{
		id:        id,
		client:    client,
		restart:   &backoffState{},
		startedAt: time.Now(),
	}
	r.healthy.Store(true)
	s.replicas = append(s.replicas, r)
	s.mu.Unlock()

	// Keep the tool-definition cache warm for scale-to-zero. Done outside
	// s.mu so client.Tools() (which may refresh under its own lock) does
	// not block Pick / Replicas / HealthyCount callers on this replica.
	if client != nil {
		s.SetToolCache(client.Tools())
	}
	return id
}

// RemoveReplica removes the replica with the given id and returns it so the
// caller can close its client. Returns an error if the id is not present.
// The caller is responsible for policy checks (min floor, warm pool, etc.);
// this method only enforces that the replica exists.
func (s *ReplicaSet) RemoveReplica(id int) (*Replica, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, r := range s.replicas {
		if r.id != id {
			continue
		}
		s.replicas = append(s.replicas[:i], s.replicas[i+1:]...)
		return r, nil
	}
	return nil, fmt.Errorf("replica %d not in set %q", id, s.name)
}

// ReinsertReplica puts a previously-removed replica back into the set
// without minting a new id. Used by the autoscaler when a drain fails so
// the in-flight counters and restart state the caller accumulated while
// the replica was detached are preserved. Idempotent: if a replica with
// the same id is already present, this is a no-op.
func (s *ReplicaSet) ReinsertReplica(r *Replica) {
	if r == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.replicas {
		if existing.id == r.id {
			return
		}
	}
	r.SetHealthy(true)
	s.replicas = append(s.replicas, r)
}

// HealthyCount returns the number of replicas currently marked healthy.
func (s *ReplicaSet) HealthyCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	for _, r := range s.replicas {
		if r.Healthy() {
			n++
		}
	}
	return n
}

// MedianInFlight returns the median of in-flight counters across currently
// healthy replicas. Returns 0 when no replica is healthy. Returns float64
// for precision in small-scale clusters where one request can tilt the mean
// noticeably.
func (s *ReplicaSet) MedianInFlight() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	samples := make([]int64, 0, len(s.replicas))
	for _, r := range s.replicas {
		if r.Healthy() {
			samples = append(samples, r.InFlight())
		}
	}
	if len(samples) == 0 {
		return 0
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	mid := len(samples) / 2
	if len(samples)%2 == 1 {
		return float64(samples[mid])
	}
	return float64(samples[mid-1]+samples[mid]) / 2
}

// SetToolCache replaces the tool-definition cache. Callers set this after
// every successful refresh so Router.AggregatedTools can fall back to the
// cached surface when every replica is currently reaped.
func (s *ReplicaSet) SetToolCache(tools []Tool) {
	if len(tools) == 0 {
		return
	}
	s.toolCacheMu.Lock()
	defer s.toolCacheMu.Unlock()
	out := make([]Tool, len(tools))
	copy(out, tools)
	s.toolCache = out
}

// CachedTools returns a copy of the cached tool surface. Returns an empty
// slice when the cache has not been primed.
func (s *ReplicaSet) CachedTools() []Tool {
	s.toolCacheMu.RLock()
	defer s.toolCacheMu.RUnlock()
	if len(s.toolCache) == 0 {
		return nil
	}
	out := make([]Tool, len(s.toolCache))
	copy(out, s.toolCache)
	return out
}
