// Hybrid pattern in TS — skill.body is the SKILL.md severity matrix above
// the frontmatter; passing it to llm({ system }) means the prose is canon
// and the handler stays thin. Edit SKILL.md and the next run picks up the
// new matrix without a code change.
//
// The fallacy of the graph applies — code is canon. For the hybrid pattern,
// the markdown body is also canon: it drives behaviour through the system
// slot, not through string concatenation in the handler.
import { llm, skill } from "@gridctl/agent";

export interface TriageInput {
  incident_description: string;
  affected_system?: string;
}

export interface TriageOutput {
  severity: string;
  next_action: string;
  rationale: string;
}

export default async function run(input: TriageInput): Promise<TriageOutput> {
  if (!input.incident_description || input.incident_description.trim() === "") {
    throw new Error("incident_description is required");
  }

  const reply = await llm({
    model: "claude-sonnet-4-6",
    system: skill.body,
    messages: [
      {
        role: "user",
        content: [
          `Skill: ${skill.name}`,
          `Affected system: ${input.affected_system ?? "unspecified"}`,
          ``,
          `Incident: ${input.incident_description}`,
          ``,
          `Return one JSON object with severity, next_action, rationale.`,
        ].join("\n"),
      },
    ],
  });

  // The model returns JSON-shaped prose per the SKILL.md contract; downstream
  // code parses it. The example surfaces the raw content so the wire shape
  // stays observable end-to-end without taking a JSON-parse dependency on
  // model behaviour.
  return {
    severity: "sev?",
    next_action: reply.content ?? "",
    rationale: `Routed via ${skill.name}; severity matrix in skill.body (${skill.body.length} chars).`,
  };
}
