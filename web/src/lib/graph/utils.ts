/**
 * Shared utility functions for graph layout engines
 */

import type { Node } from '@xyflow/react';
import { LAYOUT } from '../constants';

/**
 * Get node dimensions based on node type
 * Used by layout engines to calculate proper spacing
 */
export function getNodeDimensions(node: Node): { width: number; height: number } {
  switch (node.type) {
    case 'gateway':
      return { width: LAYOUT.GATEWAY_WIDTH, height: LAYOUT.GATEWAY_HEIGHT };
    case 'mcpServer':
    case 'resource':
      return { width: LAYOUT.NODE_WIDTH, height: LAYOUT.NODE_HEIGHT };
    case 'agent':
      return { width: LAYOUT.AGENT_WIDTH, height: LAYOUT.AGENT_HEIGHT };
    case 'client':
      return { width: LAYOUT.CLIENT_WIDTH, height: LAYOUT.CLIENT_HEIGHT };
    default:
      return { width: LAYOUT.NODE_WIDTH, height: LAYOUT.NODE_HEIGHT };
  }
}
