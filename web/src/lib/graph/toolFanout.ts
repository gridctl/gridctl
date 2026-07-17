/**
 * Tool fan-out derivation for the stack canvas.
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

/** Rows per column in the "+N more" popover grid. */
export const OVERFLOW_GRID_ROWS = 10;

/** Maximum columns in the "+N more" popover grid. */
export const OVERFLOW_GRID_MAX_COLS = 4;

/**
 * Column-grid shape for the overflow popover: hidden tools stack
 * OVERFLOW_GRID_ROWS per column and columns grow rightward, capped at
 * OVERFLOW_GRID_MAX_COLS. Beyond the cap (count > 40) rows exceed 10 and the
 * panel scrolls vertically instead of widening further.
 */
export function overflowGridShape(count: number): { rows: number; cols: number } {
  if (count <= 0) {
    return { rows: 0, cols: 0 };
  }
  const cols = Math.min(OVERFLOW_GRID_MAX_COLS, Math.ceil(count / OVERFLOW_GRID_ROWS));
  return { rows: Math.ceil(count / cols), cols };
}

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
   * Shared X for the tool column. All expanded servers fan into one column to
   * the right of the servers zone; appendToolFanout computes this so every
   * server's band lines up. Defaults to just right of this server.
   */
  columnX?: number;
  /**
   * Top Y of this server's band. appendToolFanout tiles bands top-to-bottom so
   * they do not overlap. Defaults to centering the band on the server.
   */
  startY?: number;
}

/** Height of a server's tool band given how many nodes it mounts. */
export function fanoutBandHeight(tools: string[]): number {
  const { visible, overflow } = computeToolFanout(tools);
  const count = visible.length + (overflow.length > 0 ? 1 : 0);
  if (count === 0) return 0;
  return count * LAYOUT.TOOL_HEIGHT + (count - 1) * LAYOUT.TOOL_GAP;
}

/**
 * Build the tool + overflow nodes and server -> tool edges for a single
 * expanded server as a vertical band. `columnX` sets the shared column and
 * `startY` the band's top; both default to a stand-alone column centered on
 * the server (used when fanning out a single server).
 */
export function createToolFanout(
  serverNode: Node,
  options: FanoutOptions = {}
): { nodes: Node[]; edges: Edge[] } {
  const { compact = false, draggedPositions, columnX, startY } = options;
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
  const colX =
    columnX ?? serverNode.position.x + serverWidth + LAYOUT.TOOL_OFFSET_X;
  const bandHeight = fanoutBandHeight(tools);
  const top = startY ?? serverNode.position.y + serverHeight / 2 - bandHeight / 2;
  const rowY = (index: number) =>
    top + index * (LAYOUT.TOOL_HEIGHT + LAYOUT.TOOL_GAP);

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
      position: draggedPositions?.get(id) ?? { x: colX, y: rowY(index) },
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
      position: draggedPositions?.get(id) ?? { x: colX, y: rowY(visible.length) },
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
 *
 * All expanded servers fan into ONE shared column to the right of the servers
 * zone. Each server's tools form a vertical band; bands are tiled top-to-bottom
 * in the servers' own vertical order and the whole stack is centered on the
 * servers' midpoint. Because band order matches server order, each server's
 * edge bundle fans to a contiguous, aligned block and the bundles do not cross.
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

  const { compact = false } = options;

  // Expanded servers that actually have tools, ordered top-to-bottom so their
  // bands tile in the same order they appear on the canvas.
  const servers = nodes
    .filter((node) => {
      const data = node.data as { type?: string; tools?: string[] };
      return (
        data.type === 'mcp-server' &&
        expandedServers.has(node.id) &&
        (data.tools?.length ?? 0) > 0
      );
    })
    .sort((a, b) => a.position.y - b.position.y);

  if (servers.length === 0) {
    return { nodes, edges };
  }

  // Single shared column, just right of the rightmost server edge.
  const columnX =
    Math.max(
      ...servers.map(
        (s) => s.position.x + getNodeDimensions(s, compact).width
      )
    ) + LAYOUT.TOOL_OFFSET_X;

  // Total height of all bands plus the gaps between them, centered on the
  // vertical midpoint of the expanded servers.
  const bandHeights = servers.map((s) =>
    fanoutBandHeight((s.data as { tools?: string[] }).tools ?? [])
  );
  const totalHeight =
    bandHeights.reduce((sum, h) => sum + h, 0) +
    (servers.length - 1) * LAYOUT.TOOL_BAND_GAP;
  const centerY =
    servers.reduce(
      (sum, s) => sum + s.position.y + getNodeDimensions(s, compact).height / 2,
      0
    ) / servers.length;

  const extraNodes: Node[] = [];
  const extraEdges: Edge[] = [];

  let cursorY = centerY - totalHeight / 2;
  servers.forEach((server, i) => {
    const fanout = createToolFanout(server, { ...options, columnX, startY: cursorY });
    extraNodes.push(...fanout.nodes);
    extraEdges.push(...fanout.edges);
    cursorY += bandHeights[i] + LAYOUT.TOOL_BAND_GAP;
  });

  return {
    nodes: [...nodes, ...extraNodes],
    edges: [...edges, ...extraEdges],
  };
}
