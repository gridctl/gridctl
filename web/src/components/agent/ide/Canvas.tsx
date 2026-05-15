import { useMemo, useEffect, useCallback } from 'react';
import {
  ReactFlowProvider,
  Controls,
  BackgroundVariant,
  type Node as RFNode,
  type Edge as RFEdge,
  type NodeProps,
  Handle,
  Position,
  useReactFlow,
} from '@xyflow/react';
import { editorURL, type SkillGraph } from '../../../lib/agent-api';
import { styleFor } from './kind-style';
import type { NodeTrace } from './useRunTrace';
import { TracePill } from './TracePill';
import { CanvasBase } from '../../canvas/CanvasBase';
import { EmptyState } from '../../ui/EmptyState';
import { cn } from '../../../lib/cn';
import { Sparkles } from 'lucide-react';

interface CanvasProps {
  graph: SkillGraph;
  skillDir: string;
  selected: string | null;
  onSelect: (nodeID: string | null) => void;
  trace: Record<string, NodeTrace>;
}

interface NodeData extends Record<string, unknown> {
  kind: SkillGraph['nodes'][number]['kind'];
  label: string;
  file: string;
  line: number;
  trace?: NodeTrace;
  selected: boolean;
  onJump: () => void;
}

const NODE_WIDTH = 240;
const ROW_HEIGHT = 100;

/**
 * Canvas is the Skills workspace's React Flow surface — a read-only,
 * single-column rendering of the parsed typed-skill AST. The graph mirrors
 * what NodeList shows; both consume the same trace overlay so the views
 * stay recognisably the same data.
 *
 * "The fallacy of the graph applies — code is canon." The canvas never
 * writes back to source. Double-click a node to jump to its source line.
 */
export function Canvas(props: CanvasProps) {
  return (
    <ReactFlowProvider>
      <CanvasInner {...props} />
    </ReactFlowProvider>
  );
}

function CanvasInner({ graph, skillDir, selected, onSelect, trace }: CanvasProps) {
  const { fitView } = useReactFlow();

  const nodes = useMemo<RFNode<NodeData>[]>(() => {
    return graph.nodes.map((node, i) => ({
      id: node.id,
      position: { x: 80, y: i * ROW_HEIGHT },
      data: {
        kind: node.kind,
        label: node.label,
        file: node.file,
        line: node.line,
        trace: trace[node.id],
        selected: selected === node.id,
        onJump: () => {
          window.open(editorURL(skillDir, node.file, node.line), '_self');
        },
      },
      type: 'agentNode',
      width: NODE_WIDTH,
    }));
  }, [graph.nodes, trace, selected, skillDir]);

  const edges = useMemo<RFEdge[]>(() => {
    const out: RFEdge[] = [];
    for (let i = 0; i + 1 < graph.nodes.length; i++) {
      const src = graph.nodes[i];
      const dst = graph.nodes[i + 1];
      out.push({
        id: `${src.id}->${dst.id}`,
        source: src.id,
        target: dst.id,
        type: 'smoothstep',
        animated: trace[src.id]?.status === 'running',
        style: { stroke: 'var(--color-border)', strokeWidth: 1.5 },
      });
    }
    return out;
  }, [graph.nodes, trace]);

  const handleNodeClick = useCallback(
    (_: unknown, n: RFNode<NodeData>) => {
      onSelect(selected === n.id ? null : n.id);
    },
    [onSelect, selected],
  );

  // Re-fit when the graph shape changes.
  useEffect(() => {
    if (graph.nodes.length === 0) return;
    const t = window.setTimeout(() => fitView({ padding: 0.2, duration: 200 }), 50);
    return () => window.clearTimeout(t);
  }, [graph.nodes.length, fitView]);

  if (graph.nodes.length === 0) {
    return (
      <EmptyState
        icon={Sparkles}
        title="No primitives recognised"
        description="Add a tool(), llm(), parallel(), handoff(), or approval() call to the source — the canvas re-renders on save."
      />
    );
  }

  return (
    <CanvasBase<NodeData>
      className="agent-canvas"
      nodes={nodes}
      edges={edges}
      nodeTypes={nodeTypes}
      onNodeClick={handleNodeClick}
      onPaneClick={() => onSelect(null)}
      nodesDraggable={false}
      nodesConnectable={false}
      elementsSelectable
      zoomOnScroll
      panOnScroll
      minZoom={0.4}
      maxZoom={1.6}
      backgrounds={[
        { variant: BackgroundVariant.Dots, gap: 24, size: 1, color: 'rgba(255,255,255,0.04)' },
      ]}
    >
      <Controls
        showInteractive={false}
        style={{
          background: 'var(--color-surface)',
          borderColor: 'var(--color-border)',
          borderWidth: 1,
        }}
      />
    </CanvasBase>
  );
}

const nodeTypes = {
  agentNode: AgentFlowNode,
};

function AgentFlowNode({ data }: NodeProps<RFNode<NodeData>>) {
  const style = styleFor(data.kind);
  const status = data.trace?.status;
  return (
    <div
      onDoubleClick={data.onJump}
      className={cn(
        'agent-canvas-node group relative bg-surface text-text-primary',
        'rounded-lg border transition-all duration-150',
        'overflow-hidden',
        data.selected ? 'border-primary/60 shadow-lg' : style.border,
      )}
      style={{ width: NODE_WIDTH }}
    >
      <Handle type="target" position={Position.Top} className="!bg-border !border-0 !w-2 !h-2" />
      {/* Top accent stripe — uses the per-kind colour as a thin
          identity hairline so kinds are scannable across the canvas */}
      <div className={cn('h-[3px]', style.badgeBg)} />
      <div className="px-3 py-2.5 space-y-1.5">
        <div className="flex items-center gap-2">
          <span
            className={cn(
              'inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[9px]',
              'uppercase tracking-[0.18em] font-medium font-mono',
              style.badgeBg,
              style.badgeText,
              'border',
              style.border,
            )}
          >
            <span className="text-[10px] leading-none">{style.glyph}</span>
            {style.label}
          </span>
          {status && status !== 'queued' && (
            <span className="ml-auto">
              <TracePill trace={data.trace} compact />
            </span>
          )}
        </div>
        <div className="font-mono text-sm truncate text-text-primary">{data.label}</div>
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            data.onJump();
          }}
          className={cn(
            'w-full text-left text-[10px] font-mono truncate',
            'text-text-muted hover:text-primary transition-colors',
          )}
          title={`${data.file}:${data.line} — open in $EDITOR`}
        >
          {data.file}:{data.line}
        </button>
      </div>
      <Handle type="source" position={Position.Bottom} className="!bg-border !border-0 !w-2 !h-2" />
    </div>
  );
}
