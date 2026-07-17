import type { CSSProperties, MouseEvent, ReactNode } from 'react';
import {
  ReactFlow,
  Background,
  BackgroundVariant,
  type Node as RFNode,
  type Edge as RFEdge,
  type NodeTypes,
  type EdgeTypes,
  type NodeChange,
  type EdgeChange,
  type Connection,
  type DefaultEdgeOptions,
  type FitViewOptions,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import { cn } from '../../lib/cn';

export interface BackgroundLayer {
  id?: string;
  variant?: BackgroundVariant;
  gap?: number;
  size?: number;
  color?: string;
}

export interface CanvasBaseProps<NodeData extends Record<string, unknown> = Record<string, unknown>> {
  nodes: RFNode<NodeData>[];
  edges: RFEdge[];
  nodeTypes?: NodeTypes;
  edgeTypes?: EdgeTypes;
  onNodesChange?: (changes: NodeChange[]) => void;
  onEdgesChange?: (changes: EdgeChange[]) => void;
  onNodeClick?: (event: MouseEvent, node: RFNode<NodeData>) => void;
  onPaneClick?: () => void;
  onConnect?: (connection: Connection) => void;
  defaultEdgeOptions?: DefaultEdgeOptions;
  fitView?: boolean;
  fitViewOptions?: FitViewOptions;
  minZoom?: number;
  maxZoom?: number;
  nodesDraggable?: boolean;
  nodesConnectable?: boolean;
  elementsSelectable?: boolean;
  zoomOnScroll?: boolean;
  panOnScroll?: boolean;
  // One or more <Background /> layers — defaults to a single dots layer.
  // Pass `null` for a transparent canvas (rare).
  backgrounds?: BackgroundLayer[] | null;
  // Slotted children render inside <ReactFlow>; use for <Panel>-based
  // workspace control bars or in-canvas overlays.
  children?: ReactNode;
  className?: string;
  style?: CSSProperties;
}

const DEFAULT_BACKGROUNDS: BackgroundLayer[] = [
  {
    variant: BackgroundVariant.Dots,
    gap: 24,
    size: 1,
    color: 'rgba(255,255,255,0.04)',
  },
];

/**
 * CanvasBase is the shared ReactFlow scaffolding for the Stack and Skills
 * workspaces. It owns the wrapper element, the ReactFlow component, the
 * Background layers, and the standard `proOptions`. Workspace-specific
 * pieces — custom node types, control panels, overlay layers — flow in via
 * props and the children slot so the two canvases compose this base rather
 * than inheriting from it.
 *
 * Keep this file small. New workspace behaviors should live in the workspace
 * canvas, not here; CanvasBase pays its way only because both canvases need
 * the same React Flow boilerplate.
 */
export function CanvasBase<
  NodeData extends Record<string, unknown> = Record<string, unknown>,
>({
  nodes,
  edges,
  nodeTypes,
  edgeTypes,
  onNodesChange,
  onEdgesChange,
  onNodeClick,
  onPaneClick,
  onConnect,
  defaultEdgeOptions,
  fitView,
  fitViewOptions,
  minZoom,
  maxZoom,
  nodesDraggable,
  nodesConnectable,
  elementsSelectable,
  zoomOnScroll,
  panOnScroll,
  backgrounds = DEFAULT_BACKGROUNDS,
  children,
  className,
  style,
}: CanvasBaseProps<NodeData>) {
  return (
    <div className={cn('absolute inset-0', className)} style={style}>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onNodeClick={onNodeClick as never}
        onPaneClick={onPaneClick}
        onConnect={onConnect}
        defaultEdgeOptions={defaultEdgeOptions}
        fitView={fitView}
        fitViewOptions={fitViewOptions}
        minZoom={minZoom}
        maxZoom={maxZoom}
        nodesDraggable={nodesDraggable}
        nodesConnectable={nodesConnectable}
        elementsSelectable={elementsSelectable}
        zoomOnScroll={zoomOnScroll}
        panOnScroll={panOnScroll}
        proOptions={{ hideAttribution: true }}
      >
        {backgrounds?.map((layer, i) => (
          <Background
            key={layer.id ?? `bg-${i}`}
            id={layer.id}
            variant={layer.variant ?? BackgroundVariant.Dots}
            gap={layer.gap}
            size={layer.size}
            color={layer.color}
          />
        ))}
        {children}
      </ReactFlow>
    </div>
  );
}

export default CanvasBase;
