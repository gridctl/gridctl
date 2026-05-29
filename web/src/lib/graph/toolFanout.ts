/**
 * Tool fan-out derivation for the topology canvas.
 *
 * When an MCP server node is expanded, its tools fan out as nodes to the
 * right of the server. These helpers are pure: given the laid-out backbone
 * nodes and the set of expanded server ids, they derive the extra tool nodes
 * and server -> tool edges, positioned LOCALLY relative to each server. The
 * backbone is never re-laid-out, so expanding a server does not shift it.
 *
 * The per-server fan-out is capped: at most TOOL_FANOUT_CAP tool nodes are
 * mounted. Any remainder collapses into a single "+N more" aggregate node that
 * carries the hidden tool names for an in-node popover, so a server with 80
 * tools never starbursts the canvas.
 */

import type { Node, Edge } from '@xyflow/react';
import type { ToolNodeData, ToolOverflowNodeData } from '../../types';
import { LAYOUT, NODE_TYPES } from '../constants';
import { getNodeDimensions } from './utils';

/** Maximum number of tool nodes mounted per expanded server. */
export const TOOL_FANOUT_CAP = 10;

/** Stable id for a tool fan-out node. */
export function toolNodeId(serverNodeId: string, toolName: string): string {
  return `tool-${serverNodeId}-${toolName}`;
}

/** Stable id for a server's "+N more" overflow node. */
export function overflowNodeId(serverNodeId: string): string {
  return `tool-overflow-${serverNodeId}`;
}

/** Stable id for a server -> tool edge. */
export function toolEdgeId(serverNodeId: string, targetId: string): string {
  return `edge-tool-${serverNodeId}-${targetId}`;
}

/**
 * Split a server's tools into the visible set (rendered as nodes) and the
 * overflow set (collapsed into a "+N more" node).
 *
 * - tools.length <= cap  -> all visible, no overflow.
 * - tools.length  > cap  -> first `cap` visible, the rest overflow.
 *
 * Pure and side-effect free; the unit of the cap logic.
 */
export function computeToolFanout(
  tools: string[],
  cap: number = TOOL_FANOUT_CAP
): { visible: string[]; overflow: string[] } {
  if (tools.length <= cap) {
    return { visible: [...tools], overflow: [] };
  }
  return { visible: tools.slice(0, cap), overflow: tools.slice(cap) };
}

interface FanoutOptions {
  compact?: boolean;
  /** User-dragged positions to preserve (keyed by node id). */
  draggedPositions?: Map<string, { x: number; y: number }>;
  /**
   * Lane index for this server among the currently-expanded servers. Servers
   * all share the same X (the "servers" column), so without a lane offset
   * every expanded server's tools would stack in the same place. Each lane
   * steps the tool column further right so columns never overlap.
   */
  laneIndex?: number;
}

/**
 * Build the tool + overflow nodes and server -> tool edges for a single
 * expanded server, positioned in a local vertical column to the server's
 * right and centered on the server's vertical midpoint. `laneIndex` offsets
 * the column horizontally so multiple expanded servers do not collide.
 */
export function createToolFanout(
  serverNode: Node,
  options: FanoutOptions = {}
): { nodes: Node[]; edges: Edge[] } {
  const { compact = false, draggedPositions, laneIndex = 0 } = options;
  const data = serverNode.data as { name?: string; tools?: string[] };
  const tools = data.tools ?? [];
  if (tools.length === 0) {
    return { nodes: [], edges: [] };
  }

  const serverNodeId = serverNode.id;
  const serverName = data.name ?? serverNodeId;
  const { visible, overflow } = computeToolFanout(tools);

  const { width: serverWidth, height: serverHeight } = getNodeDimensions(
    serverNode,
    compact
  );
  const laneStride = LAYOUT.TOOL_WIDTH + LAYOUT.TOOL_LANE_GAP;
  const columnX =
    serverNode.position.x + serverWidth + LAYOUT.TOOL_OFFSET_X + laneIndex * laneStride;
  const serverCenterY = serverNode.position.y + serverHeight / 2;

  const itemCount = visible.length + (overflow.length > 0 ? 1 : 0);
  const stackHeight =
    itemCount * LAYOUT.TOOL_HEIGHT + (itemCount - 1) * LAYOUT.TOOL_GAP;
  const startY = serverCenterY - stackHeight / 2;
  const rowY = (index: number) =>
    startY + index * (LAYOUT.TOOL_HEIGHT + LAYOUT.TOOL_GAP);

  const nodes: Node[] = [];
  const edges: Edge[] = [];

  visible.forEach((toolName, index) => {
    const id = toolNodeId(serverNodeId, toolName);
    const nodeData: ToolNodeData = {
      type: 'tool',
      name: toolName,
      serverName,
      serverNodeId,
    };
    nodes.push({
      id,
      type: NODE_TYPES.TOOL,
      position: draggedPositions?.get(id) ?? { x: columnX, y: rowY(index) },
      data: nodeData,
      draggable: true,
      selectable: false,
    });
    edges.push({
      id: toolEdgeId(serverNodeId, id),
      source: serverNodeId,
      sourceHandle: 'output',
      target: id,
      targetHandle: 'input',
      data: { relationType: 'server-to-tool', isHighlightable: false },
    });
  });

  if (overflow.length > 0) {
    const id = overflowNodeId(serverNodeId);
    const overflowData: ToolOverflowNodeData = {
      type: 'tool-overflow',
      serverName,
      serverNodeId,
      overflowCount: overflow.length,
      hiddenTools: overflow,
    };
    nodes.push({
      id,
      type: NODE_TYPES.TOOL_OVERFLOW,
      position: draggedPositions?.get(id) ?? { x: columnX, y: rowY(visible.length) },
      data: overflowData,
      draggable: true,
      selectable: false,
    });
    edges.push({
      id: toolEdgeId(serverNodeId, id),
      source: serverNodeId,
      sourceHandle: 'output',
      target: id,
      targetHandle: 'input',
      data: { relationType: 'server-to-tool', isHighlightable: false },
    });
  }

  return { nodes, edges };
}

/**
 * Append tool fan-out nodes and edges for every currently-expanded server to
 * an already-laid-out backbone. Returns new arrays; inputs are not mutated.
 * Servers in `expandedServers` that are not present in `nodes` (e.g. removed
 * from the stack) are silently skipped, so stale expansion state is harmless.
 */
export function appendToolFanout(
  nodes: Node[],
  edges: Edge[],
  expandedServers: Set<string>,
  options: FanoutOptions = {}
): { nodes: Node[]; edges: Edge[] } {
  if (expandedServers.size === 0) {
    return { nodes, edges };
  }

  const extraNodes: Node[] = [];
  const extraEdges: Edge[] = [];

  // Assign each expanded server its own lane in the order servers appear
  // (top to bottom), so their tool columns are separated horizontally.
  let laneIndex = 0;
  for (const node of nodes) {
    const nodeData = node.data as { type?: string };
    if (nodeData.type !== 'mcp-server' || !expandedServers.has(node.id)) {
      continue;
    }
    const fanout = createToolFanout(node, { ...options, laneIndex });
    laneIndex += 1;
    extraNodes.push(...fanout.nodes);
    extraEdges.push(...fanout.edges);
  }

  return {
    nodes: [...nodes, ...extraNodes],
    edges: [...edges, ...extraEdges],
  };
}
