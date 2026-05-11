package skill

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/mcp"
)

// helloInput / helloOutput exercise the typical struct-tagged shape.
type helloInput struct {
	Name string `json:"name" jsonschema:"required,description=Name to greet"`
}

type helloOutput struct {
	Greeting string `json:"greeting"`
}

func helloRunner(_ RunContext, in helloInput) (helloOutput, error) {
	if in.Name == "" {
		return helloOutput{}, errors.New("name required")
	}
	return helloOutput{Greeting: "hello " + in.Name}, nil
}

func TestDefine_RoundtripsTypedInputAndOutput(t *testing.T) {
	t.Parallel()
	def, err := Define("hello", "greets a name", "", helloRunner)
	if err != nil {
		t.Fatalf("Define: %v", err)
	}
	if def.Name != "hello" {
		t.Errorf("Name = %q, want %q", def.Name, "hello")
	}
	if def.Description != "greets a name" {
		t.Errorf("Description = %q, want %q", def.Description, "greets a name")
	}

	got, err := def.Invoker(context.Background(), map[string]any{"name": "world"})
	if err != nil {
		t.Fatalf("Invoker: %v", err)
	}
	if got == nil || len(got.Content) != 1 {
		t.Fatalf("got %+v, want one content item", got)
	}
	var out helloOutput
	if err := json.Unmarshal([]byte(got.Content[0].Text), &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if out.Greeting != "hello world" {
		t.Errorf("greeting = %q, want %q", out.Greeting, "hello world")
	}
}

func TestDefine_PropagatesRunnerErrors(t *testing.T) {
	t.Parallel()
	def, err := Define("hello", "greets a name", "", helloRunner)
	if err != nil {
		t.Fatalf("Define: %v", err)
	}
	got, err := def.Invoker(context.Background(), map[string]any{"name": ""})
	if err == nil {
		t.Fatalf("expected runner error, got result %+v", got)
	}
	if !strings.Contains(err.Error(), "name required") {
		t.Errorf("error = %v, want it to contain runner error", err)
	}
}

func TestDefine_RejectsScalarInput(t *testing.T) {
	t.Parallel()
	_, err := Define("scalar", "no good", "", func(_ RunContext, in int) (int, error) { return in, nil })
	if err == nil {
		t.Fatalf("expected error for scalar input type")
	}
}

func TestDefine_AllowsMapInput(t *testing.T) {
	t.Parallel()
	def, err := Define("loose", "loose input", "", func(_ RunContext, in map[string]any) (map[string]any, error) {
		return in, nil
	})
	if err != nil {
		t.Fatalf("Define with map input: %v", err)
	}

	var schema map[string]any
	if err := json.Unmarshal(def.InputSchema, &schema); err != nil {
		t.Fatalf("input schema not JSON: %v", err)
	}
	if schema["type"] != "object" {
		t.Errorf("schema type = %v, want object", schema["type"])
	}

	got, err := def.Invoker(context.Background(), map[string]any{"x": float64(1)})
	if err != nil {
		t.Fatalf("Invoker: %v", err)
	}
	if got == nil || len(got.Content) == 0 {
		t.Fatalf("expected content")
	}
}

func TestDefine_RejectsEmptyName(t *testing.T) {
	t.Parallel()
	_, err := Define("", "x", "", helloRunner)
	if err == nil {
		t.Fatalf("expected error for empty name")
	}
}

func TestDefine_RejectsNilRunner(t *testing.T) {
	t.Parallel()
	_, err := Define[helloInput, helloOutput]("hello", "x", "", nil)
	if err == nil {
		t.Fatalf("expected error for nil runner")
	}
}

func TestDefine_RunContextExposesBodyAndName(t *testing.T) {
	t.Parallel()
	const wantBody = "# triage runbook\n\nseverity: page on err > 5%\n"
	const wantName = "triage"
	var gotBody, gotName string
	def, err := Define(wantName, "expose body and name", wantBody,
		func(rc RunContext, in helloInput) (helloOutput, error) {
			gotBody = rc.SkillBody()
			gotName = rc.SkillName()
			return helloOutput{Greeting: "ok"}, nil
		})
	if err != nil {
		t.Fatalf("Define: %v", err)
	}
	if _, err := def.Invoker(context.Background(), map[string]any{"name": "x"}); err != nil {
		t.Fatalf("Invoker: %v", err)
	}
	if gotBody != wantBody {
		t.Errorf("SkillBody = %q, want %q", gotBody, wantBody)
	}
	if gotName != wantName {
		t.Errorf("SkillName = %q, want %q", gotName, wantName)
	}
}

func TestWithSkillBody_OverridesCapturedBody(t *testing.T) {
	t.Parallel()
	const captured = "captured-at-define-time"
	const override = "loader-supplied-override"
	var gotBody string
	def, err := Define("override", "body override", captured,
		func(rc RunContext, _ helloInput) (helloOutput, error) {
			gotBody = rc.SkillBody()
			return helloOutput{Greeting: "ok"}, nil
		})
	if err != nil {
		t.Fatalf("Define: %v", err)
	}
	// Without override: captured body wins.
	if _, err := def.Invoker(context.Background(), map[string]any{"name": "x"}); err != nil {
		t.Fatalf("Invoker (no override): %v", err)
	}
	if gotBody != captured {
		t.Errorf("without override SkillBody = %q, want %q", gotBody, captured)
	}
	// With override: WithSkillBody-wrapped context wins.
	ctx := WithSkillBody(context.Background(), override)
	if _, err := def.Invoker(ctx, map[string]any{"name": "x"}); err != nil {
		t.Fatalf("Invoker (override): %v", err)
	}
	if gotBody != override {
		t.Errorf("with override SkillBody = %q, want %q", gotBody, override)
	}
}

func TestDefine_RunContextPreservesContextValues(t *testing.T) {
	t.Parallel()
	type ctxKey string
	parent := context.WithValue(context.Background(), ctxKey("trace"), "abc123")
	def, err := Define("ctx", "preserves context", "",
		func(rc RunContext, _ helloInput) (helloOutput, error) {
			if rc.Value(ctxKey("trace")) != "abc123" {
				return helloOutput{}, errors.New("ctx value not propagated")
			}
			if rc.SkillBody() != "" {
				return helloOutput{}, errors.New("empty body should read as empty string")
			}
			return helloOutput{Greeting: "ok"}, nil
		})
	if err != nil {
		t.Fatalf("Define: %v", err)
	}
	if _, err := def.Invoker(parent, map[string]any{"name": "x"}); err != nil {
		t.Fatalf("Invoker: %v", err)
	}
}

func TestRegistry_RegistersAndDispatches(t *testing.T) {
	t.Parallel()
	def, err := Define("hello", "greets", "", helloRunner)
	if err != nil {
		t.Fatalf("Define: %v", err)
	}
	reg := NewRegistry()
	if err := reg.Register(def); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got, ok := reg.Get("hello")
	if !ok || got != def {
		t.Errorf("Get returned (%v, %v); want stored definition", got, ok)
	}
	if list := reg.List(); len(list) != 1 {
		t.Errorf("List len = %d, want 1", len(list))
	}

	tools := reg.Tools()
	if len(tools) != 1 || tools[0].Name != "hello" {
		t.Errorf("Tools = %+v, want one entry named hello", tools)
	}
	if len(tools[0].InputSchema) == 0 {
		t.Error("Tool.InputSchema empty")
	}

	res, err := reg.CallTool(context.Background(), "hello", map[string]any{"name": "registry"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !strings.Contains(res.Content[0].Text, "hello registry") {
		t.Errorf("call content = %q, want greeting for registry", res.Content[0].Text)
	}
}

func TestRegistry_RejectsDuplicateName(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	def1, _ := Define("hello", "1", "", helloRunner)
	def2, _ := Define("hello", "2", "", helloRunner)
	if err := reg.Register(def1); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if err := reg.Register(def2); err == nil {
		t.Fatalf("second Register: expected duplicate error")
	}
}

func TestRegistry_RejectsMalformedDefinition(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	cases := []struct {
		name string
		def  *Definition
	}{
		{"nil", nil},
		{"empty name", &Definition{Invoker: func(context.Context, map[string]any) (*mcp.ToolCallResult, error) { return nil, nil }}},
		{"no invoker", &Definition{Name: "x"}},
		{"bad schema", &Definition{
			Name:        "x",
			InputSchema: json.RawMessage(`{not json`),
			Invoker:     func(context.Context, map[string]any) (*mcp.ToolCallResult, error) { return nil, nil },
		}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := reg.Register(tc.def); err == nil {
				t.Errorf("Register %s: expected error", tc.name)
			}
		})
	}
}

func TestRegistry_CallToolUnknownSkillReturnsError(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	if _, err := reg.CallTool(context.Background(), "missing", nil); err == nil {
		t.Fatalf("expected error for unknown skill")
	}
}

func TestDefinition_ToolHandlesEmptySchema(t *testing.T) {
	t.Parallel()
	def := &Definition{
		Name: "raw",
		Invoker: func(context.Context, map[string]any) (*mcp.ToolCallResult, error) {
			return &mcp.ToolCallResult{}, nil
		},
	}
	tool := def.Tool()
	if string(tool.InputSchema) != `{"type":"object"}` {
		t.Errorf("empty schema fallback = %s", tool.InputSchema)
	}
}

func TestDefinition_ToolReturnsSchemaCopy(t *testing.T) {
	t.Parallel()
	def := &Definition{
		Name:        "raw",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Invoker: func(context.Context, map[string]any) (*mcp.ToolCallResult, error) {
			return &mcp.ToolCallResult{}, nil
		},
	}
	tool := def.Tool()
	tool.InputSchema[0] = 'X'
	if def.InputSchema[0] == 'X' {
		t.Error("Tool() returned a non-copy schema")
	}
}
