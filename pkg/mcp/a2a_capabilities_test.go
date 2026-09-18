package mcp

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestNewCapabilityStore(t *testing.T) {
	s := NewCapabilityStore()
	if s.globalLimit != (capabilityCounts{4096, 32768, 262144}) || s.adapterLimit != (capabilityCounts{1024, 8192, 65536}) {
		t.Fatal("unexpected fixed limits")
	}
	if NewGateway().capabilities == nil {
		t.Fatal("gateway must construct its store before adapters")
	}
}

func TestCapabilityStore_AtomicReservations(t *testing.T) {
	for _, dimension := range []string{"roots", "tasks", "tombstones"} {
		t.Run(dimension, func(t *testing.T) {
			s := NewCapabilityStore()
			s.globalLimit = capabilityCounts{100, 100, 100}
			s.adapterLimit = capabilityCounts{100, 100, 100}
			switch dimension {
			case "roots":
				s.globalLimit.roots = 7
			case "tasks":
				s.globalLimit.tasks = 7
			case "tombstones":
				s.globalLimit.tombstones = 7
			}
			var admitted atomic.Int32
			var wg sync.WaitGroup
			for i := range 20 {
				g, err := s.newGeneration(string(rune('a' + i)))
				if err != nil {
					t.Fatal(err)
				}
				wg.Go(func() {
					_, r, err := g.reserve(context.Background(), capabilityCounts{1, 1, 1})
					if err == nil {
						admitted.Add(1)
						if err := r.finish(capabilityCounts{1, 1, 1}); err != nil {
							t.Error(err)
						}
					} else if !errors.Is(err, errCapabilityCapacity) {
						t.Error(err)
					}
				})
			}
			wg.Wait()
			if admitted.Load() != 7 || s.used != (capabilityCounts{7, 7, 7}) {
				t.Fatal("reservation was not all-or-nothing", admitted.Load(), s.used)
			}
			s.Close()
			if s.used != (capabilityCounts{}) {
				t.Fatal("close leaked capacity")
			}
		})
	}
}

