import { GitBranch } from 'lucide-react';

const WORKFLOW_TEMPLATE = `inputs:
  param1:
    type: string
    description: First parameter
    required: true

workflow:
  - id: step-1
    tool: server__tool_name
    args:
      key: "{{ inputs.param1 }}"

output:
  format: merged`;

interface WorkflowEmptyStateProps {
  onAddTemplate?: (template: string) => void;
}

export function WorkflowEmptyState({ onAddTemplate }: WorkflowEmptyStateProps) {
  return (
    <div className="flex flex-col items-center justify-center h-64 text-center">
      <GitBranch size={32} className="text-text-muted/30 mb-3" />
      <p className="text-sm text-text-secondary mb-1">No workflow defined</p>
      <p className="text-xs text-text-muted mb-4">
        Add a <code className="text-primary bg-primary/10 px-1 rounded">workflow:</code> block to the YAML frontmatter to make this skill executable.
      </p>
      {onAddTemplate && (
        <button
          onClick={() => onAddTemplate(WORKFLOW_TEMPLATE)}
          className="btn-secondary text-xs px-3 py-1.5"
        >
          Add Workflow Template
        </button>
      )}
    </div>
  );
}
