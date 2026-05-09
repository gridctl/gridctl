package sandbox_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/agent/sandbox"
	"github.com/gridctl/gridctl/pkg/agent/skill"
)

// TestRecursiveComposition_TypedSkillHandoffFromTS is the cross-
// package smoke test for the Phase C recursive-composability
// constraint: a TS skill must be able to invoke a typed Go skill
// through the same handoff() binding it would use to call any other
// skill, with no bespoke remote / local execution mode.
//
// The wiring:
//
//   skill.Registry  ◄── Go skill.Define("greet", run) registered here
//        ▲
//        │ SkillCaller surface = Registry.CallTool
//        │
//   sandbox.Bindings.SkillCaller
//        │
//        ▼
//   handoff("greet", input) inside a TS skill
//
// This is the same wiring pkg/registry.Server uses internally:
// Server.CallTool consults a SkillRegistry. Pointing one gridctl
// instance at another over MCP would replace the in-process Registry
// with the gateway; the path through handoff() is identical.
func TestRecursiveComposition_TypedSkillHandoffFromTS(t *testing.T) {
	t.Parallel()

	type greetInput struct {
		Name string `json:"name" jsonschema:"required"`
	}
	type greetOutput struct {
		Greeting string `json:"greeting"`
	}

	registry := skill.NewRegistry()
	greet, err := skill.Define("greet", "greets a name",
		func(_ context.Context, in greetInput) (greetOutput, error) {
			return greetOutput{Greeting: "hello " + in.Name}, nil
		})
	if err != nil {
		t.Fatalf("Define greet: %v", err)
	}
	if err := registry.Register(greet); err != nil {
		t.Fatalf("Register: %v", err)
	}

	sb := sandbox.New(2 * time.Second)
	src := `
		export default async function (input: { name: string }) {
			const out = await handoff("greet", { name: input.name });
			return { wrapped: out.greeting + "!" };
		}
	`
	res, err := sb.Execute(context.Background(), src,
		map[string]any{"name": "world"},
		sandbox.Bindings{SkillCaller: registry},
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal([]byte(res.Value), &out); err != nil {
		t.Fatalf("decode value: %v (raw=%q)", err, res.Value)
	}
	if out["wrapped"] != "hello world!" {
		t.Errorf("wrapped = %v, want hello world!", out["wrapped"])
	}
}

// TestRecursiveComposition_TSCallsTSViaHandoff exercises the second
// half of the constraint: a TS skill handing off to another TS skill
// uses the same path. The path remains a single SkillCaller surface;
// the handed-off skill's bindings (its own ToolCaller, ChatModel,
// approver) are scoped to that invocation and do not leak across the
// boundary.
//
// This time we register the inner skill via a sandbox.NewInvoker so
// the TS-handler half of the registry is exercised too.
func TestRecursiveComposition_TSCallsTSViaHandoff(t *testing.T) {
	t.Parallel()

	innerSrc := `
		export default async function (input: { greeting: string }) {
			return { upper: input.greeting.toUpperCase() };
		}
	`
	outerSrc := `
		export default async function (input: { name: string }) {
			const inner = await handoff("upper", { greeting: "hello " + input.name });
			return { result: inner.upper };
		}
	`

	sb := sandbox.New(2 * time.Second)
	registry := skill.NewRegistry()

	innerLoader := func(name string) (string, error) { return innerSrc, nil }
	innerInvoker := sb.NewInvoker("upper", innerLoader, func(ctx context.Context) sandbox.Bindings {
		return sandbox.Bindings{}
	})
	innerDef := &skill.Definition{
		Name:        "upper",
		Description: "uppercase a greeting",
		Invoker:     innerInvoker,
	}
	if err := registry.Register(innerDef); err != nil {
		t.Fatalf("Register inner: %v", err)
	}

	res, err := sb.Execute(context.Background(), outerSrc,
		map[string]any{"name": "world"},
		sandbox.Bindings{SkillCaller: registry},
	)
	if err != nil {
		t.Fatalf("Execute outer: %v", err)
	}
	if !strings.Contains(res.Value, "HELLO WORLD") {
		t.Errorf("value = %s, want HELLO WORLD", res.Value)
	}
}
