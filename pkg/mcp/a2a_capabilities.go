package mcp

import (
	"context"
	"crypto/rand"
	"io"
	"sync"
	"time"
)

var (
	errCapabilityUnavailable = &capabilityError{code: "capability_unavailable"}
	errCapabilityCapacity    = &capabilityError{code: "capability_capacity_exhausted"}
)

// Capability errors never retain the request, remote values, or a cause chain.
type capabilityError struct {
	code      string
	retryable bool
}

func (e *capabilityError) Error() string   { return e.code }
func (e *capabilityError) Retryable() bool { return e.retryable }

type capabilityCounts struct {
	roots, tasks, tombstones int
}

func (c capabilityCounts) add(b capabilityCounts) capabilityCounts {
	return capabilityCounts{c.roots + b.roots, c.tasks + b.tasks, c.tombstones + b.tombstones}
}

func (c capabilityCounts) sub(b capabilityCounts) capabilityCounts {
	return capabilityCounts{c.roots - b.roots, c.tasks - b.tasks, c.tombstones - b.tombstones}
}

func (c capabilityCounts) fits(limit capabilityCounts) bool {
	return c.roots >= 0 && c.tasks >= 0 && c.tombstones >= 0 &&
		c.roots <= limit.roots && c.tasks <= limit.tasks && c.tombstones <= limit.tombstones
}

// CapabilityStore owns a gateway's process-local capability budget. Generations
// share one lock so replacement cannot reset the global budget while callbacks
// from a retired adapter are still draining. It retains no application payloads.
type CapabilityStore struct {
	mu           sync.Mutex
	closed       bool
	used         capabilityCounts
	globalLimit  capabilityCounts
	adapterLimit capabilityCounts
	generations  map[*capabilityGeneration]struct{}
	current      map[string]*capabilityGeneration
	now          func() time.Time
	random       io.Reader
}

// NewCapabilityStore creates an empty store with the fixed gateway and adapter
// limits. Each Gateway constructs exactly one store before creating clients.
func NewCapabilityStore() *CapabilityStore {
	return &CapabilityStore{
		globalLimit:  capabilityCounts{4096, 32768, 262144},
		adapterLimit: capabilityCounts{1024, 8192, 65536},
		generations:  make(map[*capabilityGeneration]struct{}),
		current:      make(map[string]*capabilityGeneration),
		now:          time.Now,
		random:       rand.Reader,
	}
}

type capabilityGeneration struct {
	store        *CapabilityStore
	server       string
	retired      bool
	used         capabilityCounts
	reservations map[*capabilityReservation]struct{}
	identity     capabilityIdentity
	roots        map[*capabilityRoot]struct{}
	contexts     map[[32]byte]*capabilityRoot
	tasks        map[[32]byte]*capabilityTask
	remoteIDs    map[capabilityRemoteKey]struct{}
	failedTokens float64
	failedAt     time.Time
}

// A reservation accounts for both pending I/O and committed authority. Active
// callbacks keep their complete charges during retirement, until finish runs.
// Unused charges can only be returned atomically through finish.
type capabilityReservation struct {
	generation *capabilityGeneration
	counts     capabilityCounts
	active     bool
	cancel     context.CancelFunc
}

func (s *CapabilityStore) newGeneration(server string) (*capabilityGeneration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.newGenerationLocked(capabilityIdentity{server: server})
}

func (s *CapabilityStore) newGenerationLocked(identity capabilityIdentity) (*capabilityGeneration, error) {
	server := identity.server
	if s.closed || server == "" {
		return nil, errCapabilityUnavailable
	}
	if old := s.current[server]; old != nil {
		old.retireLocked()
	}
	g := &capabilityGeneration{store: s, server: server, reservations: make(map[*capabilityReservation]struct{})}
	g.identity = identity
	g.roots = make(map[*capabilityRoot]struct{})
	g.contexts = make(map[[32]byte]*capabilityRoot)
	g.tasks = make(map[[32]byte]*capabilityTask)
	g.remoteIDs = make(map[capabilityRemoteKey]struct{})
	g.failedTokens = 100
	g.failedAt = s.now()
	s.current[server] = g
	s.generations[g] = struct{}{}
	return g, nil
}

