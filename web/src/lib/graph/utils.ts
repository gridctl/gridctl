/**
 * Shared utility functions for graph layout engines
 */

import type { Node } from '@xyflow/react';
import { LAYOUT } from '../constants';

/**
 * Get node dimensions based on node type and compact state
 * Used by layout engines to calculate proper spacing
 */
export function getNodeDimensions(node: Node, compact = false): { width: number; height: number } {
  switch (node.type) {
    case 'gateway':
      return { width: LAYOUT.GATEWAY_WIDTH, height: LAYOUT.GATEWAY_HEIGHT };
    case 'mcpServer':
    case 'resource':
      return {
        width: LAYOUT.NODE_WIDTH,
        height: compact ? LAYOUT.NODE_HEIGHT_COMPACT : LAYOUT.NODE_HEIGHT,
      };
    case 'client':
      return {
        width: LAYOUT.CLIENT_WIDTH,
        height: compact ? LAYOUT.CLIENT_HEIGHT_COMPACT : LAYOUT.CLIENT_HEIGHT,
      };
    default:
      return { width: LAYOUT.NODE_WIDTH, height: LAYOUT.NODE_HEIGHT };
  }
}
