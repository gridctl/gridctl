package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// fakeRunContext satisfies skill.RunContext for unit tests. The body
// fixture stands in for the SKILL.md prose the registry would parse on a
// live load.
type fakeRunContext struct {
	context.Context
	body string
	name string
}

func (f fakeRunContext) SkillBody() string { return f.body }
func (f fakeRunContext) SkillName() string { return f.name }

const fixtureBody = `## Severity matrix

- sev1: customer-facing total outage
- sev2: significant degradation
- sev3: bounded impact
- sev4: internal noise

## Runbook

1. Acknowledge in #incidents.
2. Page on-call per the matrix.
3. Begin runbook step appropriate to severity.
`

func TestRun_RejectsEmptyDescription(t *testing.T) {
	rc := fakeRunContext{Context: context.Background(), body: fixtureBody, name: "incident-triage-hybrid"}
	if _, err := run(rc, TriageInput{IncidentDescription: ""}); err == nil {
		t.Error("expected error for empty incident_description")
	}
}

// TestBuildRequest_HybridContract is the load-bearing test: it proves that
// ctx.SkillBody() carries the SKILL.md prose into agent.ChatRequest.System
// verbatim. If the hybrid contract ever regresses, this test breaks.
func TestBuildRequest_HybridContract(t *testing.T) {
	rc := fakeRunContext{Context: context.Background(), body: fixtureBody, name: "incident-triage-hybrid"}
	req := buildRequest(rc, TriageInput{
		IncidentDescription: "checkout 5xx for all users in us-east-1",
		AffectedService:     "checkout-api",
		Region:              "us-east-1",
	})
	if req.System != fixtureBody {
		t.Errorf("System slot did not match skill body verbatim\n got: %q\nwant: %q", req.System, fixtureBody)
	}
	if !strings.Contains(req.Messages[0].Content, "incident-triage-hybrid") {
		t.Errorf("user message missing skill name: %q", req.Messages[0].Content)
	}
	if !strings.Contains(req.Messages[0].Content, "checkout-api") {
		t.Errorf("user message missing affected service: %q", req.Messages[0].Content)
	}
	if !strings.Contains(req.Messages[0].Content, "us-east-1") {
		t.Errorf("user message missing region: %q", req.Messages[0].Content)
	}
}

func TestBuildRequest_EmptyBodyFallsBackToStub(t *testing.T) {
	rc := fakeRunContext{Context: context.Background(), body: "", name: "incident-triage-hybrid"}
	req := buildRequest(rc, TriageInput{IncidentDescription: "any"})
	if req.System == "" {
		t.Error("expected System to fall back to a stub when body is empty, got empty string")
	}
}

func TestNew_ProducesDispatchableDefinition(t *testing.T) {
	def := New()
	if def == nil {
		t.Fatal("New: definition is nil")
	}
	if def.Name != "incident-triage-hybrid" {
		t.Errorf("definition name = %q, want incident-triage-hybrid", def.Name)
	}
	res, err := def.Invoker(context.Background(), map[string]any{
		"incident_description": "checkout failing for all users",
		"affected_service":     "checkout-api",
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
	if out.Severity == "" || out.ImmediateAction == "" {
		t.Errorf("expected populated triage, got %+v", out)
	}
}
