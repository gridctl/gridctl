// Package orchestrator is the gridctl single-writer multi-agent
// primitive. An Orchestrator owns a typed State; subagents are invoked
// via Handoff and ParallelHandoff and never receive a pointer to that
// State. The orchestrator is the only thing that can write — subagents
// see typed inputs the orchestrator derives from Snapshot, and return
// typed outputs the orchestrator merges back via Apply.
//
// Single-writer enforcement is structural, not advisory: there is no
// path for a subagent to reach into the orchestrator's State because
// the orchestrator never hands out a pointer. Apply serialises mutations
// behind a mutex so concurrent merges from a parallel batch land in a
// well-defined order.
//
// Subagents are dispatched through agent.ToolCaller — the same surface
// the runtime invokes any MCP tool through. A *skill.Registry satisfies
// the interface directly (typed Go skills); pkg/agent/gateway adapts a
// *mcp.Gateway so the orchestrator can call any tool the gateway
// exposes (TS skills via the registry's TS dispatcher, downstream MCP
// servers, or another gridctl instance pointed at this one). One code
// path covers local and remote handoff alike.
//
// Parallel handoffs are capped at HardMaxParallel = 4. SetMaxParallel
// can lower the cap (useful for tests and rate-limited providers) but
// requests above the ceiling are clamped with a warning rather than
// rejected — the cap is the orchestrator's invariant, not the caller's
// preference.
package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/gridctl/gridctl/pkg/agent"
	"github.com/gridctl/gridctl/pkg/logging"
)

const (
	// DefaultMaxParallel is the orchestrator's default concurrency for
	// ParallelHandoff. Mirrors sandbox.DefaultMaxParallel so a TS
	// skill's parallel() and a Go orchestrator's ParallelHandoff observe
	// the same hard cap.
	DefaultMaxParallel = 4

	// HardMaxParallel is the ceiling SetMaxParallel cannot exceed.
	// Requests above the ceiling are clamped with a warning. The cap
	// exists because handoffs trigger LLM calls and tool invocations
	// that are individually expensive; "let the user fan out as wide
	// as they want" is not a safe default for an orchestrator that
	// composes onto third-party APIs.
	HardMaxParallel = 4

	tracerName = "gridctl.agent.orchestrator"
)

// Orchestrator owns a State that only the orchestrator's caller can
// mutate. State is read out via Snapshot (deep-copied) and updated via
// Apply (mutex-serialised). Handoff and ParallelHandoff invoke
// subagents through the configured agent.ToolCaller; subagents receive
// only the typed Input the caller provides — never a pointer or
// reference to State.
//
// The zero value is not usable. Construct via New.
type Orchestrator[State any] struct {
	caller agent.ToolCaller
	logger *slog.Logger
	tracer trace.Tracer

	cfgMu  sync.RWMutex
	maxPar int

	mu    sync.Mutex
	state State
}

// New returns an Orchestrator initialised with the given caller and
// initial state. The caller can be nil; calls to Handoff and
// ParallelHandoff return an error in that case rather than panicking,
// matching the Article V (no panic in library code) discipline.
//
// State is taken by value: the orchestrator owns its copy from the
// constructor call onward.
func New[State any](caller agent.ToolCaller, initial State) *Orchestrator[State] {
	return &Orchestrator[State]{
		caller: caller,
		logger: logging.NewDiscardLogger(),
		tracer: otel.Tracer(tracerName),
		maxPar: DefaultMaxParallel,
		state:  initial,
	}
}

// SetLogger replaces the orchestrator's slog.Logger. The provided
// logger is wrapped with a "component=agent-orchestrator" attribute so
// downstream filters can match on it. nil is a no-op.
func (o *Orchestrator[State]) SetLogger(logger *slog.Logger) {
	if logger == nil {
		return
	}
	o.cfgMu.Lock()
	defer o.cfgMu.Unlock()
	o.logger = logging.WithComponent(logger, "agent-orchestrator")
}

// SetTracer replaces the orchestrator's trace.Tracer. Use this when
// wiring the orchestrator under a non-default tracer provider (tests
// most often). nil is a no-op.
func (o *Orchestrator[State]) SetTracer(tracer trace.Tracer) {
	if tracer == nil {
		return
	}
	o.cfgMu.Lock()
	defer o.cfgMu.Unlock()
	o.tracer = tracer
}

// SetMaxParallel sets the parallel-handoff concurrency cap. Values <= 0
// are treated as "use DefaultMaxParallel"; values above HardMaxParallel
// are clamped at the ceiling with a warning emitted on the next
// ParallelHandoff. The cap is read at ParallelHandoff start; in-flight
// batches are not retroactively rebalanced.
func (o *Orchestrator[State]) SetMaxParallel(n int) {
	o.cfgMu.Lock()
	defer o.cfgMu.Unlock()
	o.maxPar = n
}

