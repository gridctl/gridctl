package persist

import (
	"sync"
	"sync/atomic"
)

// busSubscriberBuffer is the default per-subscriber channel depth.
// Slow consumers cap out here; once the channel is full the publisher
// drops the event for that subscriber and flags `dropped`, which the
// SSE handler surfaces as a `stream_restarted` sentinel on the next
// successful send. Sized to absorb a reasonable burst of node events
// from a handful of concurrent runs without forcing every consumer to
// stay on the hot path.
const busSubscriberBuffer = 256

// Bus is a process-wide pub/sub fan-out for run lifecycle events. The
// per-run JSONL ledger remains the durable source of truth; the bus is
// a best-effort live-tail surface for cross-run observers (the /runs
// workspace, the bottom-panel waterfall, anything else that needs to
// see events as they happen without subscribing per-run).
//
// Sends are non-blocking — a slow subscriber loses events rather than
// stalling the recorder. Dropped subscribers see a one-shot
// `stream_restarted` notification on the next successful delivery so
// clients can decide whether to refetch the affected run.
type Bus struct {
	mu   sync.RWMutex
	subs map[*Subscriber]struct{}
}

// Subscriber is one live tail of the global event stream. The SSE
// handler reads from C, checks Dropped before each send, and emits a
// sentinel when the flag flips from true → false.
type Subscriber struct {
	C       chan Event
	dropped atomic.Bool
}

// NewBus constructs an empty bus.
func NewBus() *Bus {
	return &Bus{subs: make(map[*Subscriber]struct{})}
}

// Subscribe registers a new subscriber and returns its channel along
// with an unsubscribe function. Callers must Unsubscribe on shutdown
// or the bus will hold the channel alive past its owning goroutine.
func (b *Bus) Subscribe() (*Subscriber, func()) {
	sub := &Subscriber{C: make(chan Event, busSubscriberBuffer)}
	b.mu.Lock()
	b.subs[sub] = struct{}{}
	b.mu.Unlock()
	return sub, func() {
		b.mu.Lock()
		if _, ok := b.subs[sub]; ok {
			delete(b.subs, sub)
			close(sub.C)
		}
		b.mu.Unlock()
	}
}

// Publish fans out an event to every subscriber. Subscribers whose
// channel is full have their dropped flag set instead of receiving the
// event; the SSE handler converts that into a `stream_restarted`
// frame on the next successful send.
func (b *Bus) Publish(ev Event) {
	b.mu.RLock()
	for sub := range b.subs {
		select {
		case sub.C <- ev:
		default:
			sub.dropped.Store(true)
		}
	}
	b.mu.RUnlock()
}

// TakeDropped clears the dropped flag and returns whether the
// subscriber had missed events since the last call. SSE handlers call
// this before each send so the sentinel rides ahead of the next event.
func (s *Subscriber) TakeDropped() bool {
	return s.dropped.Swap(false)
}
