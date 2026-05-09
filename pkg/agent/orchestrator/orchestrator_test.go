package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	noopotel "go.opentelemetry.io/otel/trace/noop"

	"go.opentelemetry.io/otel/trace"

	"github.com/gridctl/gridctl/pkg/agent"
	"github.com/gridctl/gridctl/pkg/mcp"
)

func slogTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func otelNoopTracer() trace.Tracer {
	return noopotel.NewTracerProvider().Tracer("test")
}

// fakeCaller is a deterministic agent.ToolCaller used by unit tests.
// Each call increments inFlight before delegating to handle and
// decrements after, so tests can assert peak concurrency by sampling
// inFlight at the right moment.
type fakeCaller struct {
	mu       sync.Mutex
	inFlight int
	peak     int
	calls    int

	// handle returns the result for a given (name, arguments) pair.
	// Tests override it to inject specific outputs or simulated work.
	handle func(ctx context.Context, name string, arguments map[string]any) (*mcp.ToolCallResult, error)
}

func (f *fakeCaller) CallTool(ctx context.Context, name string, arguments map[string]any) (*mcp.ToolCallResult, error) {
	f.mu.Lock()
	f.inFlight++
	f.calls++
	if f.inFlight > f.peak {
		f.peak = f.inFlight
	}
	f.mu.Unlock()

	defer func() {
		f.mu.Lock()
		f.inFlight--
		f.mu.Unlock()
	}()

	if f.handle == nil {
		return &mcp.ToolCallResult{
			Content: []mcp.Content{mcp.NewTextContent(`{"ok":true}`)},
		}, nil
	}
	return f.handle(ctx, name, arguments)
}

// jsonResult builds a tool-call result whose single text block is the
// given JSON literal. Keeps test bodies short and explicit about the
// wire shape skills emit.
func jsonResult(payload string) *mcp.ToolCallResult {
	return &mcp.ToolCallResult{
		Content: []mcp.Content{mcp.NewTextContent(payload)},
	}
}

type testState struct {
	Counter int      `json:"counter"`
	Items   []string `json:"items,omitempty"`
}

type addInput struct {
	Item string `json:"item"`
}

type addOutput struct {
	Added string `json:"added"`
}

func TestNew_ReturnsConstructedOrchestrator(t *testing.T) {
	t.Parallel()
	o := New[testState](&fakeCaller{}, testState{Counter: 7})
	snap := o.Snapshot()
	if snap.Counter != 7 {
		t.Errorf("snapshot counter = %d, want 7", snap.Counter)
	}
}

func TestSnapshot_DeepCopiesState(t *testing.T) {
	t.Parallel()
	o := New[testState](&fakeCaller{}, testState{Counter: 1, Items: []string{"a"}})
	snap := o.Snapshot()
	snap.Counter = 999
	snap.Items[0] = "B"

	live := o.Snapshot()
	if live.Counter != 1 {
		t.Errorf("counter mutated through snapshot: %d", live.Counter)
	}
	if len(live.Items) != 1 || live.Items[0] != "a" {
		t.Errorf("slice mutated through snapshot: %v", live.Items)
	}
}

func TestApply_MutatesStateUnderLock(t *testing.T) {
	t.Parallel()
	o := New[testState](&fakeCaller{}, testState{})
	if err := o.Apply(func(s *testState) error {
		s.Counter = 10
		s.Items = append(s.Items, "x")
		return nil
	}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	snap := o.Snapshot()
	if snap.Counter != 10 || len(snap.Items) != 1 || snap.Items[0] != "x" {
		t.Errorf("Apply did not persist mutation: %+v", snap)
	}
}

func TestApply_RejectsNilFn(t *testing.T) {
	t.Parallel()
	o := New[testState](&fakeCaller{}, testState{})
	if err := o.Apply(nil); err == nil {
		t.Fatal("expected error for nil fn")
	}
}

func TestApply_PropagatesError(t *testing.T) {
	t.Parallel()
	o := New[testState](&fakeCaller{}, testState{})
	wantErr := errors.New("boom")
	if err := o.Apply(func(*testState) error { return wantErr }); !errors.Is(err, wantErr) {
		t.Errorf("Apply error = %v, want %v", err, wantErr)
	}
}

func TestApply_ConcurrentCallsAreSerialized(t *testing.T) {
	t.Parallel()
	o := New[testState](&fakeCaller{}, testState{})
	const N = 200
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = o.Apply(func(s *testState) error {
				s.Counter++
				return nil
			})
		}()
	}
	wg.Wait()
	if got := o.Snapshot().Counter; got != N {
		t.Errorf("counter = %d, want %d (lost updates indicate broken serialization)", got, N)
	}
}