func TestCapabilityStore_ReplacementDrainsBeforeReclaim(t *testing.T) {
	s := NewCapabilityStore()
	s.globalLimit = capabilityCounts{2, 2, 4}
	a, _ := s.newGeneration("a")
	b, _ := s.newGeneration("b")
	ctx, pending, _ := a.reserve(context.Background(), capabilityCounts{1, 1, 2})
	_, other, _ := b.reserve(context.Background(), capabilityCounts{1, 1, 2})
	if err := other.finish(capabilityCounts{1, 1, 2}); err != nil {
		t.Fatal(err)
	}
	replacement, _ := s.newGeneration("a")
	if ctx.Err() != context.Canceled {
		t.Fatal("replacement must cancel admitted local I/O")
	}
	if _, _, err := replacement.reserve(context.Background(), capabilityCounts{1, 1, 2}); !errors.Is(err, errCapabilityCapacity) {
		t.Fatal("replacement reset global budget", err)
	}
	if err := pending.finish(capabilityCounts{1, 1, 2}); !errors.Is(err, errCapabilityUnavailable) {
		t.Fatal("late result committed retired authority", err)
	}
	if err := pending.finish(capabilityCounts{}); !errors.Is(err, errCapabilityUnavailable) {
		t.Fatal("duplicate callback succeeded", err)
	}
	a.retire()
	if s.used != (capabilityCounts{1, 1, 2}) || b.retired {
		t.Fatal("retirement changed unrelated generation", s.used)
	}
	_, next, err := replacement.reserve(context.Background(), capabilityCounts{1, 1, 2})
	if err != nil {
		t.Fatal(err)
	}
	if err := next.finish(capabilityCounts{}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if s.used != (capabilityCounts{}) {
		t.Fatal("accounting leaked", s.used)
	}
}

func TestCapabilityStore_ExpiryAndRollback(t *testing.T) {
	s := NewCapabilityStore()
	s.adapterLimit = capabilityCounts{1, 1, 2}
	g, _ := s.newGeneration("a")
	_, r, err := g.reserve(context.Background(), capabilityCounts{1, 1, 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := g.reserve(context.Background(), capabilityCounts{0, 1, 0}); !errors.Is(err, errCapabilityCapacity) {
		t.Fatal("per-adapter cap not enforced", err)
	}
	r.expire()
	if s.used != (capabilityCounts{1, 1, 2}) {
		t.Fatal("active callback lost reservation")
	}
	// A direct Message with a context consumes no task or task tombstone.
	if err := r.finish(capabilityCounts{1, 0, 1}); err != nil {
		t.Fatal(err)
	}
	r.expire()
	r.expire()
	if s.used != (capabilityCounts{0, 0, 1}) {
		t.Fatal("expiry must preserve tombstones exactly once", s.used)
	}
	_, unused, err := g.reserve(context.Background(), capabilityCounts{1, 1, 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := unused.finish(capabilityCounts{}); err != nil {
		t.Fatal(err)
	}
	if s.used != (capabilityCounts{0, 0, 1}) {
		t.Fatal("unused Message reservation leaked", s.used)
	}
	g.retire()
	if s.used != (capabilityCounts{}) {
		t.Fatal("retirement failed to release tombstones")
	}
}

func TestCapabilityStore_RetiredTombstonesWaitForAllCallbacks(t *testing.T) {
	s := NewCapabilityStore()
	g, _ := s.newGeneration("a")
	_, committed, _ := g.reserve(context.Background(), capabilityCounts{1, 1, 2})
	if err := committed.finish(capabilityCounts{1, 1, 2}); err != nil {
		t.Fatal(err)
	}
	_, first, _ := g.reserve(context.Background(), capabilityCounts{1, 1, 2})
	_, last, _ := g.reserve(context.Background(), capabilityCounts{1, 1, 2})
	g.retire()
	committed.expire()
	_ = first.finish(capabilityCounts{})
	if s.used != (capabilityCounts{3, 3, 6}) {
		t.Fatal("teardown reclaimed before all callbacks drained", s.used)
	}
	_ = last.finish(capabilityCounts{})
	if s.used != (capabilityCounts{}) || len(s.generations) != 0 {
		t.Fatal("drained generation leaked")
	}
}

func TestCapabilityStore_Close(t *testing.T) {
	gateway := NewGateway()
	s := gateway.capabilities
	g, _ := s.newGeneration("a")
	ctx, r, _ := g.reserve(context.Background(), capabilityCounts{1, 1, 2})
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() { s.Close() })
		wg.Go(func() { _ = r.finish(capabilityCounts{}) })
		wg.Go(func() { g.retire() })
	}
	wg.Wait()
	gateway.Close()
	if ctx.Err() != context.Canceled || s.used != (capabilityCounts{}) {
		t.Fatal("close did not cancel and reclaim")
	}
	if _, err := s.newGeneration("a"); !errors.Is(err, errCapabilityUnavailable) {
		t.Fatal("closed store allowed generation", err)
	}
	if _, _, err := g.reserve(context.Background(), capabilityCounts{}); !errors.Is(err, errCapabilityUnavailable) {
		t.Fatal("retired view allowed reservation", err)
	}
}

func TestCapabilityStore_ReservationFailurePaths(t *testing.T) {
	s := NewCapabilityStore()
	if _, err := s.newGeneration(""); !errors.Is(err, errCapabilityUnavailable) {
		t.Fatal("empty server accepted")
	}
	g, _ := s.newGeneration("a")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := g.reserve(ctx, capabilityCounts{1, 1, 2}); !errors.Is(err, context.Canceled) {
		t.Fatal("ignored canceled context", err)
	}
	if _, _, err := g.reserve(context.Background(), capabilityCounts{-1, 1, 2}); !errors.Is(err, errCapabilityCapacity) {
		t.Fatal("negative reservation accepted", err)
	}
	_, r, _ := g.reserve(context.Background(), capabilityCounts{1, 1, 2})
	if err := r.finish(capabilityCounts{2, 1, 2}); !errors.Is(err, errCapabilityCapacity) {
		t.Fatal("commit grew reservation", err)
	}
	if s.used != (capabilityCounts{}) {
		t.Fatal("failed commit leaked capacity")
	}
}
