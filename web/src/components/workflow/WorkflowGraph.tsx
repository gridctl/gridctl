import { useMemo, useCallback, useEffect, useRef } from 'react';
import {
  ReactFlow,
  Background,
  Controls,
  useReactFlow,
  type Node,
  type Edge,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import { StepNode } from './StepNode';
import { useWorkflowStore } from '../../stores/useWorkflowStore';
import type { WorkflowDefinition } from '../../types';

const nodeTypes = { stepNode: StepNode };

const LEVEL_GAP = 120;
const STEP_GAP = 260;

function buildFlowElements(
  definition: WorkflowDefinition,
  stepStatuses: Record<string, string>,
  selectedStepId: string | null,
  executionSteps?: { id: string; durationMs?: number; error?: string }[],
): { nodes: Node[]; edges: Edge[] } {
  const nodes: Node[] = [];
  const edges: Edge[] = [];

  // Build duration/error lookup from execution results
  const stepMeta: Record<string, { durationMs?: number; error?: string }> = {};
  for (const s of executionSteps ?? []) {
    stepMeta[s.id] = { durationMs: s.durationMs, error: s.error };
  }

  (definition.dag?.levels ?? []).forEach((level, levelIdx) => {
    const levelWidth = (level ?? []).length * STEP_GAP;
    const startX = -levelWidth / 2 + STEP_GAP / 2;

    (level ?? []).forEach((step, stepIdx) => {
      nodes.push({
        id: step.id,
        type: 'stepNode',
        position: { x: startX + stepIdx * STEP_GAP, y: levelIdx * LEVEL_GAP },
        data: {
          stepId: step.id,
          tool: step.tool,
          status: stepStatuses[step.id] ?? 'pending',
          hasCondition: !!step.condition,
          hasRetry: !!step.retry,
          onError: step.onError,
          isSkillCall: step.tool?.startsWith('registry__') ?? false,
          selected: step.id === selectedStepId,
          durationMs: stepMeta[step.id]?.durationMs,
          error: stepMeta[step.id]?.error,
        },
      });

      (step.dependsOn ?? []).forEach((depId) => {
        const depStatus = stepStatuses[depId] ?? 'pending';
        const targetStatus = stepStatuses[step.id] ?? 'pending';
        const isActive = targetStatus === 'running';
        edges.push({
          id: `${depId}-${step.id}`,
          source: depId,
          target: step.id,
          type: 'smoothstep',
          animated: depStatus === 'running',
          className: isActive ? 'workflow-edge-active' : undefined,
          style: {
            stroke:
              depStatus === 'success' ? '#10b981' :
              depStatus === 'failed' ? '#f43f5e' :
              '#27272a',
            strokeWidth: 1.5,
            ...(isActive && { strokeDasharray: '5 5' }),
          },
        });
      });
    });
  });

  return { nodes, edges };
}

export function WorkflowGraph() {
  const definition = useWorkflowStore((s) => s.definition);
  const stepStatuses = useWorkflowStore((s) => s.stepStatuses);
  const selectedStepId = useWorkflowStore((s) => s.selectedStepId);
  const setSelectedStep = useWorkflowStore((s) => s.setSelectedStep);
  const followMode = useWorkflowStore((s) => s.followMode);
  const executing = useWorkflowStore((s) => s.executing);
  const execution = useWorkflowStore((s) => s.execution);

  const { nodes, edges } = useMemo(() => {
    if (!definition) return { nodes: [], edges: [] };
    return buildFlowElements(
      definition,
      stepStatuses,
      selectedStepId,
      execution?.steps,
    );
  }, [definition, stepStatuses, selectedStepId, execution]);

  const handleNodeClick = useCallback(
    (_: React.MouseEvent, node: Node) => {
      setSelectedStep(node.id === selectedStepId ? null : node.id);
    },
    [setSelectedStep, selectedStepId],
  );

  const handlePaneClick = useCallback(() => {
    setSelectedStep(null);
  }, [setSelectedStep]);

  if (!definition) {
    return (
      <div className="flex items-center justify-center h-full text-text-muted text-sm">
        No workflow definition loaded
      </div>
    );
  }

  return (
    <div className="w-full h-full">
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        onNodeClick={handleNodeClick}
        onPaneClick={handlePaneClick}
        fitView
        fitViewOptions={{ padding: 0.3 }}
        proOptions={{ hideAttribution: true }}
        minZoom={0.3}
        maxZoom={2}
        defaultEdgeOptions={{ type: 'smoothstep' }}
        nodesDraggable={false}
        nodesConnectable={false}
      >
        <Background color="#27272a" gap={20} size={1} />
        <Controls
          showInteractive={false}
          className="!bg-surface-elevated/80 !border-border/40 !rounded-lg !shadow-node"
        />
        {followMode && executing && <FollowModeEffect stepStatuses={stepStatuses} />}
      </ReactFlow>
    </div>
  );
}

// Follow mode: auto-fit to running steps
function FollowModeEffect({ stepStatuses }: { stepStatuses: Record<string, string> }) {
  const { fitView } = useReactFlow();
  const prevRunning = useRef<string[]>([]);

  useEffect(() => {
    const running = Object.entries(stepStatuses)
      .filter(([, status]) => status === 'running')
      .map(([id]) => id);

    // Only re-fit when the set of running steps changes
    if (
      running.length > 0 &&
      (running.length !== prevRunning.current.length ||
        running.some((id) => !prevRunning.current.includes(id)))
    ) {
      prevRunning.current = running;
      fitView({ nodes: running.map((id) => ({ id })), padding: 0.5, duration: 300 });
    }
  }, [stepStatuses, fitView]);

  return null;
}
