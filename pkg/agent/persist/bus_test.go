package persist

import (
	"testing"
	"time"
)

// busFanOutEvent is the fixture every bus test reuses — a benign
// `run_started` payload with a known run ID so assertions can match on
// the envelope without dragging in the marshalling code paths.
func busFanOutEvent(runID string, seq uint64) Event {
	return Event{
		RunID: runID,
		Seq:   seq,
		Time:  time.Unix(0, 0).UTC(),
		Type:  EventRunStarted,
	}
}

func TestBus_PublishFansOutToSubscribers(t *testing.T) {
	bus := NewBus()
	subA, unsubA := bus.Subscribe()
	t.Cleanup(unsubA)
	subB, unsubB := bus.Subscribe()
	t.Cleanup(unsubB)

	want := busFanOutEvent("run_a", 1)
	bus.Publish(want)

	select {
	case got := <-subA.C:
		if got.RunID != want.RunID || got.Seq != want.Seq {
			t.Fatalf("subA: got %+v, want %+v", got, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("subA: did not receive event within 1s")
	}

	select {
	case got := <-subB.C:
		if got.RunID != want.RunID || got.Seq != want.Seq {
			t.Fatalf("subB: got %+v, want %+v", got, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("subB: did not receive event within 1s")
	}
}

func TestBus_UnsubscribeStopsDelivery(t *testing.T) {
	bus := NewBus()
	sub, unsub := bus.Subscribe()

	unsub()
	bus.Publish(busFanOutEvent("run_a", 1))

	// After unsubscribe the channel is closed and drained — no
	// pending event should remain.
	select {
	case ev, ok := <-sub.C:
		if ok {
			t.Fatalf("expected closed channel, got event %+v", ev)
		}
	default:
		// Closed channels receive zero-value immediately; the default
		// branch here would only fire if Unsubscribe hadn't closed
		// the channel, which is the failure mode we're guarding.
		t.Fatalf("expected channel to be closed after unsubscribe")
	}
}

func TestBus_DropOnFullSetsDroppedFlag(t *testing.T) {
	bus := NewBus()
	sub, unsub := bus.Subscribe()
	t.Cleanup(unsub)

	// Fill the buffer plus one to force a drop. The buffer depth is
	// an implementation detail, so loop until TakeDropped() returns
	// true rather than hard-coding it.
	for i := 0; i < busSubscriberBuffer*2; i++ {
		bus.Publish(busFanOutEvent("run_a", uint64(i+1)))
	}
	if !sub.TakeDropped() {
		t.Fatalf("expected dropped flag to be set after overflow")
	}
	// Second read clears the flag — it's a one-shot signal so the SSE
	// handler only emits a single `stream_restarted` frame per gap.
	if sub.TakeDropped() {
		t.Fatalf("expected dropped flag to clear after TakeDropped")
	}
}

func TestBus_NoSubscribersPublishIsNoOp(t *testing.T) {
	bus := NewBus()
	// Should not panic or block.
	bus.Publish(busFanOutEvent("run_a", 1))
}
