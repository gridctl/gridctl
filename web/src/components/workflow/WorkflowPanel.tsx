import { useEffect } from 'react';
import { ReactFlowProvider } from '@xyflow/react';
import { WorkflowGraph } from './WorkflowGraph';
import { WorkflowInspector } from './WorkflowInspector';
import { WorkflowRunner } from './WorkflowRunner';
import { useWorkflowStore } from '../../stores/useWorkflowStore';
import { Loader2, AlertCircle } from 'lucide-react';

interface WorkflowPanelProps {
  skillName: string;
}

export function WorkflowPanel({ skillName }: WorkflowPanelProps) {
  const loading = useWorkflowStore((s) => s.loading);
  const error = useWorkflowStore((s) => s.error);
  const definition = useWorkflowStore((s) => s.definition);
  const loadWorkflow = useWorkflowStore((s) => s.loadWorkflow);
  const selectedStepId = useWorkflowStore((s) => s.selectedStepId);
  const setSelectedStep = useWorkflowStore((s) => s.setSelectedStep);

  useEffect(() => {
    loadWorkflow(skillName);
  }, [skillName, loadWorkflow]);

  // Escape key deselects step
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && selectedStepId) {
        e.preventDefault();
        setSelectedStep(null);
      }
      // 'f' toggles follow mode (only when not in input)
      if (e.key === 'f' && !isInputElement(e.target as HTMLElement)) {
        e.preventDefault();
        useWorkflowStore.getState().toggleFollowMode();
      }
    };
    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [selectedStepId, setSelectedStep]);

  if (loading) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <div className="text-center space-y-3">
          <Loader2 size={24} className="text-primary animate-spin mx-auto" />
          <p className="text-sm text-text-muted">Loading workflow...</p>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <div className="text-center p-6 max-w-sm">
          <AlertCircle size={24} className="text-status-error mx-auto mb-3" />
          <p className="text-sm text-status-error mb-3">{error}</p>
          <button
            onClick={() => loadWorkflow(skillName)}
            className="btn-secondary text-xs py-1.5 px-4"
          >
            Retry
          </button>
        </div>
      </div>
    );
  }

  if (!definition) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <p className="text-sm text-text-muted">No workflow definition found</p>
      </div>
    );
  }

  return (
    <div className="flex-1 flex flex-col min-h-0">
      <div className="flex-1 flex min-h-0">
        {/* Graph area */}
        <div className="flex-1 min-w-0 min-h-0">
          <ReactFlowProvider>
            <WorkflowGraph />
          </ReactFlowProvider>
        </div>

        {/* Inspector (right panel, shown when step selected) */}
        {selectedStepId && <WorkflowInspector />}
      </div>

      {/* Runner (bottom) */}
      <div className="max-h-[40%] min-h-[120px] overflow-hidden flex flex-col">
        <WorkflowRunner />
      </div>
    </div>
  );
}

function isInputElement(el: HTMLElement): boolean {
  const tag = el.tagName?.toLowerCase();
  return tag === 'input' || tag === 'textarea' || tag === 'select' || el.isContentEditable;
}
