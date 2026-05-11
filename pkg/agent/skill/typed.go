package skill

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gridctl/gridctl/pkg/mcp"
)

// RunContext is the typed runner's first argument. It embeds
// context.Context — cancellation, deadlines, and request-scoped values
// flow through unchanged — and surfaces two skill-scoped accessors so
// authors can drive the hybrid pattern: feed the SKILL.md body straight
// into an llm.Generate System slot, key per-skill state on SkillName.
//
// Body and Name are captured at Define-time and read with no per-call
// I/O. The runtime resolves the body from the registry store before
// constructing the Definition, so RunContext readers never reach back
// into the registry on the call path.
type RunContext interface {
	context.Context

	// SkillBody returns the post-frontmatter markdown body the
	// registry parsed from SKILL.md. Empty string for skills whose
	// SKILL.md has no body — and for skills constructed via Define
	// without one (programmatic registrations, tests).
	SkillBody() string

	// SkillName returns the registered skill name. The same string
	// the gateway exposes as the unprefixed MCP tool name.
	SkillName() string
}

// runContext is the concrete RunContext the Define-built invoker
// constructs per call. The struct embeds context.Context so the
// runner sees a real context that satisfies the interface, and the
// body/name fields are closure-captured at Define time so reads are
// a single pointer chase with no synchronisation cost.
type runContext struct {
	context.Context
	body string
	name string
}

// skillBodyOverrideKey is the context-value key WithSkillBody uses to
// inject a body that takes precedence over the value captured at
// Define time. The gateway-builder Go-plugin loader sets this so a
// plugin that registered with body="" still surfaces the on-disk
// SKILL.md body through ctx.SkillBody() — preserving the hybrid
// pattern across the plugin boundary without a generics-crossing
// re-registration the loader cannot perform.
type skillBodyOverrideKey struct{}

// WithSkillBody returns a context whose downstream RunContext.SkillBody()
// resolves to body instead of the value captured at Define time. The
// override is read-only (a context value), so concurrent invocations
// of the same Definition each see the body wired by their own
// caller's context. In-process registrations have no reason to call
// this — they pass body to Define directly.
func WithSkillBody(parent context.Context, body string) context.Context {
	return context.WithValue(parent, skillBodyOverrideKey{}, body)
}

func (r *runContext) SkillBody() string {
	if v, ok := r.Value(skillBodyOverrideKey{}).(string); ok {
		return v
	}
	return r.body
}
func (r *runContext) SkillName() string { return r.name }

// newRunContext lifts a parent context plus the skill-scoped body
// and name into a RunContext. Exposed unexported so the package's
// own invokers and tests can build instances without leaking the
// concrete type to authors — RunContext is the surface, not
// runContext.
func newRunContext(parent context.Context, body, name string) *runContext {
	return &runContext{Context: parent, body: body, name: name}
}

// TypedRunner is the typed authoring signature skill authors write.
// I and O are Go structs; the SDK marshals across the boundary so the
// handler body works in typed Go and the wire form stays JSON. The
// first argument is RunContext rather than context.Context so the
// runner can read the SKILL.md body and the registered name without
// reaching back into the registry.
type TypedRunner[I any, O any] func(ctx RunContext, input I) (O, error)

// Define wraps a typed runner as a Definition. The input schema is
// inferred from I via reflectInputSchema (jsonschema struct tags); the
// returned Definition's Invoker:
//
//  1. Re-marshals the argument map back to JSON (the gateway hands
//     skills the decoded shape; the typed boundary needs the raw
//     bytes to feed json.Unmarshal into a Go value of type I).
//  2. Decodes into a fresh I.
//  3. Constructs a RunContext closing over the body and name passed
//     to Define so ctx.SkillBody() / ctx.SkillName() resolve without
//     per-call I/O.
//  4. Invokes the runner.
//  5. Renders the typed output O back to MCP content as a single
//     JSON text block. Skills that need richer content shapes
//     (multi-part, image, etc.) should construct a Definition by hand.
//
// body is the post-frontmatter SKILL.md markdown. Pass an empty string
// for programmatic registrations that don't have a SKILL.md sibling
// (tests, hand-built fixtures); the runner's ctx.SkillBody() reads as
// "" and the hybrid pattern degrades gracefully.
//
// Decode failures and runner errors flow back to the caller; the
// runner's err is wrapped, never swallowed.
func Define[I any, O any](name, description, body string, run TypedRunner[I, O]) (*Definition, error) {
	if name == "" {
		return nil, errors.New("skill.Define: name is required")
	}
	if run == nil {
		return nil, fmt.Errorf("skill %q: run is nil", name)
	}

	var zeroIn I
	schema, err := reflectInputSchema(zeroIn)
	if err != nil {
		return nil, fmt.Errorf("skill %q: inferring input schema: %w", name, err)
	}

	invoker := func(ctx context.Context, arguments map[string]any) (*mcp.ToolCallResult, error) {
		var input I
		if len(arguments) > 0 {
			raw, err := json.Marshal(arguments)
			if err != nil {
				return nil, fmt.Errorf("skill %q: re-marshaling arguments: %w", name, err)
			}
			if err := json.Unmarshal(raw, &input); err != nil {
				return nil, fmt.Errorf("skill %q: decoding input: %w", name, err)
			}
		}

		rc := newRunContext(ctx, body, name)
		output, err := run(rc, input)
		if err != nil {
			return nil, fmt.Errorf("skill %q: %w", name, err)
		}

		raw, err := json.Marshal(output)
		if err != nil {
			return nil, fmt.Errorf("skill %q: marshaling output: %w", name, err)
		}
		return &mcp.ToolCallResult{
			Content: []mcp.Content{mcp.NewTextContent(string(raw))},
		}, nil
	}

	return &Definition{
		Name:        name,
		Description: description,
		InputSchema: schema,
		Invoker:     invoker,
	}, nil
}

// MustDefine is the panicking variant of Define. Use it in package-init
// code where a malformed skill is a programming error and the binary
// has nothing useful to do without it. Library code should call
// Define and propagate the error.
func MustDefine[I any, O any](name, description, body string, run TypedRunner[I, O]) *Definition {
	def, err := Define[I, O](name, description, body, run)
	if err != nil {
		panic(err)
	}
	return def
}