func (g *capabilityGeneration) reserve(ctx context.Context, counts capabilityCounts) (context.Context, *capabilityReservation, error) {
	s := g.store
	s.mu.Lock()
	defer s.mu.Unlock()
	return g.reserveLocked(ctx, counts)
}

func (g *capabilityGeneration) reserveLocked(ctx context.Context, counts capabilityCounts) (context.Context, *capabilityReservation, error) {
	s := g.store
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if g.retired || s.closed {
		return nil, nil, errCapabilityUnavailable
	}
	if !counts.fits(s.adapterLimit) || !g.used.add(counts).fits(s.adapterLimit) || !s.used.add(counts).fits(s.globalLimit) {
		return nil, nil, errCapabilityCapacity
	}
	ctx, cancel := context.WithCancel(ctx)
	r := &capabilityReservation{generation: g, counts: counts, active: true, cancel: cancel}
	g.reservations[r] = struct{}{}
	g.used = g.used.add(counts)
	s.used = s.used.add(counts)
	return ctx, r, nil
}

// finish drains a callback exactly once. Retired generations cannot commit any
// authority; their pending budget is returned only here. keep describes validated
// records, not a remote response that has yet to pass identity checks.
func (r *capabilityReservation) finish(keep capabilityCounts) error {
	s := r.generation.store
	s.mu.Lock()
	defer s.mu.Unlock()
	return r.finishLocked(keep)
}

func (r *capabilityReservation) finishLocked(keep capabilityCounts) error {
	s := r.generation.store
	if !r.active {
		return errCapabilityUnavailable
	}
	r.active = false
	r.cancel()
	if r.generation.retired || s.closed {
		r.generation.drainLocked()
		return errCapabilityUnavailable
	}
	if !keep.fits(r.counts) {
		r.releaseLocked(r.counts)
		return errCapabilityCapacity
	}
	r.releaseLocked(r.counts.sub(keep))
	return nil
}

// expire releases live capacity but preserves collision protection for the
// entire generation. In-flight callbacks must drain before their charges move.
func (r *capabilityReservation) expire() {
	s := r.generation.store
	s.mu.Lock()
	defer s.mu.Unlock()
	if !r.active && !r.generation.retired {
		r.releaseLocked(capabilityCounts{roots: r.counts.roots, tasks: r.counts.tasks})
	}
}

func (r *capabilityReservation) releaseLocked(counts capabilityCounts) {
	g := r.generation
	r.counts = r.counts.sub(counts)
	g.used = g.used.sub(counts)
	g.store.used = g.store.used.sub(counts)
	if r.counts == (capabilityCounts{}) && !r.active {
		delete(g.reservations, r)
	}
	if g.retired && len(g.reservations) == 0 {
		delete(g.store.generations, g)
	}
}

func (g *capabilityGeneration) retire() {
	g.store.mu.Lock()
	defer g.store.mu.Unlock()
	g.retireLocked()
}

func (s *CapabilityStore) retireServer(server string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if generation := s.current[server]; generation != nil {
		generation.retireLocked()
	}
}

func (g *capabilityGeneration) retireLocked() {
	if g.retired {
		return
	}
	g.retired = true
	if g.store.current[g.server] == g {
		delete(g.store.current, g.server)
	}
	for r := range g.reservations {
		if r.active {
			// Cancel only local I/O. Retirement never requests remote cancellation.
			r.cancel()
		}
	}
	g.drainLocked()
}

func (g *capabilityGeneration) drainLocked() {
	for r := range g.reservations {
		if r.active {
			return
		}
	}
	for r := range g.reservations {
		r.releaseLocked(r.counts)
	}
	clear(g.roots)
	clear(g.contexts)
	clear(g.tasks)
	clear(g.remoteIDs)
	delete(g.store.generations, g)
}

// Close invalidates all generations and cancels admitted local operations.
// Pending callbacks remain charged until they drain; Close never waits on I/O.
func (s *CapabilityStore) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	for g := range s.generations {
		g.retireLocked()
	}
}
