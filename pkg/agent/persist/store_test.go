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

// TestEventVocabularyRoundTrip drives every event type the closed
// contract in events.go exposes (except EventLLMChunk, intentionally
// deferred — streaming chunk emission lands in a follow-on) through
// the same Record → Read → typed-decode cycle. Adding a new event
// type to the vocabulary should fail this test until the new payload
// shape is exercised here too; the table is the single place to
// extend.
func TestEventVocabularyRoundTrip(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		evType  EventType
		payload any
		verify  func(t *testing.T, ev Event)
	}{
		{
			name:    "run_started",
			evType:  EventRunStarted,
			payload: RunStartedPayload{Skill: "demo", Flavor: "ts", ParentRunID: "run_parent", TraceID: "trace_x"},
			verify: func(t *testing.T, ev Event) {
				var p RunStartedPayload
				if err := json.Unmarshal(ev.Payload, &p); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if p.ParentRunID != "run_parent" || p.Flavor != "ts" {
					t.Fatalf("payload mismatch: %+v", p)
				}
			},
		},
		{
			name:    "run_completed",
			evType:  EventRunCompleted,
			payload: RunCompletedPayload{Status: "ok", Output: json.RawMessage(`{"answer":42}`)},
			verify: func(t *testing.T, ev Event) {
				var p RunCompletedPayload
				if err := json.Unmarshal(ev.Payload, &p); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if p.Status != "ok" || string(p.Output) != `{"answer":42}` {
					t.Fatalf("payload mismatch: %+v", p)
				}
			},
		},
		{
			name:    "node_enter",
			evType:  EventNodeEnter,
			payload: NodeEnterPayload{NodeID: "tool:srv__t#1", NodeName: "tool", SpanID: "span_a"},
			verify: func(t *testing.T, ev Event) {
				var p NodeEnterPayload
				if err := json.Unmarshal(ev.Payload, &p); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if p.NodeID != "tool:srv__t#1" || p.NodeName != "tool" {
					t.Fatalf("payload mismatch: %+v", p)
				}
			},
		},
		{
			name:    "node_exit",
			evType:  EventNodeExit,
			payload: NodeExitPayload{NodeID: "tool:srv__t#1", DurationMicros: 4321, Success: true},
			verify: func(t *testing.T, ev Event) {
				var p NodeExitPayload
				if err := json.Unmarshal(ev.Payload, &p); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if p.DurationMicros != 4321 || !p.Success {
					t.Fatalf("payload mismatch: %+v", p)
				}
			},
		},
		{
			name:   "tool_call",
			evType: EventToolCall,
			payload: ToolCallPayload{
				CallID:    "call_1",
				NodeID:    "tool:srv__t#1",
				Name:      "srv__t",
				Arguments: json.RawMessage(`{"k":"v"}`),
			},
			verify: func(t *testing.T, ev Event) {
				var p ToolCallPayload
				if err := json.Unmarshal(ev.Payload, &p); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if p.NodeID != "tool:srv__t#1" || string(p.Arguments) != `{"k":"v"}` {
					t.Fatalf("payload mismatch: %+v", p)
				}
			},
		},
		{
			name:   "tool_result",
			evType: EventToolResult,
			payload: ToolResultPayload{
				CallID: "call_1",
				NodeID: "tool:srv__t#1",
				Output: "ok",
			},
			verify: func(t *testing.T, ev Event) {
				var p ToolResultPayload
				if err := json.Unmarshal(ev.Payload, &p); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if p.Output != "ok" || p.IsError {
					t.Fatalf("payload mismatch: %+v", p)
				}
			},
		},
		{
			name:   "llm_call",
			evType: EventLLMCall,
			payload: LLMCallPayload{
				Model:        "claude-opus-4-7",
				Provider:     "anthropic",
				PromptTokens: 12,
				OutputTokens: 34,
				CostUSD:      0.0125,
			},
			verify: func(t *testing.T, ev Event) {
				var p LLMCallPayload
				if err := json.Unmarshal(ev.Payload, &p); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if p.PromptTokens != 12 || p.OutputTokens != 34 || p.CostUSD != 0.0125 {
					t.Fatalf("payload mismatch: %+v", p)
				}
			},
		},
		{
			name:    "structured_output",
			evType:  EventStructuredOutput,
			payload: StructuredOutputPayload{SchemaDigest: "sha256:abc", Output: json.RawMessage(`{"ok":true}`)},
			verify: func(t *testing.T, ev Event) {
				var p StructuredOutputPayload
				if err := json.Unmarshal(ev.Payload, &p); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if p.SchemaDigest != "sha256:abc" || string(p.Output) != `{"ok":true}` {
					t.Fatalf("payload mismatch: %+v", p)
				}
			},
		},
		{
			name:    "approval_request",
			evType:  EventApprovalRequest,
			payload: ApprovalRequestPayload{ApprovalID: "ap_1", Prompt: "ok?", TimeoutSeconds: 60},
			verify: func(t *testing.T, ev Event) {
				var p ApprovalRequestPayload
				if err := json.Unmarshal(ev.Payload, &p); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if p.ApprovalID != "ap_1" || p.TimeoutSeconds != 60 {
					t.Fatalf("payload mismatch: %+v", p)
				}
			},
		},
		{
			name:    "approval_response",
			evType:  EventApprovalResponse,
			payload: ApprovalResponsePayload{ApprovalID: "ap_1", Approved: true, Reason: "ship it", Source: "web"},
			verify: func(t *testing.T, ev Event) {
				var p ApprovalResponsePayload
				if err := json.Unmarshal(ev.Payload, &p); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if !p.Approved || p.Source != "web" {
					t.Fatalf("payload mismatch: %+v", p)
				}
			},
		},
		{
			name:    "error",
			evType:  EventError,
			payload: ErrorPayload{Message: "boom", NodeID: "tool:srv__t#1"},
			verify: func(t *testing.T, ev Event) {
				var p ErrorPayload
				if err := json.Unmarshal(ev.Payload, &p); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if p.Message != "boom" || p.NodeID != "tool:srv__t#1" {
					t.Fatalf("payload mismatch: %+v", p)
				}
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			store := NewStore(dir)
			runID := NewRunID()
			rec, err := store.OpenWriter(runID)
			if err != nil {
				t.Fatalf("OpenWriter: %v", err)
			}
			if _, err := rec.Record(tc.evType, tc.payload); err != nil {
				t.Fatalf("Record: %v", err)
			}
			if err := rec.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}
			events, err := store.Read(runID)
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			if len(events) != 1 {
				t.Fatalf("expected 1 event, got %d", len(events))
			}
			if events[0].Type != tc.evType {
				t.Fatalf("event type = %s, want %s", events[0].Type, tc.evType)
			}
			tc.verify(t, events[0])
		})
	}
}

