package compose

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/agent/persist"
)

func openRecorder(t *testing.T) (*persist.Store, *persist.Recorder, string) {
	t.Helper()
	dir := t.TempDir()
	store := persist.NewStore(dir)
	id := persist.NewRunID()
	rec, err := store.OpenWriter(id)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	return store, rec, id
}

func TestGateApproveResolves(t *testing.T) {
	t.Parallel()
	store, rec, runID := openRecorder(t)
	defer store.CloseAll() //nolint:errcheck // best-effort

	registry := NewRegistry()
	gate, err := NewGate(runID, rec, registry, &SlogNotifier{}, WithTimeout(2*time.Second))
	if err != nil {
		t.Fatalf("NewGate: %v", err)
	}

	approvalID := make(chan string, 1)
	go func() {
		// Loop briefly until the registry sees the pending request.
		deadline := time.Now().Add(1 * time.Second)
		for time.Now().Before(deadline) {
			if list := registry.List(); len(list) == 1 {
				approvalID <- list[0].ID
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		approvalID <- ""
	}()

	go func() {
		id := <-approvalID
		if id == "" {
			return
		}
		if err := registry.Resolve(id, true, "looks good", "cli"); err != nil {
			t.Errorf("Resolve: %v", err)
		}
	}()

	dec, err := gate.Approve(context.Background(), "ship it?")
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if !dec.Approved || dec.Reason != "looks good" {
		t.Fatalf("unexpected decision: %+v", dec)
	}

	if err := rec.Close(); err != nil {
		t.Fatalf("rec.Close: %v", err)
	}
	events, err := store.Read(runID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Type != persist.EventApprovalRequest {
		t.Fatalf("expected approval_request first, got %s", events[0].Type)
	}
	if events[1].Type != persist.EventApprovalResponse {
		t.Fatalf("expected approval_response second, got %s", events[1].Type)
	}
}

func TestGateApproveTimesOut(t *testing.T) {
	t.Parallel()
	store, rec, runID := openRecorder(t)
	defer store.CloseAll() //nolint:errcheck // best-effort

	registry := NewRegistry()
	gate, err := NewGate(runID, rec, registry, &SlogNotifier{}, WithTimeout(50*time.Millisecond), WithWarnFraction(0.5))
	if err != nil {
		t.Fatalf("NewGate: %v", err)
	}

	dec, err := gate.Approve(context.Background(), "ship?")
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if dec.Approved {
		t.Fatalf("expected timeout to reject, got approved")
	}
	if dec.Reason != "approval window elapsed" {
		t.Fatalf("expected timeout reason, got %q", dec.Reason)
	}
}

func TestGateApproveCancelled(t *testing.T) {
	t.Parallel()
	store, rec, runID := openRecorder(t)
	defer store.CloseAll() //nolint:errcheck // best-effort

	registry := NewRegistry()
	gate, err := NewGate(runID, rec, registry, &SlogNotifier{}, WithTimeout(5*time.Second))
	if err != nil {
		t.Fatalf("NewGate: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	_, err = gate.Approve(ctx, "ship?")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestRegistryResolveTwice(t *testing.T) {
	t.Parallel()
	store, rec, runID := openRecorder(t)
	defer store.CloseAll() //nolint:errcheck // best-effort

	registry := NewRegistry()
	gate, err := NewGate(runID, rec, registry, &SlogNotifier{}, WithTimeout(2*time.Second))
	if err != nil {
		t.Fatalf("NewGate: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = gate.Approve(context.Background(), "ship?")
	}()

	// Wait for the gate to register.
	deadline := time.Now().Add(time.Second)
	var id string
	for time.Now().Before(deadline) {
		if list := registry.List(); len(list) == 1 {
			id = list[0].ID
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if id == "" {
		t.Fatal("registry never saw the pending approval")
	}
	if err := registry.Resolve(id, true, "ok", "cli"); err != nil {
		t.Fatalf("first Resolve: %v", err)
	}
	if err := registry.Resolve(id, false, "no", "cli"); !errors.Is(err, ErrApprovalNotFound) && !errors.Is(err, ErrAlreadyResolved) {
		t.Fatalf("expected resolved/not-found on second Resolve, got %v", err)
	}
	wg.Wait()
}

func TestRegistryLookupAndList(t *testing.T) {
	t.Parallel()
	store, rec, runID := openRecorder(t)
	defer store.CloseAll() //nolint:errcheck // best-effort

	registry := NewRegistry()
	gate, err := NewGate(runID, rec, registry, &SlogNotifier{}, WithTimeout(2*time.Second), WithSkill("hello"))
	if err != nil {
		t.Fatalf("NewGate: %v", err)
	}

	approveDone := make(chan struct{})
	go func() {
		_, _ = gate.Approve(context.Background(), "ship?")
		close(approveDone)
	}()

	// Wait for registration.
	var id string
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if list := registry.List(); len(list) == 1 {
			id = list[0].ID
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if id == "" {
		t.Fatal("registry never saw the pending approval")
	}

	got, ok := registry.Lookup(id)
	if !ok {
		t.Fatal("Lookup returned false")
	}
	if got.RunID != runID || got.Skill != "hello" || got.Prompt != "ship?" {
		t.Fatalf("Lookup returned wrong data: %+v", got)
	}

	if err := registry.Resolve(id, false, "", "cli"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	<-approveDone
}

func TestSlogNotifierIsNoOpOnNilLogger(t *testing.T) {
	t.Parallel()
	n := &SlogNotifier{}
	err := n.NotifyApproval(context.Background(), ApprovalRequest{
		RunID:      "r",
		ApprovalID: "ap_x",
		Prompt:     "ok?",
		Timeout:    time.Hour,
	})
	if err != nil {
		t.Fatalf("NotifyApproval: %v", err)
	}
}

type collectNotifier struct {
	mu       sync.Mutex
	requests []ApprovalRequest
	err      error
}

func (n *collectNotifier) NotifyApproval(_ context.Context, req ApprovalRequest) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.err != nil {
		return n.err
	}
	n.requests = append(n.requests, req)
	return nil
}

func TestMultiNotifierFansOut(t *testing.T) {
	t.Parallel()
	a := &collectNotifier{}
	b := &collectNotifier{}
	multi := &MultiNotifier{Notifiers: []Notifier{a, b}}
	err := multi.NotifyApproval(context.Background(), ApprovalRequest{ApprovalID: "ap_1"})
	if err != nil {
		t.Fatalf("NotifyApproval: %v", err)
	}
	if len(a.requests) != 1 || len(b.requests) != 1 {
		t.Fatalf("expected each notifier to see 1 request, got a=%d b=%d", len(a.requests), len(b.requests))
	}
}

func TestMultiNotifierStopsOnError(t *testing.T) {
	t.Parallel()
	a := &collectNotifier{err: errors.New("boom")}
	b := &collectNotifier{}
	multi := &MultiNotifier{Notifiers: []Notifier{a, b}}
	err := multi.NotifyApproval(context.Background(), ApprovalRequest{ApprovalID: "ap_1"})
	if err == nil {
		t.Fatal("expected error from MultiNotifier")
	}
	if len(b.requests) != 0 {
		t.Fatal("expected fan-out to stop after first error")
	}
}

func TestNewGateRejectsMissingDependencies(t *testing.T) {
	t.Parallel()
	if _, err := NewGate("", nil, nil, nil); err == nil {
		t.Fatal("expected error when run id is empty")
	}
	if _, err := NewGate("r", nil, NewRegistry(), nil); err == nil {
		t.Fatal("expected error when recorder is nil")
	}
	dir := t.TempDir()
	store := persist.NewStore(dir)
	id := persist.NewRunID()
	rec, err := store.OpenWriter(id)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	defer rec.Close() //nolint:errcheck // best-effort
	if _, err := NewGate(id, rec, nil, nil); err == nil {
		t.Fatal("expected error when registry is nil")
	}
}
