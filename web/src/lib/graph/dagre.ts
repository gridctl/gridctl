/**
 * Dagre-based hierarchical layout engine
 *
 * This is the legacy/hierarchical layout strategy that uses Dagre's
 * automatic rank computation based on edge connectivity.
 */

import Dagre from '@dagrejs/dagre';
import type { Node, Edge } from '@xyflow/react';
import type { LayoutEngine, LayoutInput, LayoutOutput, LayoutOptions } from './types';
import { LAYOUT } from '../constants';
import { getNodeDimensions } from './utils';

/**
 * Layout direction for dagre graph
 */
export type LayoutDirection = 'LR' | 'TB' | 'RL' | 'BT';

/**
 * Dagre-specific layout options
 */
export interface DagreLayoutOptions extends LayoutOptions {
  direction?: LayoutDirection;
  nodeSpacing?: number;
  rankSpacing?: number;
}

/**
 * Apply dagre layout algorithm to nodes
 *
 * @param nodes - Nodes to position
 * @param edges - Edges that define connectivity
 * @param options - Dagre configuration options
 * @returns Nodes with calculated positions
 */
function applyDagreLayout(
  nodes: Node[],
  edges: Edge[],
  options: DagreLayoutOptions = {}
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
 * Dagre layout engine implementation
 *
 * Uses Dagre's hierarchical algorithm for automatic node positioning.
 * This is the legacy layout that infers ranks from edge connectivity.
 */
export class DagreLayoutEngine implements LayoutEngine {
  readonly name = 'dagre';
  private options: DagreLayoutOptions;

  constructor(options: DagreLayoutOptions = {}) {
    this.options = options;
  }

  layout(input: LayoutInput, options?: LayoutOptions): LayoutOutput {
    const { nodes, edges } = input;
    const preservedPositions = options?.preservedPositions;

    // Apply dagre layout
    const layoutedNodes = applyDagreLayout(nodes, edges, this.options);

    // Merge with preserved positions if provided
    const finalNodes = preservedPositions && preservedPositions.size > 0
      ? layoutedNodes.map((node) => {
          const preserved = preservedPositions.get(node.id);
          return preserved ? { ...node, position: preserved } : node;
        })
      : layoutedNodes;

    return { nodes: finalNodes, edges };
  }
}