// TestCapRawJSONRespectsBudget locks in the size-cap contract for
// verbatim payload fields. The cap protects the ledger from authors
// feeding multi-megabyte blobs into a tool call; under-budget values
// pass through unchanged so the common path is lossless.
func TestCapRawJSONRespectsBudget(t *testing.T) {
	t.Parallel()
	small := []byte(`{"a":1}`)
	got, truncated := CapRawJSON(small)
	if truncated {
		t.Fatalf("under-budget value flagged as truncated")
	}
	if string(got) != string(small) {
		t.Fatalf("under-budget value mutated: got %s, want %s", got, small)
	}

	big := make([]byte, MaxPayloadBytes+1024)
	for i := range big {
		big[i] = 'a'
	}
	got, truncated = CapRawJSON(big)
	if !truncated {
		t.Fatalf("over-budget value not flagged as truncated")
	}
	// The returned value must still be valid JSON so readers don't
	// choke; the cap helper wraps the prefix as a JSON string.
	var probe any
	if err := json.Unmarshal(got, &probe); err != nil {
		t.Fatalf("capped value is not valid JSON: %v", err)
	}
	if s, ok := probe.(string); !ok || len(s) != MaxPayloadBytes {
		t.Fatalf("capped string length = %d, want %d (probe=%T)", len(s), MaxPayloadBytes, probe)
	}
}

// TestCapStringRespectsBudget mirrors TestCapRawJSONRespectsBudget for
// plain-text fields (e.g. ToolResultPayload.Output).
func TestCapStringRespectsBudget(t *testing.T) {
	t.Parallel()
	got, truncated := CapString("hello")
	if truncated || got != "hello" {
		t.Fatalf("under-budget string mutated: got %q, truncated=%v", got, truncated)
	}
	big := strings.Repeat("x", MaxPayloadBytes+512)
	got, truncated = CapString(big)
	if !truncated {
		t.Fatalf("over-budget string not flagged as truncated")
	}
	if len(got) != MaxPayloadBytes {
		t.Fatalf("capped length = %d, want %d", len(got), MaxPayloadBytes)
	}
}
