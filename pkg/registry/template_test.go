package registry

import (
	"fmt"
	"strings"
	"testing"
)

func TestResolveString(t *testing.T) {
	ctx := &TemplateContext{
		Inputs: map[string]any{
			"name":  "alice",
			"count": 42,
			"flag":  true,
		},
		Steps: map[string]*StepResult{
			"add": NewStepResult(`{"result": {"value": 100}}`, false),
			"fetch": NewStepResult(`{"data": {"name": "bob"}, "items": [{"id": "first"}, {"id": "second"}]}`, false),
			"check": NewStepResult("ok", false),
			"fail":  NewStepResult("something went wrong", true),
			"empty": NewStepResult("", false),
		},
	}

	tests := []struct {
		name    string
		input   string
		want    any
		wantErr bool
	}{
		{
			name:  "simple input reference",
			input: "{{ inputs.name }}",
			want:  "alice",
		},
		{
			name:  "numeric input (whole expression returns typed value)",
			input: "{{ inputs.count }}",
			want:  42,
		},
		{
			name:  "boolean input (whole expression returns typed value)",
			input: "{{ inputs.flag }}",
			want:  true,
		},
		{
			name:  "mixed text with input",
			input: "Hello {{ inputs.name }}!",
			want:  "Hello alice!",
		},
		{
			name:  "step result reference",
			input: "{{ steps.check.result }}",
			want:  "ok",
		},
		{
			name:  "JSON path extraction",
			input: "{{ steps.fetch.json.data.name }}",
			want:  "bob",
		},
		{
			name:  "nested JSON path",
			input: "{{ steps.add.json.result.value }}",
			want:  float64(100),
		},
		{
			name:  "array index in JSON path",
			input: "{{ steps.fetch.json.items.0.id }}",
			want:  "first",
		},
		{
			name:  "array index second element",
			input: "{{ steps.fetch.json.items.1.id }}",
			want:  "second",
		},
		{
			name:  "step is_error false",
			input: "{{ steps.check.is_error }}",
			want:  false,
		},
		{
			name:  "step is_error true",
			input: "{{ steps.fail.is_error }}",
			want:  true,
		},
		{
			name:    "missing input reference",
			input:   "{{ inputs.missing }}",
			wantErr: true,
		},
		{
			name:    "missing step reference",
			input:   "{{ steps.nonexistent.result }}",
			wantErr: true,
		},
		{
			name:    "invalid JSON path",
			input:   "{{ steps.check.json.foo }}",
			wantErr: true,
		},
		{
			name:  "no template expression (passthrough)",
			input: "plain text",
			want:  "plain text",
		},
		{
			name:  "empty string passthrough",
			input: "",
			want:  "",
		},
		{
			name:  "multiple expressions in text",
			input: "{{ inputs.name }} has {{ inputs.count }} items",
			want:  "alice has 42 items",
		},
		{
			name:    "unknown namespace",
			input:   "{{ env.HOME }}",
			wantErr: true,
		},
		{
			name:    "incomplete step reference",
			input:   "{{ steps.add }}",
			wantErr: true,
		},
		{
			name:    "unknown step field",
			input:   "{{ steps.add.unknown }}",
			wantErr: true,
		},
		{
			name:    "array index out of bounds",
			input:   "{{ steps.fetch.json.items.5.id }}",
			wantErr: true,
		},
		{
			name:    "non-numeric array index",
			input:   "{{ steps.fetch.json.items.abc.id }}",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveString(tt.input, ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("ResolveString() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ResolveString() = %v (%T), want %v (%T)", got, got, tt.want, tt.want)
			}
		})
	}
}

