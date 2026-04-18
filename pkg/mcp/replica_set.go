package mcp

import (
	"errors"
	"sync"
	"sync/atomic"
)

// Dispatch policies for a ReplicaSet.
const (
	ReplicaPolicyRoundRobin       = "round-robin"
	ReplicaPolicyLeastConnections = "least-connections"
)

// ErrNoHealthyReplicas is returned by ReplicaSet.Pick when every replica in
// the set is marked unhealthy.
var ErrNoHealthyReplicas = errors.New("no healthy replicas")

// backoffState carries exponential-backoff bookkeeping for per-replica
// restart scheduling. Fields and methods are introduced alongside the
// restart loop in a later phase; the type is declared here so the Replica
// layout is stable.
type backoffState struct{}

// Replica is a single member of a ReplicaSet. It wraps one AgentClient (the
// concrete transport — ProcessClient, StdioClient, Client, etc.) and tracks
// liveness and in-flight request count for dispatch decisions.
type Replica struct {
	id       int
	client   AgentClient
	healthy  atomic.Bool
	inFlight atomic.Int64
	restart  *backoffState
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
}

// NewReplicaSet builds a ReplicaSet from an ordered slice of AgentClients. The
// first client becomes replica-0, and so on. All replicas start healthy. An
// unknown policy falls back to round-robin.
func NewReplicaSet(name, policy string, clients []AgentClient) *ReplicaSet {
	if policy != ReplicaPolicyRoundRobin && policy != ReplicaPolicyLeastConnections {
		policy = ReplicaPolicyRoundRobin
	}
	set := &ReplicaSet{
		name:     name,
		policy:   policy,
		replicas: make([]*Replica, 0, len(clients)),
	}
	for i, c := range clients {
		r := &Replica{
			id:      i,
			client:  c,
			restart: &backoffState{},
		}
		r.healthy.Store(true)
		set.replicas = append(set.replicas, r)
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
		if chosen == nil || inFlight < chosenInFlight {
			chosen = r
			chosenInFlight = inFlight
		}
	}
	if chosen == nil {
		return nil, ErrNoHealthyReplicas
	}
	return chosen, nil
}
