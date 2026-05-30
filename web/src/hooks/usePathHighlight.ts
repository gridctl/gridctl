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
import type { ClientScopeResult } from '../types';

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
  edges: Edge[],
  inScopeTools?: Set<string>
): void {
  for (const node of nodes) {
    const data = node.data as {
      type?: string;
      serverNodeId?: string;
      serverName?: string;
      name?: string;
    };
    if (
      (data.type === 'tool' || data.type === 'tool-overflow') &&
      data.serverNodeId &&
      highlightedNodeIds.has(data.serverNodeId)
    ) {
      // When a per-client tool scope is active, only individual tool nodes
      // whose prefixed name (server__tool) is in scope light up; out-of-scope
      // tools stay dimmed. The "+N more" overflow follows its server.
      if (inScopeTools && data.type === 'tool') {
        const prefixed = `${data.serverName}__${data.name}`;
        if (!inScopeTools.has(prefixed)) continue;
      }
      highlightedNodeIds.add(node.id);
    }
  }

  for (const edge of edges) {
    const meta = edge.data as EdgeMetadata | undefined;
    if (
      meta?.relationType === 'server-to-tool' &&
      highlightedNodeIds.has(edge.source) &&
      highlightedNodeIds.has(edge.target)
    ) {
      highlightedEdgeIds.add(edge.id);
    }
  }
}

/**
 * Narrow an already-traced highlight to a client's real access scope, in place.
 *
 * Removes any highlighted MCP-server node whose name is not in the client's
 * `effectiveScope.servers`, along with the edges leading to it. The gateway and
 * client stay highlighted, so a client scoped to nothing shows as reaching the
 * gateway and stopping there (the empty-state) rather than the full graph.
 * Tools of pruned servers fall out automatically since their parent server is
 * no longer highlighted.
 */
function narrowToClientScope(
  highlightedNodeIds: Set<string>,
  highlightedEdgeIds: Set<string>,
  nodes: Node[],
  edges: Edge[],
  scope: ClientScopeResult
): void {
  const inScopeServers = new Set(scope.servers);
  const nodeById = new Map(nodes.map((n) => [n.id, n]));

  const removed = new Set<string>();
  for (const id of highlightedNodeIds) {
    const data = nodeById.get(id)?.data as { type?: string; name?: string } | undefined;
    if (data?.type === 'mcp-server' && !inScopeServers.has(data.name ?? '')) {
      removed.add(id);
    }
  }
  for (const id of removed) {
    highlightedNodeIds.delete(id);
  }
  for (const edge of edges) {
    if (removed.has(edge.target)) {
      highlightedEdgeIds.delete(edge.id);
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
 * @param scopeOverride - When set and a client is selected, this scope is used
 *   in place of the client node's saved `effectiveScope`. The Topology Access
 *   Lens passes a draft `ClientScopeResult` here so the canvas re-lights live
 *   against the unsaved draft. Passing null/undefined uses the saved scope.
 * @returns Highlight state with sets of node/edge IDs
 */
export function computeHighlightState(
  nodes: Node[],
  edges: Edge[],
  selectedNodeId: string | null,
  scopeOverride?: ClientScopeResult | null
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
  const isClient = nodeData?.type === 'client';

  // Client nodes: highlight the transitive client -> gateway -> servers path.
  // Other node types: just highlight the selected node.
  const { highlightedNodeIds, highlightedEdgeIds } = isClient
    ? traceReachablePath(selectedNodeId, edges)
    : {
        highlightedNodeIds: new Set([selectedNodeId]),
        highlightedEdgeIds: new Set<string>(),
      };

  // When the selected client has a configured, narrowed scope, restrict the
  // highlight to what it can actually reach. An absent or `unscoped` scope
  // preserves the gateway-exposed reach (the no-clients-block case). A
  // scopeOverride (the Access Lens draft) takes precedence over the saved scope.
  const scope = (scopeOverride ?? nodeData?.effectiveScope) as ClientScopeResult | undefined;
  let inScopeTools: Set<string> | undefined;
  if (isClient && scope && scope.configured && !scope.unscoped) {
    narrowToClientScope(highlightedNodeIds, highlightedEdgeIds, nodes, edges, scope);
    inScopeTools = new Set(scope.tools);
  }

  includeHighlightedServerTools(
    highlightedNodeIds,
    highlightedEdgeIds,
    nodes,
    edges,
    inScopeTools
  );

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
 * @param scopeOverride - Optional draft scope to preview (see computeHighlightState)
 * @returns Highlight state with sets of node/edge IDs
 */
export function usePathHighlight(
  nodes: Node[],
  edges: Edge[],
  selectedNodeId: string | null,
  scopeOverride?: ClientScopeResult | null
): HighlightState {
  return useMemo(
    () => computeHighlightState(nodes, edges, selectedNodeId, scopeOverride),
    [nodes, edges, selectedNodeId, scopeOverride]
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
