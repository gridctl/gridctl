/**
 * Path highlighting hook for butterfly layout
 *
 * Computes which nodes and edges should be highlighted based on the
 * selected node. For clients, traces the full transitive reachable path:
 * Client -> Gateway -> the servers that client can reach.
 */

import { useMemo } from 'react';
import type { Node, Edge } from '@xyflow/react';
import type { EdgeMetadata } from '../lib/graph/types';

/**
 * Highlight state returned by the hook
 */
export interface HighlightState {
  /** Node IDs that should be highlighted */
  highlightedNodeIds: Set<string>;
  /** Edge IDs that should be highlighted */
  highlightedEdgeIds: Set<string>;
  /** Whether any selection is active */
  hasSelection: boolean;
}

/** Empty highlight state - nothing selected, nothing dimmed. */
function emptyHighlight(): HighlightState {
  return {
    highlightedNodeIds: new Set<string>(),
    highlightedEdgeIds: new Set<string>(),
    hasSelection: false,
  };
}

/**
 * Trace the transitive reachable path outward from a node.
 *
 * Performs a breadth-first walk following only outgoing highlightable edges
 * (source -> target). Edge metadata's `isHighlightable` flag gates traversal,
 * so a client reaches the gateway and the servers behind it, but not the
 * resources or skill groups the gateway also manages (those edges are not
 * highlightable). When per-client scoping lands, the scoped-out gateway ->
 * server edges drop out of this walk and the highlight narrows automatically.
 *
 * @param startId - Node to start the walk from
 * @param edges - All edges in the graph
 * @returns Highlighted node and edge ID sets, including the start node
 */
function traceReachablePath(
  startId: string,
  edges: Edge[]
): { highlightedNodeIds: Set<string>; highlightedEdgeIds: Set<string> } {
  const highlightedNodeIds = new Set<string>([startId]);
  const highlightedEdgeIds = new Set<string>();

  // Index outgoing highlightable edges by source for an O(1) frontier expand.
  const outgoing = new Map<string, Edge[]>();
  for (const edge of edges) {
    const meta = edge.data as EdgeMetadata | undefined;
    if (!meta?.isHighlightable) continue;
    const bucket = outgoing.get(edge.source);
    if (bucket) {
      bucket.push(edge);
    } else {
      outgoing.set(edge.source, [edge]);
    }
  }

  const queue = [startId];
  while (queue.length > 0) {
    const current = queue.shift() as string;
    for (const edge of outgoing.get(current) ?? []) {
      highlightedEdgeIds.add(edge.id);
      if (!highlightedNodeIds.has(edge.target)) {
        highlightedNodeIds.add(edge.target);
        queue.push(edge.target);
      }
    }
  }

  return { highlightedNodeIds, highlightedEdgeIds };
}

/**
 * Fold the fanned-out tools of already-highlighted servers into the highlight
 * sets, in place. A tool (or "+N more") node inherits its parent server's
 * highlight state via its `serverNodeId`, and the server -> tool edge follows.
 *
 * This is what lets a client stay focused while you expand one of its
 * reachable servers: the new tool nodes light up in scope instead of being
 * dimmed with everything off the path. Tools of an out-of-scope server keep
 * their dimmed state because that server is not in the highlighted set.
 */
function includeHighlightedServerTools(
  highlightedNodeIds: Set<string>,
  highlightedEdgeIds: Set<string>,
  nodes: Node[],
  edges: Edge[]
): void {
  for (const node of nodes) {
    const data = node.data as { type?: string; serverNodeId?: string };
    if (
      (data.type === 'tool' || data.type === 'tool-overflow') &&
      data.serverNodeId &&
      highlightedNodeIds.has(data.serverNodeId)
    ) {
      highlightedNodeIds.add(node.id);
    }
  }

  for (const edge of edges) {
    const meta = edge.data as EdgeMetadata | undefined;
    if (meta?.relationType === 'server-to-tool' && highlightedNodeIds.has(edge.source)) {
      highlightedEdgeIds.add(edge.id);
    }
  }
}

/**
 * Compute path highlighting based on the selected node.
 *
 * When a client is selected: highlight the transitive client -> gateway ->
 * reachable-servers path. For other node types: highlight only the selected
 * node. In every case, the fanned-out tools of a highlighted server are
 * highlighted too, so expanding a reachable server in focus mode keeps its
 * tools at full opacity. Pure function so it can be unit-tested without React.
 *
 * @param nodes - All nodes in the graph
 * @param edges - All edges in the graph
 * @param selectedNodeId - Currently selected node ID, or null
 * @returns Highlight state with sets of node/edge IDs
 */
export function computeHighlightState(
  nodes: Node[],
  edges: Edge[],
  selectedNodeId: string | null
): HighlightState {
  // No selection = no highlighting
  if (!selectedNodeId) {
    return emptyHighlight();
  }

  // Find the selected node
  const selectedNode = nodes.find((n) => n.id === selectedNodeId);
  if (!selectedNode) {
    return {
      highlightedNodeIds: new Set([selectedNodeId]),
      highlightedEdgeIds: new Set<string>(),
      hasSelection: true,
    };
  }

  const nodeData = selectedNode.data as Record<string, unknown>;

  // Client nodes: highlight the transitive client -> gateway -> servers path.
  // Other node types: just highlight the selected node.
  const { highlightedNodeIds, highlightedEdgeIds } =
    nodeData?.type === 'client'
      ? traceReachablePath(selectedNodeId, edges)
      : {
          highlightedNodeIds: new Set([selectedNodeId]),
          highlightedEdgeIds: new Set<string>(),
        };

  includeHighlightedServerTools(highlightedNodeIds, highlightedEdgeIds, nodes, edges);

  return { highlightedNodeIds, highlightedEdgeIds, hasSelection: true };
}

/**
 * Compute path highlighting based on selected node
 *
 * When a client is selected: highlight client -> gateway -> reachable-servers
 * path. For other node types: just highlight the selected node.
 *
 * @param nodes - All nodes in the graph
 * @param edges - All edges in the graph
 * @param selectedNodeId - Currently selected node ID, or null
 * @returns Highlight state with sets of node/edge IDs
 */
export function usePathHighlight(
  nodes: Node[],
  edges: Edge[],
  selectedNodeId: string | null
): HighlightState {
  return useMemo(
    () => computeHighlightState(nodes, edges, selectedNodeId),
    [nodes, edges, selectedNodeId]
  );
}

/**
 * Check if a node should be dimmed based on highlight state
 *
 * @param nodeId - Node ID to check
 * @param highlightState - Current highlight state
 * @returns true if the node should be dimmed
 */
export function isNodeDimmed(
  nodeId: string,
  highlightState: HighlightState
): boolean {
  if (!highlightState.hasSelection) return false;
  return !highlightState.highlightedNodeIds.has(nodeId);
}

/**
 * Check if an edge should be dimmed based on highlight state
 *
 * @param edgeId - Edge ID to check
 * @param highlightState - Current highlight state
 * @returns true if the edge should be dimmed
 */
export function isEdgeDimmed(
  edgeId: string,
  highlightState: HighlightState
): boolean {
  if (!highlightState.hasSelection) return false;
  return !highlightState.highlightedEdgeIds.has(edgeId);
}