func TestHandoff_RoundtripsTypedInputAndOutput(t *testing.T) {
	t.Parallel()
	caller := &fakeCaller{
		handle: func(_ context.Context, name string, args map[string]any) (*mcp.ToolCallResult, error) {
			if name != "add" {
				t.Errorf("called %q, want add", name)
			}
			item, _ := args["item"].(string)
			return jsonResult(fmt.Sprintf(`{"added":%q}`, "+"+item)), nil
		},
	}
	o := New[testState](caller, testState{})

	out, err := Handoff[testState, addOutput](context.Background(), o, Call{
		Skill: "add",
		Input: addInput{Item: "alpha"},
	})
	if err != nil {
		t.Fatalf("Handoff: %v", err)
	}
	if out.Added != "+alpha" {
		t.Errorf("Added = %q, want +alpha", out.Added)
	}
}

func TestHandoff_AcceptsMapInput(t *testing.T) {
	t.Parallel()
	caller := &fakeCaller{
		handle: func(_ context.Context, _ string, args map[string]any) (*mcp.ToolCallResult, error) {
			if got, _ := args["k"].(string); got != "v" {
				t.Errorf("args[k] = %v, want v", args["k"])
			}
			return jsonResult(`{"ok":true}`), nil
		},
	}
	o := New[testState](caller, testState{})
	type out struct {
		Ok bool `json:"ok"`
	}
	got, err := Handoff[testState, out](context.Background(), o, Call{
		Skill: "echo",
		Input: map[string]any{"k": "v"},
	})
	if err != nil {
		t.Fatalf("Handoff: %v", err)
	}
	if !got.Ok {
		t.Errorf("ok = %v, want true", got.Ok)
	}
}

func TestHandoff_NilInputBecomesEmptyMap(t *testing.T) {
	t.Parallel()
	caller := &fakeCaller{
		handle: func(_ context.Context, _ string, args map[string]any) (*mcp.ToolCallResult, error) {
			if args == nil {
				t.Error("args is nil; want empty map")
			}
			if len(args) != 0 {
				t.Errorf("args = %v, want empty map", args)
			}
			return jsonResult(`null`), nil
		},
	}
	o := New[testState](caller, testState{})
	type out struct{}
	if _, err := Handoff[testState, out](context.Background(), o, Call{Skill: "noop"}); err != nil {
		t.Fatalf("Handoff: %v", err)
	}
}

func TestHandoff_NilOrchestrator(t *testing.T) {
	t.Parallel()
	type out struct{}
	if _, err := Handoff[testState, out](context.Background(), nil, Call{Skill: "x"}); err == nil {
		t.Fatal("expected error for nil orchestrator")
	}
}

func TestHandoff_EmptySkillName(t *testing.T) {
	t.Parallel()
	o := New[testState](&fakeCaller{}, testState{})
	type out struct{}
	if _, err := Handoff[testState, out](context.Background(), o, Call{Skill: ""}); err == nil {
		t.Fatal("expected error for empty skill name")
	}
}

func TestHandoff_NilCaller(t *testing.T) {
	t.Parallel()
	o := New[testState](nil, testState{})
	type out struct{}
	if _, err := Handoff[testState, out](context.Background(), o, Call{Skill: "x"}); err == nil {
		t.Fatal("expected error for nil ToolCaller")
	}
}

func TestHandoff_PropagatesCallerError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("upstream failure")
	caller := &fakeCaller{
		handle: func(context.Context, string, map[string]any) (*mcp.ToolCallResult, error) {
			return nil, wantErr
		},
	}
	o := New[testState](caller, testState{})
	type out struct{}
	_, err := Handoff[testState, out](context.Background(), o, Call{Skill: "x"})
	if err == nil || !strings.Contains(err.Error(), "upstream failure") {
		t.Fatalf("err = %v, want it to wrap upstream failure", err)
	}
}

func TestHandoff_PropagatesIsError(t *testing.T) {
	t.Parallel()
	caller := &fakeCaller{
		handle: func(context.Context, string, map[string]any) (*mcp.ToolCallResult, error) {
			return &mcp.ToolCallResult{
				Content: []mcp.Content{mcp.NewTextContent("boom")},
				IsError: true,
			}, nil
		},
	}
	o := New[testState](caller, testState{})
	type out struct{}
	_, err := Handoff[testState, out](context.Background(), o, Call{Skill: "x"})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v, want it to surface skill error", err)
	}
}

