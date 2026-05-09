package orchestrator_test

// Article IV integration test: the orchestrator drives two real TS
// subagents through the existing pkg/agent/sandbox stack — esbuild
// transpiles each skill, goja runs them, and results come back as the
// same JSON-shape any MCP tool result has. This is the cross-package
// smoke for the Phase D requirement that a Go-side orchestrator
// composes TS subagents in parallel through the same code path used
// for any other skill invocation.

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/agent/orchestrator"
	"github.com/gridctl/gridctl/pkg/agent/sandbox"
	"github.com/gridctl/gridctl/pkg/agent/skill"
)

type itemInput struct {
	Item string `json:"item"`
}

type itemOutput struct {
	Result string `json:"result"`
}

type itemState struct {
	Items   []string `json:"items"`
	Results []string `json:"results"`
}

// registerTSSkill wires a TS source string into the registry as a
// skill the sandbox dispatcher resolves on call. Mirrors the pattern
// pkg/agent/sandbox/recursive_test.go uses for cross-skill handoffs;
// kept local here so the integration test reads top-to-bottom.
func registerTSSkill(t *testing.T, reg *skill.Registry, sb *sandbox.Sandbox, name, source string) {
	t.Helper()
	loader := func(string) (string, error) { return source, nil }
	invoker := sb.NewInvoker(name, loader, func(ctx context.Context) sandbox.Bindings {
		return sandbox.Bindings{}
	})
	def := &skill.Definition{
		Name:        name,
		Description: name,
		Invoker:     invoker,
	}
	if err := reg.Register(def); err != nil {
		t.Fatalf("register %s: %v", name, err)
	}
}

// TestIntegration_OrchestratorDrivesTwoParallelTSSubagents is the
// acceptance test for Phase D: a Go orchestrator hands off to two TS
// skills in parallel, merges their outputs back into State via Apply,
// and never lets either skill see *State directly. Run with -race.
func TestIntegration_OrchestratorDrivesTwoParallelTSSubagents(t *testing.T) {
	t.Parallel()

	upperSrc := `
		export default async function (input) {
			return { result: input.item.toUpperCase() };
		}
	`
	doubleSrc := `
		export default async function (input) {
			return { result: input.item + input.item };
		}
	`

	reg := skill.NewRegistry()
	sb := sandbox.New(5 * time.Second)
	registerTSSkill(t, reg, sb, "upper", upperSrc)
	registerTSSkill(t, reg, sb, "double", doubleSrc)

	o := orchestrator.New[itemState](reg, itemState{
		Items: []string{"alpha", "beta"},
	})

	snap := o.Snapshot()

	calls := make([]orchestrator.Call, 0, len(snap.Items)*2)
	for _, item := range snap.Items {
		calls = append(calls,
			orchestrator.Call{Skill: "upper", Input: itemInput{Item: item}},
			orchestrator.Call{Skill: "double", Input: itemInput{Item: item}},
		)
	}

	results, err := orchestrator.ParallelHandoff[itemState, itemOutput](context.Background(), o, calls)
	if err != nil {
		t.Fatalf("ParallelHandoff: %v", err)
	}
	if len(results) != len(calls) {
		t.Fatalf("len(results) = %d, want %d", len(results), len(calls))
	}

	if err := o.Apply(func(s *itemState) error {
		for _, r := range results {
			if r.Err != nil {
				return r.Err
			}
			s.Results = append(s.Results, r.Output.Result)
		}
		return nil
	}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	final := o.Snapshot()
	got := append([]string{}, final.Results...)
	sort.Strings(got)
	want := []string{"ALPHA", "BETA", "alphaalpha", "betabeta"}
	sort.Strings(want)
	if !equalStrings(got, want) {
		t.Errorf("results = %v, want %v", got, want)
	}

	// Verify single-writer enforcement is structural: the original
	// snapshot mutating in-place must not affect the orchestrator's
	// State, even after the parallel batch and merge.
	snap.Items = nil
	snap.Results = []string{"hijacked"}
	live := o.Snapshot()
	if len(live.Items) != 2 || len(live.Results) != 4 {
		t.Errorf("snapshot mutation leaked into State: items=%v results=%v",
			live.Items, live.Results)
	}
}

// TestIntegration_HandoffEmitsTypedJSONFromTS confirms the single
// handoff path round-trips a typed JSON output through the TS skill
// boundary unchanged. The shape is the same one ParallelHandoff
// composes onto, so a single end-to-end check on Handoff plus the
// parallel batch test above covers both surfaces.
func TestIntegration_HandoffEmitsTypedJSONFromTS(t *testing.T) {
	t.Parallel()

	src := `
		export default async function (input) {
			return { result: "hello " + input.item };
		}
	`
	reg := skill.NewRegistry()
	sb := sandbox.New(5 * time.Second)
	registerTSSkill(t, reg, sb, "greet", src)

	o := orchestrator.New[itemState](reg, itemState{})
	out, err := orchestrator.Handoff[itemState, itemOutput](
		context.Background(), o,
		orchestrator.Call{Skill: "greet", Input: itemInput{Item: "world"}},
	)
	if err != nil {
		t.Fatalf("Handoff: %v", err)
	}
	if !strings.Contains(out.Result, "hello world") {
		t.Errorf("result = %q, want containing 'hello world'", out.Result)
	}
}

// equalStrings is the smallest dependency-free slice equality check.
// Avoids pulling reflect.DeepEqual into a hot test path for what is
// fundamentally a sorted-string comparison.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// guard: ensure the testState type encodes/decodes via JSON without
// loss. The orchestrator's Snapshot path depends on this property; if
// tests inadvertently introduce a non-marshalable State, this check
// surfaces it before the rest of the suite runs.
func init() {
	probe := itemState{Items: []string{"x"}, Results: []string{"y"}}
	raw, err := json.Marshal(probe)
	if err != nil {
		panic("integration_test: itemState not marshalable: " + err.Error())
	}
	var back itemState
	if err := json.Unmarshal(raw, &back); err != nil {
		panic("integration_test: itemState not unmarshalable: " + err.Error())
	}
}
