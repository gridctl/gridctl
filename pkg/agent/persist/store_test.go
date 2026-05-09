package persist

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestNewRunIDIsUnique(t *testing.T) {
	t.Parallel()
	seen := make(map[string]struct{}, 1024)
	for i := 0; i < 1024; i++ {
		id := NewRunID()
		if !strings.HasPrefix(id, "run_") {
			t.Fatalf("expected run_ prefix, got %q", id)
		}
		if _, ok := seen[id]; ok {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = struct{}{}
	}
}

func TestStoreRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := NewStore(dir)
	defer store.CloseAll() //nolint:errcheck // best-effort

	runID := NewRunID()
	rec, err := store.OpenWriter(runID)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}

	if _, err := rec.Record(EventRunStarted, RunStartedPayload{Skill: "hello", Flavor: "ts"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if _, err := rec.Record(EventNodeEnter, NodeEnterPayload{NodeID: "n1", NodeName: "first"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if _, err := rec.Record(EventNodeExit, NodeExitPayload{NodeID: "n1", DurationMicros: 1234, Success: true}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if _, err := rec.Record(EventRunCompleted, RunCompletedPayload{Status: "ok"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	events, err := store.Read(runID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d", len(events))
	}
	if events[0].Seq != 1 || events[3].Seq != 4 {
		t.Fatalf("unexpected sequence numbers: %+v", events)
	}
	if events[0].Type != EventRunStarted {
		t.Fatalf("expected RunStarted first, got %s", events[0].Type)
	}

	// File permissions: 0600.
	info, err := os.Stat(store.PathFor(runID))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected mode 0600, got %o", info.Mode().Perm())
	}
}

func TestStoreSummary(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := NewStore(dir)
	runID := NewRunID()
	rec, err := store.OpenWriter(runID)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	if _, err := rec.Record(EventRunStarted, RunStartedPayload{Skill: "skill-x", Flavor: "go", TraceID: "abc"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if _, err := rec.Record(EventApprovalRequest, ApprovalRequestPayload{ApprovalID: "ap-1", Prompt: "ok?"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	sum, err := store.Summary(runID)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if sum.Skill != "skill-x" || sum.Flavor != "go" || sum.TraceID != "abc" {
		t.Fatalf("summary header missing: %+v", sum)
	}
	if sum.Status != "awaiting_approval" {
		t.Fatalf("expected status=awaiting_approval, got %s", sum.Status)
	}
	if sum.PendingApproval != "ap-1" {
		t.Fatalf("expected pending approval id ap-1, got %q", sum.PendingApproval)
	}
}

func TestStoreList(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := NewStore(dir)

	for i := 0; i < 3; i++ {
		runID := NewRunID()
		rec, err := store.OpenWriter(runID)
		if err != nil {
			t.Fatalf("OpenWriter: %v", err)
		}
		if _, err := rec.Record(EventRunStarted, RunStartedPayload{Skill: "s"}); err != nil {
			t.Fatalf("Record: %v", err)
		}
		if err := rec.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}
	// A non-jsonl file in the runs dir must be ignored.
	if err := os.WriteFile(filepath.Join(dir, "stray.txt"), []byte("ignore me"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	summaries, err := store.List(0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(summaries) != 3 {
		t.Fatalf("expected 3 runs, got %d", len(summaries))
	}
}

func TestRecorderTrailingPartialLineIgnored(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := NewStore(dir)
	runID := NewRunID()
	rec, err := store.OpenWriter(runID)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	if _, err := rec.Record(EventRunStarted, RunStartedPayload{Skill: "x"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Append a partial line as a crash mid-write would leave behind.
	f, err := os.OpenFile(store.PathFor(runID), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if _, err := f.WriteString(`{"run_id":"x","seq":2,"type":"node_enter"`); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	events, err := store.Read(runID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event after partial-line append, got %d", len(events))
	}
}

func TestRecorderMonotonicSeqAcrossReopens(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := NewStore(dir)
	runID := NewRunID()
	rec, err := store.OpenWriter(runID)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	if _, err := rec.Record(EventRunStarted, RunStartedPayload{Skill: "x"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if _, err := rec.Record(EventNodeEnter, NodeEnterPayload{NodeID: "n1"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopen — the new recorder should pick up at seq=3.
	rec2, err := store.OpenWriter(runID)
	if err != nil {
		t.Fatalf("OpenWriter (reopen): %v", err)
	}
	defer rec2.Close() //nolint:errcheck // best-effort
	ev, err := rec2.Record(EventNodeExit, NodeExitPayload{NodeID: "n1", DurationMicros: 1, Success: true})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if ev.Seq != 3 {
		t.Fatalf("expected seq=3 after reopen, got %d", ev.Seq)
	}
}

func TestRecorderConcurrentWrites(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := NewStore(dir)
	runID := NewRunID()
	rec, err := store.OpenWriter(runID)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	defer rec.Close() //nolint:errcheck // best-effort

	const writers = 8
	const perWriter = 64
	var wg sync.WaitGroup
	wg.Add(writers)
	for i := 0; i < writers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < perWriter; j++ {
				if _, err := rec.Record(EventNodeEnter, NodeEnterPayload{NodeID: "n"}); err != nil {
					t.Errorf("Record: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()

	events, err := store.Read(runID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(events) != writers*perWriter {
		t.Fatalf("expected %d events, got %d", writers*perWriter, len(events))
	}
	// Every Seq must be unique and within the expected range.
	seen := make(map[uint64]struct{}, len(events))
	for _, ev := range events {
		if ev.Seq < 1 || ev.Seq > uint64(writers*perWriter) {
			t.Fatalf("seq out of range: %d", ev.Seq)
		}
		if _, dup := seen[ev.Seq]; dup {
			t.Fatalf("duplicate seq %d", ev.Seq)
		}
		seen[ev.Seq] = struct{}{}
	}
}

func TestStoreStream(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := NewStore(dir)
	runID := NewRunID()
	rec, err := store.OpenWriter(runID)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	for i := 0; i < 5; i++ {
		if _, err := rec.Record(EventNodeEnter, NodeEnterPayload{NodeID: "n"}); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	count := 0
	err = store.Stream(context.Background(), runID, func(ev Event) error {
		count++
		if ev.Type != EventNodeEnter {
			t.Fatalf("expected node_enter, got %s", ev.Type)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if count != 5 {
		t.Fatalf("expected 5 streamed events, got %d", count)
	}

	// Early-stop on consumer error.
	stopErr := errors.New("stop")
	err = store.Stream(context.Background(), runID, func(ev Event) error {
		if ev.Seq == 2 {
			return stopErr
		}
		return nil
	})
	if !errors.Is(err, stopErr) {
		t.Fatalf("expected stopErr, got %v", err)
	}
}

func TestMarshalEventEmbedsPayload(t *testing.T) {
	t.Parallel()
	ev, err := MarshalEvent("run_x", 7, EventToolCall, ToolCallPayload{CallID: "c1", Name: "srv__t"})
	if err != nil {
		t.Fatalf("MarshalEvent: %v", err)
	}
	if ev.Seq != 7 || ev.RunID != "run_x" || ev.Type != EventToolCall {
		t.Fatalf("envelope mismatch: %+v", ev)
	}
	var p ToolCallPayload
	if err := json.Unmarshal(ev.Payload, &p); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if p.CallID != "c1" || p.Name != "srv__t" {
		t.Fatalf("payload mismatch: %+v", p)
	}
}
