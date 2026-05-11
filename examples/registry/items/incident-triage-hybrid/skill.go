// Incident-triage-hybrid is the hybrid-pattern reference: a Go skill whose
// handler reads its own SKILL.md body — the severity matrix, decision rules,
// runbook, and stakeholder template — and feeds it as the LLM system prompt.
// The "hybrid" label is literal: code is the runtime, prose is the canon,
// and ctx.SkillBody() is the glue between them.
//
// Edit SKILL.md to evolve the matrix or the runbook. The handler does not
// need to change. That is the whole point of the pattern.
//
// Built via 'gridctl agent build incident-triage-hybrid' as a Go plugin
// (-buildmode=plugin); the gateway opens the resulting skill.so at start.
// The fallacy of the graph applies — code is canon. The body is also canon.
package main

import (
	"fmt"
	"strings"

	"github.com/gridctl/gridctl/pkg/agent"
	"github.com/gridctl/gridctl/pkg/agent/llm"
	"github.com/gridctl/gridctl/pkg/agent/skill"
)

// TriageInput is the active-incident description the on-call submits.
type TriageInput struct {
	IncidentDescription string `json:"incident_description" jsonschema:"required,description=One-paragraph description of the active incident: what is failing, user-visible impact, and any signals already gathered"`
	AffectedService     string `json:"affected_service,omitempty" jsonschema:"description=Name of the service or subsystem affected (e.g. checkout-api, postgres-primary)"`
	Region              string `json:"region,omitempty" jsonschema:"description=Region or zone scope, if known (e.g. us-east-1)"`
}

// TriageOutput is the structured triage the war room consumes. Severity
// drives paging policy; immediate_action drives the next 60 seconds;
// stakeholder_message is the broadcast line for sev1 / sev2 (empty for
// sev3 / sev4 per the template in SKILL.md).
type TriageOutput struct {
	Severity            string `json:"severity"`
	ImmediateAction     string `json:"immediate_action"`
	StakeholderMessage  string `json:"stakeholder_message"`
}

// provider is the llm.Provider the runtime is expected to wire into the
// plugin at registration time. Kept package-scoped so run() can reference
// it without adding a generics-crossing accessor to RunContext. nil means
// "no provider wired" — run() falls back to a deterministic triage so the
// example exercises end-to-end without a vault-configured key.
var provider llm.Provider

// buildRequest constructs the agent.ChatRequest the runner sends. Extracted
// so unit tests can assert that ctx.SkillBody() carries the SKILL.md prose
// into the System slot verbatim — that is the hybrid contract this skill
// exists to demonstrate.
func buildRequest(ctx skill.RunContext, in TriageInput) agent.ChatRequest {
	system := ctx.SkillBody()
	if system == "" {
		// Programmatic registrations (tests, hand-built fixtures) may pass
		// an empty body. Surface a one-line stub so the wire shape stays
		// valid; the live matrix is in SKILL.md and reaches us through
		// ctx.SkillBody() in the gateway-loaded path.
		system = "Triage the incident; return a JSON object with severity, immediate_action, stakeholder_message."
	}
	service := in.AffectedService
	if service == "" {
		service = "unspecified"
	}
	region := in.Region
	if region == "" {
		region = "unspecified"
	}
	return agent.ChatRequest{
		Model:  "claude-sonnet-4-6",
		System: system,
		Messages: []agent.Message{
			{
				Role: agent.RoleUser,
				Content: fmt.Sprintf(
					"Skill: %s\nService: %s\nRegion: %s\n\nIncident: %s\n\nReturn one JSON object per the SKILL.md output contract.",
					ctx.SkillName(), service, region, in.IncidentDescription,
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
		// Fallback so 'gridctl run incident-triage-hybrid' returns a usable
		// shape before the runtime wires a provider. The matrix in
		// req.System (sourced from ctx.SkillBody()) would drive the live
		// classification — we surface its size so the operator can confirm
		// the body plumbed through end-to-end.
		return TriageOutput{
			Severity:        "sev3",
			ImmediateAction: "Page on-call; walk the matrix in SKILL.md.",
			StakeholderMessage: fmt.Sprintf(
				"No llm.Provider wired; SKILL.md matrix in skill.body (%d chars) would drive classification on a live run.",
				len(req.System),
			),
		}, nil
	}

	resp, err := provider.Generate(ctx, req)
	if err != nil {
		return TriageOutput{}, fmt.Errorf("triage generate: %w", err)
	}
	// Parsing the model's JSON response is downstream's job; surface the
	// content verbatim so the wire shape stays observable end-to-end.
	return TriageOutput{
		Severity:           "sev?",
		ImmediateAction:    resp.Content,
		StakeholderMessage: "",
	}, nil
}

// New constructs the typed Definition the registry server lifts into an MCP
// tool envelope. The body argument is "" — the gateway-builder loader
// re-decorates the Definition with the on-disk SKILL.md body via
// skill.WithSkillBody so ctx.SkillBody() returns the live prose.
func New() *skill.Definition {
	return skill.MustDefine[TriageInput, TriageOutput](
		"incident-triage-hybrid",
		"Triage an active incident against the SKILL.md severity matrix and runbook; return severity, immediate action, and stakeholder message.",
		"",
		run,
	)
}

// RegisterSkill is the plugin entry point the gateway-builder loader looks
// up after plugin.Open.
func RegisterSkill(reg *skill.Registry) error {
	return reg.Register(New())
}

// main is unused — this package is built as a Go plugin via
// 'go build -buildmode=plugin' and has no executable entry point.
func main() {}
