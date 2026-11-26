/**
 * Shared utility functions for graph layout engines
 */

import type { Node } from '@xyflow/react';
import { LAYOUT } from '../constants';

// Full-card row heights in px, from the CustomNode class arithmetic and
// checked in the browser by web/stack-layout.browser.mjs.
const FULL_CARD = {
  BASE: 2 + 49 + 24, // border, header, body padding (p-3)
  ROW_GAP: 10,       // space-y-2.5
  ENDPOINT: 43,      // endpoint or container row
  IMAGE: 48,         // resource image row (its label is an inline span)
  TRANSPORT: 27,     // transport row, including its pt-1
  NETWORK: 24,       // resource network chip
  BOX: 30,           // health error, needs authorization, drift, or blocked box
  HEALTHY: 16,       // "Healthy" line
  STATUS: 29,        // status badge row, including its pt-1
  STATUS_DISABLED: 32, // the disabled badge sits in a block wrapper
} as const;

function usable(value: unknown): boolean {
  return typeof value === 'string' && value !== '' && value !== 'unknown';
}

/**
 * Estimated rendered height of a full (non-compact) server or resource card.
 * Mirrors the body rows of components/graph/CustomNode.tsx and their
 * conditions; keep the two in step. Estimates err a few px high so layout
 * never lets cards touch. Nodes without card data get LAYOUT.NODE_HEIGHT.
 */
export function estimateFullCardHeight(node: Node): number {
  const data = (node.data ?? {}) as Record<string, unknown>;
  const rows: number[] = [];
  if (data.type === 'mcp-server') {
    const enabled = data.enabled !== false;
    if (usable(data.endpoint) || usable(data.containerId)) rows.push(FULL_CARD.ENDPOINT);
    if (data.transport) rows.push(FULL_CARD.TRANSPORT);
    if (enabled && data.healthy === false && data.authStatus !== 'needs_auth') rows.push(FULL_CARD.BOX);
    if (enabled && data.healthy === true) rows.push(FULL_CARD.HEALTHY);
    if (enabled && data.authStatus === 'needs_auth') rows.push(FULL_CARD.BOX);
    if (data.pinStatus === 'drift' || data.pinStatus === 'blocked') rows.push(FULL_CARD.BOX);
    rows.push(enabled ? FULL_CARD.STATUS : FULL_CARD.STATUS_DISABLED);
  } else if (data.type === 'resource') {
    if (data.image) rows.push(FULL_CARD.IMAGE);
    if (data.network) rows.push(FULL_CARD.NETWORK);
    rows.push(FULL_CARD.STATUS);
  } else {
    return LAYOUT.NODE_HEIGHT;
  }
  return FULL_CARD.BASE + rows.reduce((sum, row) => sum + row, 0) + (rows.length - 1) * FULL_CARD.ROW_GAP;
}

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
        height: compact ? LAYOUT.NODE_HEIGHT_COMPACT : estimateFullCardHeight(node),
      };
    case 'client':
      return {
        width: compact ? LAYOUT.CLIENT_WIDTH : LAYOUT.CLIENT_WIDTH_FULL,
        height: compact ? LAYOUT.CLIENT_HEIGHT_COMPACT : LAYOUT.CLIENT_HEIGHT,
      };
    case 'skill':
      return {
        width: 176, // w-44 = 11rem = 176px
        height: compact ? LAYOUT.NODE_HEIGHT_COMPACT : LAYOUT.NODE_HEIGHT,
      };
    default:
      return { width: LAYOUT.NODE_WIDTH, height: LAYOUT.NODE_HEIGHT };
  }
}