// Snapshot returns a deep copy of the current State. The copy is
// produced by JSON round-trip; State types with unexported fields,
// channels, or function values must marshal cleanly for the copy to be
// faithful — the orchestrator deliberately rejects sharing pointers
// rather than supporting opaque-by-design types. If JSON round-trip
// fails (which only happens for non-marshalable State types), Snapshot
// returns the zero value of State and logs the error; callers should
// treat State as a JSON-marshalable value object.
func (o *Orchestrator[State]) Snapshot() State {
	o.mu.Lock()
	state := o.state
	o.mu.Unlock()

	out, err := jsonCopy(state)
	if err != nil {
		o.getLogger().Error("snapshot: state is not JSON-marshalable; returning zero value",
			"error", err)
		var zero State
		return zero
	}
	return out
}

// Apply runs fn under the orchestrator's write lock. fn receives a
// pointer to the live State so it can mutate fields directly; the
// pointer must NOT escape fn — it is invalid after Apply returns and
// concurrent reads via Snapshot will not see partial updates.
//
// fn returning an error leaves the state unchanged from fn's
// perspective: errors do not roll back mutations fn already performed
// before returning. Callers that need transactional semantics should
// stage the mutation in a local copy and assign it inside fn only on
// success.
//
// Apply with a nil fn returns an error rather than silently no-oping —
// passing nil is almost always a mistake.
func (o *Orchestrator[State]) Apply(fn func(*State) error) error {
	if fn == nil {
		return errors.New("orchestrator: Apply: fn is nil")
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return fn(&o.state)
}

// Call describes a single handoff. Skill is the unprefixed skill name
// the configured agent.ToolCaller resolves; Input is the typed input
// the orchestrator marshals to JSON before dispatch. Input may be a
// struct, a map[string]any, or nil (treated as an empty object).
type Call struct {
	// Skill is the unprefixed skill / tool name the orchestrator's
	// caller will resolve. Empty Skill names are rejected at handoff
	// time.
	Skill string

	// Input is the typed argument object. Non-nil values are JSON-
	// marshaled, then unmarshaled into a map[string]any so the
	// downstream caller (which expects map[string]any per the
	// pkg/mcp.AgentClient.CallTool contract) sees a JSON-clean view.
	Input any
}

// Result is the per-item outcome from ParallelHandoff. Index references
// the position of the originating Call in the input slice (useful when
// the caller needs to merge results positionally back into State). Err
// captures per-item failures; ParallelHandoff returns nil at the batch
// level when individual handoffs fail so callers can decide how to
// merge — partial success is a real outcome for parallel agentic flows
// and the orchestrator does not pre-empt that decision.
type Result[Out any] struct {
	Index  int
	Output Out
	Err    error
}

// Handoff dispatches a single subagent call through the orchestrator's
// ToolCaller and decodes the result into Out via JSON. State is not
// passed to the subagent — the caller is responsible for constructing
// Input from a Snapshot and merging Output back via Apply.
//
// Handoff is a free function (not a method) because Go does not allow
// generic methods to introduce new type parameters. The Orchestrator's
// State parameter is required so the function statically witnesses
// which orchestrator the handoff belongs to; the runtime does not
// otherwise need it.
func Handoff[State, Out any](ctx context.Context, o *Orchestrator[State], call Call) (Out, error) {
	var zero Out
	if o == nil {
		return zero, errors.New("orchestrator: Handoff: nil Orchestrator")
	}
	if call.Skill == "" {
		return zero, errors.New("orchestrator: Handoff: empty skill name")
	}
	caller := o.caller
	if caller == nil {
		return zero, errors.New("orchestrator: Handoff: nil ToolCaller")
	}

	tracer := o.getTracer()
	ctx, span := tracer.Start(ctx, "agent.orchestrator.handoff",
		trace.WithAttributes(attribute.String("agent.skill", call.Skill)))
	defer span.End()

	args, err := toArguments(call.Input)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return zero, fmt.Errorf("handoff %q: marshaling input: %w", call.Skill, err)
	}

	logger := o.getLogger().With("skill", call.Skill)
	logger.Debug("handoff dispatch")

	res, err := caller.CallTool(ctx, call.Skill, args)
	if err != nil {
		logger.Error("handoff failed", "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return zero, fmt.Errorf("handoff %q: %w", call.Skill, err)
	}
	if res != nil && res.IsError {
		text := contentText(res)
		errOut := fmt.Errorf("handoff %q: skill returned error: %s", call.Skill, text)
		span.RecordError(errOut)
		span.SetStatus(codes.Error, errOut.Error())
		return zero, errOut
	}

	out, err := decodeResult[Out](res)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return zero, fmt.Errorf("handoff %q: decoding output: %w", call.Skill, err)
	}
	span.SetAttributes(attribute.Bool("agent.handoff.ok", true))
	return out, nil
}

