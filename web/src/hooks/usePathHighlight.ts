/**
 * Path highlighting hook for butterfly layout
 *
 * Computes which nodes and edges should be highlighted based on
 * the selected node. For clients, traces the path: Client -> Gateway.
 */

import { useMemo } from 'react';
import type { Node, Edge } from '@xyflow/react';

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

/**
 * Compute path highlighting based on selected node
 *
 * When a client is selected: highlight client -> gateway path.
 * For other node types: just highlight the selected node.
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
  return useMemo(() => {
    // No selection = no highlighting
    if (!selectedNodeId) {
      return {
        highlightedNodeIds: new Set<string>(),
        highlightedEdgeIds: new Set<string>(),
        hasSelection: false,
      };
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

    // Client nodes: highlight client -> gateway path
    if (nodeData?.type === 'client') {
      const highlightedNodeIds = new Set<string>([selectedNodeId]);
      const highlightedEdgeIds = new Set<string>();

      for (const edge of edges) {
        if (edge.source === selectedNodeId && edge.target === 'gateway') {
          highlightedEdgeIds.add(edge.id);
          highlightedNodeIds.add('gateway');
          break;
        }
      }

      return { highlightedNodeIds, highlightedEdgeIds, hasSelection: true };
    }

    // For all other node types, just highlight the selected node
    return {
      highlightedNodeIds: new Set([selectedNodeId]),
      highlightedEdgeIds: new Set<string>(),
      hasSelection: true,
    };
  }, [nodes, edges, selectedNodeId]);
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
