/**
 * Path highlighting hook for butterfly layout
 *
 * Computes which nodes and edges should be highlighted based on
 * the selected node. When an agent is selected, traces the path:
 * Agent -> Gateway -> used servers
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

/**
 * Compute path highlighting based on selected node
 *
 * When an agent is selected:
 * 1. Highlight the agent node
 * 2. Highlight Agent -> Gateway edge
 * 3. Highlight Gateway node
 * 4. Find all servers this agent uses (from agent.data.uses)
 * 5. Highlight Gateway -> those servers edges
 * 6. Highlight those server nodes
 * 7. Highlight Agent -> Server/Agent edges (uses relationships)
 *
 * Everything not highlighted should be dimmed.
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

    // Only agents trigger full path highlighting
    if (nodeData?.type !== 'agent') {
      // For non-agents (servers, resources), just highlight the selected node
      return {
        highlightedNodeIds: new Set([selectedNodeId]),
        highlightedEdgeIds: new Set<string>(),
        hasSelection: true,
      };
    }

    // Build highlight sets for agent path
    const highlightedNodeIds = new Set<string>();
    const highlightedEdgeIds = new Set<string>();

    // 1. Highlight selected agent
    highlightedNodeIds.add(selectedNodeId);

    // Get agent name for matching edges
    const agentName = nodeData.name as string;

    // 2. Find and highlight Agent -> Gateway edge
    for (const edge of edges) {
      const metadata = edge.data as EdgeMetadata | undefined;

      if (
        metadata?.relationType === 'agent-to-gateway' &&
        metadata?.sourceAgent === agentName
      ) {
        highlightedEdgeIds.add(edge.id);
        highlightedNodeIds.add(edge.target); // Gateway
        break;
      }
    }

    // 3. Find servers this agent uses
    const uses = nodeData.uses as Array<{ server: string }> | undefined;
    const usedServerIds = new Set<string>();

    if (uses) {
      for (const selector of uses) {
        // Could be MCP server or another agent
        usedServerIds.add(`mcp-${selector.server}`);
        usedServerIds.add(`agent-${selector.server}`);
      }
    }

    // 4. Find and highlight Gateway -> used servers edges AND Agent -> used server/agent edges
    for (const edge of edges) {
      const metadata = edge.data as EdgeMetadata | undefined;

      // Gateway -> Server edges for servers this agent uses
      if (
        metadata?.relationType === 'gateway-to-server' &&
        usedServerIds.has(edge.target)
      ) {
        highlightedEdgeIds.add(edge.id);
        highlightedNodeIds.add(edge.target);
      }

      // Agent -> Server/Agent edges (uses relationships)
      if (
        (metadata?.relationType === 'agent-uses-server' ||
          metadata?.relationType === 'agent-uses-agent') &&
        metadata?.sourceAgent === agentName
      ) {
        highlightedEdgeIds.add(edge.id);
        highlightedNodeIds.add(edge.target);
      }
    }

    return {
      highlightedNodeIds,
      highlightedEdgeIds,
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
