package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// fakeRunContext satisfies skill.RunContext for unit tests. Mirrors the
// helper the scaffold ships in skill_test.go — same fixture pattern across
// every typed Go skill.
type fakeRunContext struct {
	context.Context
	body string
	name string
}

func (f fakeRunContext) SkillBody() string { return f.body }
func (f fakeRunContext) SkillName() string { return f.name }

func TestRun_RejectsEmptyDescription(t *testing.T) {
	rc := fakeRunContext{Context: context.Background(), name: "triage-go"}
	if _, err := run(rc, TriageInput{IncidentDescription: ""}); err == nil {
		t.Error("expected error for empty incident_description")
	}
}

func TestRun_FallbackTriageWhenNoProvider(t *testing.T) {
	rc := fakeRunContext{Context: context.Background(), body: "matrix prose", name: "triage-go"}
	out, err := run(rc, TriageInput{IncidentDescription: "API returning 500s for all users"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.Severity == "" || out.NextAction == "" {
		t.Errorf("expected populated triage, got %+v", out)
	}
}

func TestBuildRequest_CarriesSkillBodyAsSystem(t *testing.T) {
	body := "## Severity matrix\n\nsev1: customer-facing outage..."
	rc := fakeRunContext{Context: context.Background(), body: body, name: "triage-go"}
	req := buildRequest(rc, TriageInput{IncidentDescription: "checkout broken"})
	if req.System != body {
		t.Errorf("System != skill body\n got: %q\nwant: %q", req.System, body)
	}
	if !strings.Contains(req.Messages[0].Content, "triage-go") {
		t.Errorf("user message missing skill name: %q", req.Messages[0].Content)
	}
}

func TestNew_ProducesDispatchableDefinition(t *testing.T) {
	def := New()
	if def == nil {
		t.Fatal("New: definition is nil")
	}
	if def.Name != "triage-go" {
		t.Errorf("definition name = %q, want triage-go", def.Name)
	}
	res, err := def.Invoker(context.Background(), map[string]any{
		"incident_description": "elevated latency on checkout",
	})
	if err != nil {
		t.Fatalf("Invoker: %v", err)
	}
	if res == nil || len(res.Content) != 1 {
		t.Fatalf("expected one content item, got %+v", res)
	}
	var out TriageOutput
	if err := json.Unmarshal([]byte(res.Content[0].Text), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Severity == "" {
		t.Error("severity is empty")
	}
}
