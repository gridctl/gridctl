// Triage-go is the typed Go counterpart to examples/registry/items/triage-ts.
// Built via 'gridctl agent build triage-go' as a Go plugin (-buildmode=plugin);
// the gateway opens the resulting skill.so at start and calls RegisterSkill
// against the shared *skill.Registry.
//
// The graph: validate input -> classify against the SKILL.md severity matrix
// (delivered through ctx.SkillBody() so the prose stays in SKILL.md, not in
// this file) -> return a structured triage. The fallacy of the graph applies
// — code is canon. The body is also canon for the hybrid pattern.
package main

import (
	"fmt"
	"strings"

	"github.com/gridctl/gridctl/pkg/agent"
	"github.com/gridctl/gridctl/pkg/agent/llm"
	"github.com/gridctl/gridctl/pkg/agent/skill"
)

// TriageInput is the typed input shape. The jsonschema tag drives the
// schema the gateway hands to upstream MCP clients; the json tag is the
// wire form on the way in.
type TriageInput struct {
	IncidentDescription string `json:"incident_description" jsonschema:"required,description=One-paragraph description of the incident: what's failing and the user-visible impact"`
	AffectedSystem      string `json:"affected_system,omitempty" jsonschema:"description=System or service affected (e.g. api-gateway, postgres-primary). Optional."`
}

// TriageOutput is the structured triage. Severity is one of
// sev1 / sev2 / sev3 / sev4 per the matrix in SKILL.md.
type TriageOutput struct {
	Severity   string `json:"severity"`
	NextAction string `json:"next_action"`
	Rationale  string `json:"rationale"`
}

// provider is the llm.Provider the runtime is expected to wire into the
// plugin at registration time. Kept package-scoped so the run() body can
// reference it without adding a generics-crossing accessor to RunContext.
// nil means "no provider wired" — run() falls back to a deterministic
// triage so the example exercises end-to-end without a vault-configured key.
var provider llm.Provider

// buildRequest constructs the agent.ChatRequest the runner sends. Extracted
// so unit tests can assert the System slot carries ctx.SkillBody() verbatim
// — that's the hybrid contract this skill exists to demonstrate.
func buildRequest(ctx skill.RunContext, in TriageInput) agent.ChatRequest {
	system := ctx.SkillBody()
	if system == "" {
		// Programmatic registrations (tests, hand-built fixtures) may pass
		// an empty body. Fall back to a one-line stub so the wire shape
		// stays valid; the matrix in SKILL.md is still the canonical source
		// for non-test paths.
		system = "Classify the incident; return a JSON object with severity, next_action, rationale."
	}
	affected := in.AffectedSystem
	if affected == "" {
		affected = "unspecified"
	}
	return agent.ChatRequest{
		Model:  "claude-sonnet-4-6",
		System: system,
		Messages: []agent.Message{
			{
				Role: agent.RoleUser,
				Content: fmt.Sprintf(
					"Skill: %s\nAffected system: %s\n\nIncident: %s\n\nReturn one JSON object with severity, next_action, rationale.",
					ctx.SkillName(), affected, in.IncidentDescription,
				),
			},
		},
	}
}

func run(ctx skill.RunContext, in TriageInput) (TriageOutput, error) {
	if strings.TrimSpace(in.IncidentDescription) == "" {
		return TriageOutput{}, fmt.Errorf("incident_description is required")
	}

	req := buildRequest(ctx, in)
	if provider == nil {
		// Fallback path so 'gridctl run triage-go' returns a usable shape
		// even before the runtime wires a provider. Live runs hit the
		// branch below.
		return TriageOutput{
			Severity:   "sev3",
			NextAction: "Page on-call and walk the matrix in SKILL.md.",
			Rationale: fmt.Sprintf(
				"No llm.Provider wired; matrix in skill.body (%d chars) would drive classification on a live run.",
				len(req.System),
			),
		}, nil
	}

	resp, err := provider.Generate(ctx, req)
	if err != nil {
		return TriageOutput{}, fmt.Errorf("triage generate: %w", err)
	}
	return TriageOutput{
		Severity:   "sev?",
		NextAction: resp.Content,
		Rationale:  fmt.Sprintf("Routed via %s; severity matrix in skill.body.", ctx.SkillName()),
	}, nil
}

// New constructs the typed Definition the registry server lifts into an MCP
// tool envelope. The body argument stays "" — the gateway-builder loader
// re-decorates the Definition with the on-disk SKILL.md body via
// skill.WithSkillBody so ctx.SkillBody() returns the live prose.
func New() *skill.Definition {
	return skill.MustDefine[TriageInput, TriageOutput](
		"triage-go",
		"Classify an incident against the SKILL.md severity matrix and return the first runbook step.",
		"",
		run,
	)
}

// RegisterSkill is the plugin entry point the gateway-builder loader looks
// up after plugin.Open. The loader hands the plugin the shared
// *skill.Registry; the plugin registers each skill it owns.
func RegisterSkill(reg *skill.Registry) error {
	return reg.Register(New())
}

// main is unused — this package is built as a Go plugin via
// 'go build -buildmode=plugin' and has no executable entry point. The
// empty main() lets plain 'go build' compile-check the source.
func main() {}
