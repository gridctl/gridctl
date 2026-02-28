import { useCallback, useMemo, useRef } from 'react';
import {
  ReactFlow,
  Background,
  Controls,
  useNodesState,
  useEdgesState,
  addEdge,
  type Node,
  type Edge,
  type Connection,
  type OnConnect,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import { AlignCenter } from 'lucide-react';
import { StepNode } from './StepNode';
import { showToast } from '../ui/Toast';
import type { WorkflowStep } from '../../types';

const nodeTypes = { stepNode: StepNode };

const LEVEL_GAP = 120;
const STEP_GAP = 260;

interface DesignerGraphProps {
  steps: WorkflowStep[];
  selectedStepId: string | null;
  onSelectStep: (id: string | null) => void;
  onStepsChange: (steps: WorkflowStep[]) => void;
  onAddStep: (tool: string, position: { x: number; y: number }) => void;
  onDeleteStep: (id: string) => void;
}

// Build React Flow elements from workflow steps
function stepsToFlow(
  steps: WorkflowStep[],
  selectedStepId: string | null,
): { nodes: Node[]; edges: Edge[] } {
  const nodes: Node[] = [];
  const edges: Edge[] = [];

  // Simple topological sort into levels
  const levels = topoSort(steps);

  levels.forEach((level, levelIdx) => {
    const levelWidth = level.length * STEP_GAP;
    const startX = -levelWidth / 2 + STEP_GAP / 2;

    level.forEach((step, stepIdx) => {
      nodes.push({
        id: step.id,
        type: 'stepNode',
        position: { x: startX + stepIdx * STEP_GAP, y: levelIdx * LEVEL_GAP },
        data: {
          stepId: step.id,
          tool: step.tool,
          status: 'pending',
          hasCondition: !!step.condition,
          hasRetry: !!step.retry,
          onError: step.onError,
          isSkillCall: step.tool?.startsWith('registry__') ?? false,
          selected: step.id === selectedStepId,
        },
      });

      (step.dependsOn ?? []).forEach((depId) => {
        edges.push({
          id: `${depId}-${step.id}`,
          source: depId,
          target: step.id,
          type: 'smoothstep',
          style: { stroke: '#27272a', strokeWidth: 1.5 },
        });
      });
    });
  });

  return { nodes, edges };
}

// Simple topological sort into levels using Kahn's algorithm
function topoSort(steps: WorkflowStep[]): WorkflowStep[][] {
  if (steps.length === 0) return [];

  const stepById = new Map(steps.map((s) => [s.id, s]));
  const inDegree = new Map(steps.map((s) => [s.id, 0]));

  for (const step of steps) {
    for (const dep of step.dependsOn ?? []) {
      if (stepById.has(dep)) {
        inDegree.set(step.id, (inDegree.get(step.id) ?? 0) + 1);
      }
    }
  }

  const levels: WorkflowStep[][] = [];
  let queue = steps.filter((s) => (inDegree.get(s.id) ?? 0) === 0);

  while (queue.length > 0) {
    levels.push(queue);
    const nextQueue: WorkflowStep[] = [];
    for (const s of queue) {
      for (const step of steps) {
        if ((step.dependsOn ?? []).includes(s.id)) {
          const deg = (inDegree.get(step.id) ?? 1) - 1;
          inDegree.set(step.id, deg);
          if (deg === 0) nextQueue.push(step);
        }
      }
    }
    queue = nextQueue;
  }

  // Add any remaining steps (in case of cycles)
  const placed = new Set(levels.flat().map((s) => s.id));
  const remaining = steps.filter((s) => !placed.has(s.id));
  if (remaining.length > 0) levels.push(remaining);

  return levels;
}

export function DesignerGraph({
  steps,
  selectedStepId,
  onSelectStep,
  onStepsChange,
  onAddStep,
  onDeleteStep,
}: DesignerGraphProps) {
  const reactFlowWrapper = useRef<HTMLDivElement>(null);

  const { nodes: initialNodes, edges: initialEdges } = useMemo(
    () => stepsToFlow(steps, selectedStepId),
    [steps, selectedStepId],
  );

  const [nodes, setNodes, onNodesChange] = useNodesState(initialNodes);
  const [edges, setEdges, onEdgesChange] = useEdgesState(initialEdges);

  // Sync when steps change externally
  useMemo(() => {
    const { nodes: newNodes, edges: newEdges } = stepsToFlow(steps, selectedStepId);
    setNodes(newNodes);
    setEdges(newEdges);
  }, [steps, selectedStepId, setNodes, setEdges]);

  // Handle new edge creation (depends_on)
  const onConnect: OnConnect = useCallback(
    (connection: Connection) => {
      if (!connection.source || !connection.target) return;
      // Update step dependencies
      const updatedSteps = steps.map((s) => {
        if (s.id === connection.target) {
          const deps = [...(s.dependsOn ?? [])];
          if (!deps.includes(connection.source!)) {
            deps.push(connection.source!);
          }
          return { ...s, dependsOn: deps };
        }
        return s;
      });
      onStepsChange(updatedSteps);
      setEdges((eds) => addEdge({ ...connection, type: 'smoothstep', style: { stroke: '#27272a', strokeWidth: 1.5 } }, eds));
    },
    [steps, onStepsChange, setEdges],
  );

  // Handle node click
  const handleNodeClick = useCallback(
    (_: React.MouseEvent, node: Node) => {
      onSelectStep(node.id === selectedStepId ? null : node.id);
    },
    [onSelectStep, selectedStepId],
  );

  // Handle pane click (deselect)
  const handlePaneClick = useCallback(() => {
    onSelectStep(null);
  }, [onSelectStep]);

  // Handle drop from toolbox
  const handleDragOver = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    e.dataTransfer.dropEffect = 'move';
  }, []);

  const handleDrop = useCallback(
    (e: React.DragEvent) => {
      e.preventDefault();
      const data = e.dataTransfer.getData('application/reactflow');
      if (!data) return;

      try {
        const { tool } = JSON.parse(data);
        if (!tool) return;

        const bounds = reactFlowWrapper.current?.getBoundingClientRect();
        if (!bounds) return;

        const position = {
          x: e.clientX - bounds.left - 110,
          y: e.clientY - bounds.top - 40,
        };

        onAddStep(tool, position);
      } catch {
        // Invalid drag data
      }
    },
    [onAddStep],
  );

  // Handle keyboard shortcuts
  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if ((e.key === 'Delete' || e.key === 'Backspace') && selectedStepId) {
        const target = e.target as HTMLElement;
        if (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.tagName === 'SELECT') return;
        e.preventDefault();
        onDeleteStep(selectedStepId);
        showToast('success', 'Step removed');
      }
    },
    [selectedStepId, onDeleteStep],
  );

  // Attach keyboard listener
  useMemo(() => {
    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [handleKeyDown]);

  // Auto-layout (tidy) button handler
  const handleTidy = useCallback(() => {
    const { nodes: tidyNodes } = stepsToFlow(steps, selectedStepId);
    setNodes(tidyNodes);
  }, [steps, selectedStepId, setNodes]);

  return (
    <div ref={reactFlowWrapper} className="w-full h-full relative">
      <ReactFlow
        nodes={nodes}
        edges={edges}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onConnect={onConnect}
        nodeTypes={nodeTypes}
        onNodeClick={handleNodeClick}
        onPaneClick={handlePaneClick}
        onDragOver={handleDragOver}
        onDrop={handleDrop}
        fitView
        fitViewOptions={{ padding: 0.3 }}
        proOptions={{ hideAttribution: true }}
        minZoom={0.3}
        maxZoom={2}
        defaultEdgeOptions={{ type: 'smoothstep' }}
        nodesDraggable={true}
        nodesConnectable={true}
      >
        <Background color="#27272a" gap={20} size={1} />
        <Controls
          showInteractive={false}
          className="!bg-surface-elevated/80 !border-border/40 !rounded-lg !shadow-node"
        />
      </ReactFlow>

      {/* Tidy button */}
      <button
        onClick={handleTidy}
        title="Auto-layout"
        className="absolute top-3 right-3 p-1.5 rounded-lg bg-surface-elevated/80 border border-border/40 text-text-muted hover:text-primary hover:border-primary/40 transition-all duration-200"
      >
        <AlignCenter size={14} />
      </button>
    </div>
  );
}
