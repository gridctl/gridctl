import type { WorkflowStep, SkillInput, WorkflowOutput } from '../types';

// Check if content contains a workflow block
export function hasWorkflowBlock(content: string): boolean {
  return /^workflow:/m.test(content) || /\nworkflow:/m.test(content);
}

// Serialize workflow steps back to YAML-compatible object for frontmatter reconstruction.
// This builds the structured data that gets merged back into the frontmatter.
export function workflowToYAMLFields(
  steps: WorkflowStep[],
  inputs: Record<string, SkillInput>,
  output: WorkflowOutput | undefined,
): { workflow: unknown[]; inputs?: Record<string, unknown>; output?: Record<string, unknown> } {
  const workflow = steps.map((step) => {
    const entry: Record<string, unknown> = {
      id: step.id,
      tool: step.tool,
    };
    if (step.args && Object.keys(step.args).length > 0) {
      entry.args = step.args;
    }
    if ((step.dependsOn ?? []).length > 0) {
      entry.depends_on = step.dependsOn;
    }
    if (step.condition) entry.condition = step.condition;
    if (step.onError && step.onError !== 'fail') entry.on_error = step.onError;
    if (step.timeout) entry.timeout = step.timeout;
    if (step.retry) {
      entry.retry = {
        max_attempts: step.retry.maxAttempts,
        ...(step.retry.backoff && { backoff: step.retry.backoff }),
      };
    }
    return entry;
  });

  const result: { workflow: unknown[]; inputs?: Record<string, unknown>; output?: Record<string, unknown> } = { workflow };

  if (Object.keys(inputs).length > 0) {
    const inputsYAML: Record<string, unknown> = {};
    for (const [name, input] of Object.entries(inputs)) {
      const entry: Record<string, unknown> = { type: input.type };
      if (input.description) entry.description = input.description;
      if (input.required) entry.required = true;
      if (input.default != null) entry.default = input.default;
      if ((input.enum ?? []).length > 0) entry.enum = input.enum;
      inputsYAML[name] = entry;
    }
    result.inputs = inputsYAML;
  }

  if (output) {
    const outputYAML: Record<string, unknown> = {};
    if (output.format) outputYAML.format = output.format;
    if ((output.include ?? []).length > 0) outputYAML.include = output.include;
    if (output.template) outputYAML.template = output.template;
    if (Object.keys(outputYAML).length > 0) {
      result.output = outputYAML;
    }
  }

  return result;
}
