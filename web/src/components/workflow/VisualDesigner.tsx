import { useState, useCallback, useMemo, useEffect } from 'react';
import { ReactFlowProvider } from '@xyflow/react';
import { ToolboxPalette } from './ToolboxPalette';
import { DesignerGraph } from './DesignerGraph';
import { DesignerInspector } from './DesignerInspector';
import { Loader2 } from 'lucide-react';
import { fetchTools } from '../../lib/api';
import { showToast } from '../ui/Toast';
import type { WorkflowStep, SkillInput, WorkflowOutput, Tool } from '../../types';

interface VisualDesignerProps {
  steps: WorkflowStep[];
  inputs: Record<string, SkillInput>;
  output: WorkflowOutput | undefined;
  onStepsChange: (steps: WorkflowStep[]) => void;
  onInputsChange: (inputs: Record<string, SkillInput>) => void;
  onOutputChange: (output: WorkflowOutput | undefined) => void;
}

export function VisualDesigner({
  steps,
  inputs,
  output,
  onStepsChange,
  onInputsChange,
  onOutputChange,
}: VisualDesignerProps) {
  const [selectedStepId, setSelectedStepId] = useState<string | null>(null);
  const [tools, setTools] = useState<Tool[]>([]);
  const [toolsLoading, setToolsLoading] = useState(true);

  // Load available tools
  useEffect(() => {
    let cancelled = false;
    const load = async () => {
      try {
        const result = await fetchTools();
        if (!cancelled) setTools(result.tools ?? []);
      } catch {
        // Tools loading is best-effort
      } finally {
        if (!cancelled) setToolsLoading(false);
      }
    };
    load();
    return () => { cancelled = true; };
  }, []);

  // Generate a unique step ID from tool name
  const generateStepId = useCallback(
    (tool: string): string => {
      const base = (tool.split('__').pop() ?? 'step').replace(/[^a-z0-9]/g, '-');
      const existingIds = new Set(steps.map((s) => s.id));
      let id = base;
      let counter = 1;
      while (existingIds.has(id)) {
        id = `${base}-${counter}`;
        counter++;
      }
      return id;
    },
    [steps],
  );

  // Add a new step from toolbox drag
  const handleAddStep = useCallback(
    (tool: string, _position: { x: number; y: number }) => {
      const id = generateStepId(tool);
      const newStep: WorkflowStep = { id, tool };
      onStepsChange([...steps, newStep]);
      setSelectedStepId(id);
      showToast('success', `Step "${id}" added`);
    },
    [steps, onStepsChange, generateStepId],
  );

  // Delete a step
  const handleDeleteStep = useCallback(
    (id: string) => {
      // Remove the step and clean up references
      const filtered = steps
        .filter((s) => s.id !== id)
        .map((s) => ({
          ...s,
          dependsOn: (s.dependsOn ?? []).filter((d) => d !== id),
        }))
        .map((s) => ({
          ...s,
          dependsOn: (s.dependsOn ?? []).length > 0 ? s.dependsOn : undefined,
        }));
      onStepsChange(filtered);
      if (selectedStepId === id) setSelectedStepId(null);
    },
    [steps, onStepsChange, selectedStepId],
  );

  // Update a step from inspector
  const handleStepChange = useCallback(
    (updated: WorkflowStep) => {
      const idx = steps.findIndex((s) => s.id === selectedStepId);
      if (idx === -1) return;

      const newSteps = [...steps];
      // If ID changed, update references
      if (updated.id !== selectedStepId) {
        const oldId = selectedStepId!;
        newSteps[idx] = updated;
        // Update depends_on references in other steps
        for (let i = 0; i < newSteps.length; i++) {
          if (i === idx) continue;
          const deps = newSteps[i].dependsOn;
          if (deps?.includes(oldId)) {
            newSteps[i] = {
              ...newSteps[i],
              dependsOn: deps.map((d) => (d === oldId ? updated.id : d)),
            };
          }
        }
        setSelectedStepId(updated.id);
      } else {
        newSteps[idx] = updated;
      }
      onStepsChange(newSteps);
    },
    [steps, onStepsChange, selectedStepId],
  );

  const selectedStep = useMemo(
    () => (steps ?? []).find((s) => s.id === selectedStepId) ?? null,
    [steps, selectedStepId],
  );

  const stepIds = useMemo(() => (steps ?? []).map((s) => s.id), [steps]);

  // Keyboard shortcuts
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && selectedStepId) {
        setSelectedStepId(null);
      }
    };
    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [selectedStepId]);

  if (toolsLoading) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <Loader2 size={20} className="text-primary animate-spin" />
      </div>
    );
  }

  return (
    <div className="flex-1 flex min-h-0">
      {/* Left: Toolbox palette */}
      <ToolboxPalette
        tools={tools}
        inputs={inputs}
        output={output}
        onInputsChange={onInputsChange}
        onOutputChange={onOutputChange}
        stepIds={stepIds}
      />

      {/* Center: Canvas */}
      <div className="flex-1 min-w-0 min-h-0">
        <ReactFlowProvider>
          <DesignerGraph
            steps={steps}
            selectedStepId={selectedStepId}
            onSelectStep={setSelectedStepId}
            onStepsChange={onStepsChange}
            onAddStep={handleAddStep}
            onDeleteStep={handleDeleteStep}
          />
        </ReactFlowProvider>
      </div>

      {/* Right: Inspector (when step selected) */}
      {selectedStep && (
        <DesignerInspector
          step={selectedStep}
          allSteps={steps}
          inputs={inputs}
          tools={tools}
          onChange={handleStepChange}
          onClose={() => setSelectedStepId(null)}
        />
      )}
    </div>
  );
}
