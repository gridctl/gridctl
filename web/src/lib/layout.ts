import Dagre from '@dagrejs/dagre';
import type { Node, Edge } from '@xyflow/react';
import { LAYOUT } from './constants';

/**
 * Layout direction for the dagre graph
 */
export type LayoutDirection = 'LR' | 'TB' | 'RL' | 'BT';

/**
 * Options for the dagre layout
 */
export interface LayoutOptions {
  direction?: LayoutDirection;
  nodeSpacing?: number;
  rankSpacing?: number;
}

/**
 * Get node dimensions based on node type
 * Used by dagre to calculate proper spacing
 */
function getNodeDimensions(node: Node): { width: number; height: number } {
  switch (node.type) {
    case 'gateway':
      return { width: LAYOUT.GATEWAY_WIDTH, height: LAYOUT.GATEWAY_HEIGHT };
    case 'mcpServer':
    case 'resource':
      return { width: LAYOUT.NODE_WIDTH, height: LAYOUT.NODE_HEIGHT };
    case 'agent':
      return { width: LAYOUT.AGENT_SIZE, height: LAYOUT.AGENT_SIZE };
    default:
      return { width: LAYOUT.NODE_WIDTH, height: LAYOUT.NODE_HEIGHT };
  }
}

/**
 * Apply dagre layout to nodes and edges
 *
 * This implements a Left-to-Right (LR) hierarchical layout:
 * - Tier 1 (Left): Gateway node - the entry point/controller
 * - Tier 2 (Center): MCP Servers, Agents - direct connections to gateway
 * - Tier 3 (Right): Resources, A2A Agents - dependencies/skills
 *
 * The layout follows western UI conventions where:
 * - Control/Input flows from the left
 * - Output/Workers flow to the right
 */
export function applyDagreLayout(
  nodes: Node[],
  edges: Edge[],
  options: LayoutOptions = {}
): Node[] {
  const {
    direction = 'LR',
    nodeSpacing = LAYOUT.NODE_SPACING,
    rankSpacing = LAYOUT.RANK_SPACING,
  } = options;

  // Create a new dagre graph
  const g = new Dagre.graphlib.Graph().setDefaultEdgeLabel(() => ({}));

  // Configure the graph layout
  g.setGraph({
    rankdir: direction,
    nodesep: nodeSpacing,
    ranksep: rankSpacing,
    marginx: LAYOUT.MARGIN_X,
    marginy: LAYOUT.MARGIN_Y,
  });

  // Add nodes to the graph with their dimensions
  nodes.forEach((node) => {
    const { width, height } = getNodeDimensions(node);
    g.setNode(node.id, { width, height });
  });

  // Add edges to the graph
  edges.forEach((edge) => {
    g.setEdge(edge.source, edge.target);
  });

  // Run the dagre layout algorithm
  Dagre.layout(g);

  // Apply the calculated positions to nodes
  return nodes.map((node) => {
    const nodeWithPosition = g.node(node.id);
    const { width, height } = getNodeDimensions(node);

    return {
      ...node,
      position: {
        // Dagre returns center positions, convert to top-left for React Flow
        x: nodeWithPosition.x - width / 2,
        y: nodeWithPosition.y - height / 2,
      },
    };
  });
}

/**
 * Apply layout with preserved positions for specific nodes
 * Useful when you want to keep user-dragged positions
 */
export function applyDagreLayoutWithPreserved(
  nodes: Node[],
  edges: Edge[],
  preservedPositions: Map<string, { x: number; y: number }>,
  options: LayoutOptions = {}
): Node[] {
  // If no preserved positions, apply full layout
  if (preservedPositions.size === 0) {
    return applyDagreLayout(nodes, edges, options);
  }

  // Apply layout to get new positions
  const layoutedNodes = applyDagreLayout(nodes, edges, options);

  // Merge with preserved positions
  return layoutedNodes.map((node) => {
    const preserved = preservedPositions.get(node.id);
    if (preserved) {
      return { ...node, position: preserved };
    }
    return node;
  });
}