// ParallelHandoff dispatches every Call concurrently with concurrency
// capped at min(SetMaxParallel, HardMaxParallel). The returned Results
// slice mirrors the calls slice positionally — Results[i] is the
// outcome of calls[i], regardless of completion order. Per-call errors
// surface in Result.Err; the batch-level error is non-nil only on
// invalid arguments (nil orchestrator) or context cancellation that
// occurred before any handoff started.
//
// Cancelling ctx in flight stops new handoffs from being scheduled and
// surfaces ctx.Err() on calls that had not yet started; in-flight
// handoffs receive the cancelled context and propagate the cancellation
// through their own logic. The function only returns once every
// scheduled handoff goroutine has exited, so callers can rely on
// Results being fully populated.
func ParallelHandoff[State, Out any](ctx context.Context, o *Orchestrator[State], calls []Call) ([]Result[Out], error) {
	if o == nil {
		return nil, errors.New("orchestrator: ParallelHandoff: nil Orchestrator")
	}
	if len(calls) == 0 {
		return nil, nil
	}

	cap := o.effectiveCap()

	tracer := o.getTracer()
	ctx, span := tracer.Start(ctx, "agent.orchestrator.parallel",
		trace.WithAttributes(
			attribute.Int("parallel.size", len(calls)),
			attribute.Int("parallel.cap", cap),
		))
	defer span.End()

	logger := o.getLogger().With("size", len(calls), "cap", cap)
	logger.Debug("parallel handoff start")

	results := make([]Result[Out], len(calls))
	sem := make(chan struct{}, cap)
	var wg sync.WaitGroup

	for i, c := range calls {
		i, c := i, c
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i].Index = i

			// Acquire a slot or surrender to ctx cancellation. The
			// select pattern ensures a cancelled batch does not deadlock
			// when the semaphore is full of in-flight items that will
			// eventually drain.
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				results[i].Err = ctx.Err()
				return
			}
			defer func() { <-sem }()

			out, err := Handoff[State, Out](ctx, o, c)
			results[i].Output = out
			results[i].Err = err
		}()
	}
	wg.Wait()

	errCount := 0
	for _, r := range results {
		if r.Err != nil {
			errCount++
		}
	}
	span.SetAttributes(attribute.Int("agent.handoff.errors", errCount))
	logger.Debug("parallel handoff done", "errors", errCount)

	return results, nil
}

// effectiveCap reads the configured cap and clamps it into [1,
// HardMaxParallel]. Out-of-range values surface a single warning per
// call rather than per-invocation panic; the warning includes the
// requested value so users discover the clamp without crashing their
// flows.
func (o *Orchestrator[State]) effectiveCap() int {
	o.cfgMu.RLock()
	requested := o.maxPar
	o.cfgMu.RUnlock()

	cap := requested
	if cap <= 0 {
		cap = DefaultMaxParallel
	}
	if cap > HardMaxParallel {
		o.getLogger().Warn("parallel handoff cap exceeds hard ceiling; clamping",
			"requested", requested, "ceiling", HardMaxParallel)
		cap = HardMaxParallel
	}
	return cap
}

func (o *Orchestrator[State]) getLogger() *slog.Logger {
	o.cfgMu.RLock()
	defer o.cfgMu.RUnlock()
	return o.logger
}

func (o *Orchestrator[State]) getTracer() trace.Tracer {
	o.cfgMu.RLock()
	defer o.cfgMu.RUnlock()
	return o.tracer
}

// toArguments converts a typed Input value into the map[string]any
// shape mcp.AgentClient.CallTool expects. nil input becomes an empty
// map; map[string]any inputs pass through verbatim; struct or
// composite inputs round-trip via JSON so the downstream caller sees
// a JSON-clean view (notably: numeric values arrive as float64,
// matching the convention pkg/agent/sandbox.normalizeArgs follows).
func toArguments(input any) (map[string]any, error) {
	if input == nil {
		return map[string]any{}, nil
	}
	if m, ok := input.(map[string]any); ok {
		if m == nil {
			return map[string]any{}, nil
		}
		return m, nil
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}

// decodeResult extracts a JSON-encoded typed output from a tool-call
// result. Skill outputs are surfaced as a single text content block
// holding JSON (typed Go skills emit it via skill.Define's marshaller;
// TS skills emit it via the sandbox dispatcher) — the orchestrator
// concatenates all text blocks before unmarshaling so multi-block
// results compose cleanly.
//
// An empty result decodes as the zero value of Out, which is the
// natural representation of a "the skill ran but emitted nothing"
// outcome.
func decodeResult[Out any](res *agent.ToolCallResult) (Out, error) {
	var zero Out
	if res == nil {
		return zero, nil
	}
	text := contentText(res)
	if text == "" || text == "null" {
		return zero, nil
	}
	var out Out
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return zero, fmt.Errorf("output is not valid JSON: %w", err)
	}
	return out, nil
}

// contentText concatenates all Text blocks in a tool-call result. It
// ignores non-text content (image, resource) — Phase D only handles
// JSON-shaped skill outputs; richer content types land alongside the
// approval-gate work in Phase E.
func contentText(res *agent.ToolCallResult) string {
	if res == nil {
		return ""
	}
	var b []byte
	for _, c := range res.Content {
		if c.Text != "" {
			b = append(b, c.Text...)
		}
	}
	return string(b)
}

// jsonCopy deep-copies a value via JSON round-trip. Used by Snapshot;
// extracted so tests can exercise the failure mode (non-marshalable
// State) deterministically.
func jsonCopy[T any](v T) (T, error) {
	var zero T
	raw, err := json.Marshal(v)
	if err != nil {
		return zero, err
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		return zero, err
	}
	return out, nil
}