func TestHandoff_UnmarshalErrorWrapsContext(t *testing.T) {
	t.Parallel()
	caller := &fakeCaller{
		handle: func(context.Context, string, map[string]any) (*mcp.ToolCallResult, error) {
			return jsonResult("not-json"), nil
		},
	}
	o := New[testState](caller, testState{})
	type out struct {
		X int `json:"x"`
	}
	_, err := Handoff[testState, out](context.Background(), o, Call{Skill: "x"})
	if err == nil || !strings.Contains(err.Error(), "decoding output") {
		t.Fatalf("err = %v, want decoding output context", err)
	}
}

func TestParallelHandoff_RunsAllCalls(t *testing.T) {
	t.Parallel()
	caller := &fakeCaller{
		handle: func(_ context.Context, _ string, args map[string]any) (*mcp.ToolCallResult, error) {
			item, _ := args["item"].(string)
			return jsonResult(fmt.Sprintf(`{"added":%q}`, "+"+item)), nil
		},
	}
	o := New[testState](caller, testState{})

	calls := []Call{
		{Skill: "add", Input: addInput{Item: "a"}},
		{Skill: "add", Input: addInput{Item: "b"}},
		{Skill: "add", Input: addInput{Item: "c"}},
	}
	results, err := ParallelHandoff[testState, addOutput](context.Background(), o, calls)
	if err != nil {
		t.Fatalf("ParallelHandoff: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("len = %d, want 3", len(results))
	}
	wantOut := []string{"+a", "+b", "+c"}
	for i, r := range results {
		if r.Index != i {
			t.Errorf("results[%d].Index = %d", i, r.Index)
		}
		if r.Err != nil {
			t.Errorf("results[%d].Err = %v", i, r.Err)
		}
		if r.Output.Added != wantOut[i] {
			t.Errorf("results[%d].Output = %q, want %q", i, r.Output.Added, wantOut[i])
		}
	}
}

func TestParallelHandoff_RespectsHardCap(t *testing.T) {
	t.Parallel()
	// Block each call until we release. Lets us measure peak concurrency.
	release := make(chan struct{})
	var started int32
	caller := &fakeCaller{
		handle: func(ctx context.Context, _ string, _ map[string]any) (*mcp.ToolCallResult, error) {
			atomic.AddInt32(&started, 1)
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return jsonResult(`{}`), nil
		},
	}
	o := New[testState](caller, testState{})
	o.SetMaxParallel(100) // request ridiculously high; expect clamp to HardMaxParallel.

	const total = 12
	calls := make([]Call, total)
	for i := range calls {
		calls[i] = Call{Skill: "x"}
	}

	type empty struct{}
	done := make(chan struct{})
	go func() {
		_, err := ParallelHandoff[testState, empty](context.Background(), o, calls)
		if err != nil {
			t.Errorf("ParallelHandoff: %v", err)
		}
		close(done)
	}()

	// Wait until peak concurrency stabilises at the cap.
	deadline := time.After(2 * time.Second)
	for {
		caller.mu.Lock()
		peak := caller.peak
		caller.mu.Unlock()
		if peak == HardMaxParallel {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("peak concurrency never reached cap; peak=%d cap=%d", peak, HardMaxParallel)
		default:
			time.Sleep(2 * time.Millisecond)
		}
	}

	caller.mu.Lock()
	peak := caller.peak
	caller.mu.Unlock()
	if peak > HardMaxParallel {
		t.Errorf("peak concurrency = %d, want <= %d", peak, HardMaxParallel)
	}

	close(release)
	<-done

	caller.mu.Lock()
	calls2 := caller.calls
	caller.mu.Unlock()
	if calls2 != total {
		t.Errorf("calls = %d, want %d", calls2, total)
	}
}

func TestParallelHandoff_ClampsZeroToDefault(t *testing.T) {
	t.Parallel()
	caller := &fakeCaller{}
	o := New[testState](caller, testState{})
	o.SetMaxParallel(0) // expect default cap.

	type empty struct{}
	calls := []Call{{Skill: "x"}, {Skill: "y"}}
	if _, err := ParallelHandoff[testState, empty](context.Background(), o, calls); err != nil {
		t.Fatalf("ParallelHandoff: %v", err)
	}
}

func TestParallelHandoff_AggregatesPerItemErrors(t *testing.T) {
	t.Parallel()
	caller := &fakeCaller{
		handle: func(_ context.Context, name string, _ map[string]any) (*mcp.ToolCallResult, error) {
			if name == "bad" {
				return nil, errors.New("planned")
			}
			return jsonResult(`{"ok":true}`), nil
		},
	}
	o := New[testState](caller, testState{})

	type out struct {
		Ok bool `json:"ok"`
	}
	calls := []Call{
		{Skill: "good"},
		{Skill: "bad"},
		{Skill: "good"},
	}
	results, err := ParallelHandoff[testState, out](context.Background(), o, calls)
	if err != nil {
		t.Fatalf("batch err = %v, want nil at batch level", err)
	}
	if results[0].Err != nil || !results[0].Output.Ok {
		t.Errorf("results[0] = %+v, want ok=true err=nil", results[0])
	}
	if results[1].Err == nil {
		t.Errorf("results[1].Err = nil, want planned failure")
	}
	if results[2].Err != nil || !results[2].Output.Ok {
		t.Errorf("results[2] = %+v, want ok=true err=nil", results[2])
	}
}

func TestParallelHandoff_NilOrchestrator(t *testing.T) {
	t.Parallel()
	type out struct{}
	if _, err := ParallelHandoff[testState, out](context.Background(), nil, []Call{{Skill: "x"}}); err == nil {
		t.Fatal("expected error for nil orchestrator")
	}
}

func TestParallelHandoff_EmptyCalls(t *testing.T) {
	t.Parallel()
	o := New[testState](&fakeCaller{}, testState{})
	type out struct{}
	results, err := ParallelHandoff[testState, out](context.Background(), o, nil)
	if err != nil {
		t.Fatalf("ParallelHandoff: %v", err)
	}
	if results != nil {
		t.Errorf("results = %v, want nil", results)
	}
}

func TestParallelHandoff_ContextCancellation(t *testing.T) {
	t.Parallel()
	block := make(chan struct{})
	caller := &fakeCaller{
		handle: func(ctx context.Context, _ string, _ map[string]any) (*mcp.ToolCallResult, error) {
			select {
			case <-block:
				return jsonResult(`{}`), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}
	o := New[testState](caller, testState{})

	ctx, cancel := context.WithCancel(context.Background())
	type empty struct{}
	calls := make([]Call, 8)
	for i := range calls {
		calls[i] = Call{Skill: "x"}
	}

	done := make(chan struct{})
	var results []Result[empty]
	go func() {
		results, _ = ParallelHandoff[testState, empty](ctx, o, calls)
		close(done)
	}()

	// Give some handoffs time to enter the caller, then cancel.
	time.Sleep(20 * time.Millisecond)
	cancel()
	close(block) // release any blocked goroutines so the test cleans up
	<-done

	cancelled := 0
	for _, r := range results {
		if r.Err != nil {
			cancelled++
		}
	}
	if cancelled == 0 {
		t.Errorf("expected at least one cancelled handoff, got results=%+v", results)
	}
}

func TestSetMaxParallel_LowersCapBelowDefault(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	caller := &fakeCaller{
		handle: func(ctx context.Context, _ string, _ map[string]any) (*mcp.ToolCallResult, error) {
			select {
			case <-release:
				return jsonResult(`{}`), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}
	o := New[testState](caller, testState{})
	o.SetMaxParallel(2)

	type empty struct{}
	calls := make([]Call, 6)
	for i := range calls {
		calls[i] = Call{Skill: "x"}
	}
	go func() {
		_, _ = ParallelHandoff[testState, empty](context.Background(), o, calls)
	}()

	deadline := time.After(2 * time.Second)
	for {
		caller.mu.Lock()
		peak := caller.peak
		caller.mu.Unlock()
		if peak == 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("peak never reached 2; peak=%d", peak)
		default:
			time.Sleep(2 * time.Millisecond)
		}
	}

	caller.mu.Lock()
	peak := caller.peak
	caller.mu.Unlock()
	if peak != 2 {
		t.Errorf("peak = %d, want 2 (caller-set lower cap)", peak)
	}
	close(release)
}

func TestSingleWriterFlow_E2E(t *testing.T) {
	t.Parallel()
	// Two parallel skills produce per-item outputs; the orchestrator
	// merges them back into State via Apply. Verifies the full flow
	// the prompt asks for: snapshot → derive inputs → ParallelHandoff
	// → merge. Run with -race to catch any single-writer regression.
	caller := &fakeCaller{
		handle: func(_ context.Context, name string, args map[string]any) (*mcp.ToolCallResult, error) {
			item, _ := args["item"].(string)
			switch name {
			case "double":
				return jsonResult(fmt.Sprintf(`{"added":%q}`, item+item)), nil
			case "shout":
				return jsonResult(fmt.Sprintf(`{"added":%q}`, strings.ToUpper(item))), nil
			default:
				return nil, fmt.Errorf("unknown skill %q", name)
			}
		},
	}
	o := New[testState](caller, testState{Items: []string{"alpha", "beta"}})

	snap := o.Snapshot()
	calls := make([]Call, 0, len(snap.Items)*2)
	for _, item := range snap.Items {
		calls = append(calls,
			Call{Skill: "double", Input: addInput{Item: item}},
			Call{Skill: "shout", Input: addInput{Item: item}},
		)
	}

	results, err := ParallelHandoff[testState, addOutput](context.Background(), o, calls)
	if err != nil {
		t.Fatalf("ParallelHandoff: %v", err)
	}

	if err := o.Apply(func(s *testState) error {
		for _, r := range results {
			if r.Err != nil {
				return r.Err
			}
			s.Items = append(s.Items, r.Output.Added)
			s.Counter++
		}
		return nil
	}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	final := o.Snapshot()
	if final.Counter != 4 {
		t.Errorf("counter = %d, want 4", final.Counter)
	}
	wantSuffix := []string{"alphaalpha", "ALPHA", "betabeta", "BETA"}
	for _, want := range wantSuffix {
		found := false
		for _, item := range final.Items {
			if item == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("final items missing %q: %v", want, final.Items)
		}
	}

	caller.mu.Lock()
	peak := caller.peak
	caller.mu.Unlock()
	if peak > HardMaxParallel {
		t.Errorf("peak exceeded cap: %d > %d", peak, HardMaxParallel)
	}
}

// noopCaller is the smallest possible agent.ToolCaller — it never
// returns a result. Used to keep fixture wiring compact in setter tests.
type noopCaller struct{}

func (noopCaller) CallTool(_ context.Context, _ string, _ map[string]any) (*agent.ToolCallResult, error) {
	return jsonResult(`{}`), nil
}

func TestSetters_AcceptNilWithoutPanic(t *testing.T) {
	t.Parallel()
	o := New[testState](noopCaller{}, testState{})
	o.SetLogger(nil)
	o.SetTracer(nil)
	o.SetMaxParallel(0)
	type empty struct{}
	if _, err := Handoff[testState, empty](context.Background(), o, Call{Skill: "x"}); err != nil {
		t.Fatalf("Handoff after nil setters: %v", err)
	}
}

func TestSetLogger_AppliesComponentAttribute(t *testing.T) {
	t.Parallel()
	// Use a real slog.Logger to confirm SetLogger doesn't panic and
	// produces a logger ParallelHandoff can write through.
	o := New[testState](noopCaller{}, testState{})
	o.SetLogger(slogTestLogger())
	o.SetMaxParallel(100) // triggers the clamp warning path on next ParallelHandoff
	type empty struct{}
	if _, err := ParallelHandoff[testState, empty](context.Background(), o, []Call{{Skill: "x"}}); err != nil {
		t.Fatalf("ParallelHandoff: %v", err)
	}
}

func TestSetTracer_OverridesDefault(t *testing.T) {
	t.Parallel()
	o := New[testState](noopCaller{}, testState{})
	o.SetTracer(otelNoopTracer())
	type empty struct{}
	if _, err := Handoff[testState, empty](context.Background(), o, Call{Skill: "x"}); err != nil {
		t.Fatalf("Handoff after SetTracer: %v", err)
	}
}

func TestSnapshot_HandlesUnmarshalableState(t *testing.T) {
	t.Parallel()
	// chan fields are not JSON-marshalable; jsonCopy fails so Snapshot
	// returns the zero value and logs an error rather than panicking.
	type weird struct {
		Ch chan int `json:"-"`
		// This struct still marshals (chan ignored). Use a more direct
		// trigger: a value json.Marshal rejects.
		Bad func() `json:",omitempty"`
	}
	w := weird{Bad: func() {}}
	// Confirm json round-trip really does fail for this shape.
	if _, err := jsonCopy(w); err == nil {
		t.Skip("jsonCopy unexpectedly succeeded; weird-state probe is no longer a round-trip failure")
	}
	o := New[weird](noopCaller{}, w)
	got := o.Snapshot()
	if got.Bad != nil {
		t.Errorf("Snapshot returned non-zero on jsonCopy failure: %+v", got)
	}
}

func TestToArguments_Variants(t *testing.T) {
	t.Parallel()
	// nil → empty map.
	got, err := toArguments(nil)
	if err != nil || len(got) != 0 {
		t.Errorf("nil: got %v, err %v", got, err)
	}
	// nil-typed map → empty map (defensive against the nil-map-typed-as-any case).
	var nilMap map[string]any
	got, err = toArguments(nilMap)
	if err != nil || len(got) != 0 {
		t.Errorf("nil map: got %v, err %v", got, err)
	}
	// non-marshalable input bubbles up.
	if _, err := toArguments(func() {}); err == nil {
		t.Error("expected error for non-marshalable input")
	}
}