func TestResolveArgs(t *testing.T) {
	ctx := &TemplateContext{
		Inputs: map[string]any{
			"name":  "alice",
			"count": 42,
		},
		Steps: map[string]*StepResult{
			"prev": NewStepResult(`{"value": 10}`, false),
		},
	}

	tests := []struct {
		name    string
		args    map[string]any
		check   func(t *testing.T, result map[string]any)
		wantErr bool
	}{
		{
			name: "mixed types",
			args: map[string]any{
				"name":    "{{ inputs.name }}",
				"count":   "{{ inputs.count }}",
				"literal": 99,
				"flag":    true,
			},
			check: func(t *testing.T, result map[string]any) {
				if result["name"] != "alice" {
					t.Errorf("name = %v, want alice", result["name"])
				}
				if result["count"] != 42 {
					t.Errorf("count = %v, want 42", result["count"])
				}
				if result["literal"] != 99 {
					t.Errorf("literal = %v, want 99", result["literal"])
				}
				if result["flag"] != true {
					t.Errorf("flag = %v, want true", result["flag"])
				}
			},
		},
		{
			name: "nested map resolution",
			args: map[string]any{
				"nested": map[string]any{
					"inner": "{{ inputs.name }}",
				},
			},
			check: func(t *testing.T, result map[string]any) {
				nested := result["nested"].(map[string]any)
				if nested["inner"] != "alice" {
					t.Errorf("nested.inner = %v, want alice", nested["inner"])
				}
			},
		},
		{
			name: "array values with templates",
			args: map[string]any{
				"items": []any{"{{ inputs.name }}", "literal", "{{ steps.prev.json.value }}"},
			},
			check: func(t *testing.T, result map[string]any) {
				items := result["items"].([]any)
				if items[0] != "alice" {
					t.Errorf("items[0] = %v, want alice", items[0])
				}
				if items[1] != "literal" {
					t.Errorf("items[1] = %v, want literal", items[1])
				}
				if items[2] != float64(10) {
					t.Errorf("items[2] = %v, want 10", items[2])
				}
			},
		},
		{
			name: "all plain values (no templates)",
			args: map[string]any{
				"a": "hello",
				"b": 42,
				"c": true,
			},
			check: func(t *testing.T, result map[string]any) {
				if result["a"] != "hello" {
					t.Errorf("a = %v, want hello", result["a"])
				}
				if result["b"] != 42 {
					t.Errorf("b = %v, want 42", result["b"])
				}
				if result["c"] != true {
					t.Errorf("c = %v, want true", result["c"])
				}
			},
		},
		{
			name:    "error in arg propagates",
			args:    map[string]any{"bad": "{{ inputs.missing }}"},
			wantErr: true,
		},
		{
			name: "nil args returns nil",
			args: nil,
			check: func(t *testing.T, result map[string]any) {
				if result != nil {
					t.Errorf("expected nil, got %v", result)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ResolveArgs(tt.args, ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("ResolveArgs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.check != nil {
				tt.check(t, result)
			}
		})
	}
}

func TestEvaluateCondition(t *testing.T) {
	ctx := &TemplateContext{
		Inputs: map[string]any{},
		Steps: map[string]*StepResult{
			"check": NewStepResult(`{"valid": true, "status": "ok"}`, false),
			"fail":  NewStepResult("error details", true),
			"empty": NewStepResult("", false),
		},
	}

	tests := []struct {
		name    string
		expr    string
		want    bool
		wantErr bool
	}{
		{
			name: "equality true",
			expr: "{{ steps.check.json.valid == true }}",
			want: true,
		},
		{
			name: "equality false",
			expr: "{{ steps.check.json.valid == false }}",
			want: false,
		},
		{
			name: "inequality true",
			expr: "{{ steps.check.json.status != error }}",
			want: true,
		},
		{
			name: "inequality false",
			expr: "{{ steps.check.json.status != ok }}",
			want: false,
		},
		{
			name: "boolean check is_error false",
			expr: "{{ steps.check.is_error }}",
			want: false,
		},
		{
			name: "boolean check is_error true",
			expr: "{{ steps.fail.is_error }}",
			want: true,
		},
		{
			name: "existence check non-empty result",
			expr: "{{ steps.fail.result }}",
			want: true,
		},
		{
			name: "existence check empty result",
			expr: "{{ steps.empty.result }}",
			want: false,
		},
		{
			name: "string equality",
			expr: "{{ steps.check.json.status == ok }}",
			want: true,
		},
		{
			name:    "missing step in condition",
			expr:    "{{ steps.nonexistent.result }}",
			wantErr: true,
		},
		{
			name: "without {{ }} wrapper",
			expr: "steps.check.json.valid == true",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EvaluateCondition(tt.expr, ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("EvaluateCondition() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("EvaluateCondition() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveTemplate(t *testing.T) {
	ctx := &TemplateContext{
		Inputs: map[string]any{
			"project": "gridctl",
		},
		Steps: map[string]*StepResult{
			"lint":  NewStepResult("0 errors", false),
			"build": NewStepResult("build successful", false),
		},
	}

	tests := []struct {
		name    string
		tmpl    string
		want    string
		wantErr bool
	}{
		{
			name: "multi-line template with step references",
			tmpl: "Lint: {{ steps.lint.result }}\nBuild: {{ steps.build.result }}",
			want: "Lint: 0 errors\nBuild: build successful",
		},
		{
			name: "template with inputs and steps",
			tmpl: "Project {{ inputs.project }}: {{ steps.build.result }}",
			want: "Project gridctl: build successful",
		},
		{
			name: "no expressions",
			tmpl: "plain text output",
			want: "plain text output",
		},
		{
			name:    "missing reference produces error",
			tmpl:    "Result: {{ steps.missing.result }}",
			wantErr: true,
		},
		{
			name: "empty template",
			tmpl: "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveTemplate(tt.tmpl, ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("ResolveTemplate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ResolveTemplate() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewStepResult(t *testing.T) {
	t.Run("parses valid JSON", func(t *testing.T) {
		sr := NewStepResult(`{"key": "value"}`, false)
		if sr.Raw == nil {
			t.Error("expected Raw to be non-nil for valid JSON")
		}
		if sr.IsError {
			t.Error("expected IsError to be false")
		}
	})

	t.Run("nil Raw for non-JSON", func(t *testing.T) {
		sr := NewStepResult("plain text", false)
		if sr.Raw != nil {
			t.Errorf("expected Raw to be nil, got %v", sr.Raw)
		}
	})

	t.Run("truncates oversized result", func(t *testing.T) {
		big := strings.Repeat("x", maxResultSize+100)
		sr := NewStepResult(big, false)
		if len(sr.Result) != maxResultSize {
			t.Errorf("expected result length %d, got %d", maxResultSize, len(sr.Result))
		}
	})

	t.Run("preserves isError", func(t *testing.T) {
		sr := NewStepResult("error", true)
		if !sr.IsError {
			t.Error("expected IsError to be true")
		}
	})
}

func TestSafetyConstraints(t *testing.T) {
	ctx := &TemplateContext{
		Inputs: map[string]any{"name": "test"},
		Steps:  map[string]*StepResult{},
	}

	t.Run("rejects expression exceeding max length", func(t *testing.T) {
		long := "inputs." + strings.Repeat("a", maxExpressionLen)
		_, err := ResolveString("{{ "+long+" }}", ctx)
		if err == nil {
			t.Error("expected error for oversized expression")
		}
		if !strings.Contains(err.Error(), "exceeds maximum length") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("rejects expressions with invalid characters", func(t *testing.T) {
		badExprs := []string{
			"inputs.name; rm -rf",
			"inputs.name`whoami`",
			"inputs.name$(cmd)",
		}
		for _, expr := range badExprs {
			_, err := ResolveString("{{ "+expr+" }}", ctx)
			if err == nil {
				t.Errorf("expected error for expression %q", expr)
			}
		}
	})

	t.Run("rejects deep JSON path", func(t *testing.T) {
		// Build a deeply nested JSON
		parts := make([]string, maxJSONPathDepth+1)
		for i := range parts {
			parts[i] = fmt.Sprintf("k%d", i)
		}
		deepPath := strings.Join(parts, ".")

		// Create a step with deeply nested JSON
		json := `{"k0":{"k1":{"k2":{"k3":{"k4":{"k5":{"k6":{"k7":{"k8":{"k9":{"k10":"deep"}}}}}}}}}}}`
		deepCtx := &TemplateContext{
			Steps: map[string]*StepResult{
				"deep": NewStepResult(json, false),
			},
		}

		_, err := ResolveString("{{ steps.deep.json."+deepPath+" }}", deepCtx)
		if err == nil {
			t.Error("expected error for deep JSON path")
		}
		if !strings.Contains(err.Error(), "exceeds maximum depth") {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestResolveString_EdgeCases(t *testing.T) {
	ctx := &TemplateContext{
		Inputs: map[string]any{
			"zero":       0,
			"empty":      "",
			"null_value": nil,
		},
		Steps: map[string]*StepResult{
			"json_array": NewStepResult(`[1, 2, 3]`, false),
		},
	}

	tests := []struct {
		name  string
		input string
		want  any
	}{
		{
			name:  "zero value input",
			input: "{{ inputs.zero }}",
			want:  0,
		},
		{
			name:  "empty string input",
			input: "{{ inputs.empty }}",
			want:  "",
		},
		{
			name:  "nil input value",
			input: "{{ inputs.null_value }}",
			want:  nil,
		},
		{
			name:  "JSON array root access",
			input: "{{ steps.json_array.json.0 }}",
			want:  float64(1),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveString(tt.input, ctx)
			if err != nil {
				t.Fatalf("ResolveString() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("ResolveString() = %v (%T), want %v (%T)", got, got, tt.want, tt.want)
			}
		})
	}
}
