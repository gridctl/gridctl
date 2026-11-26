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
  TRANSPORT: 27,     // transport row on one line, including its pt-1
  TRANSPORT_RIGHT_WRAPPED: 35, // a count label breaks onto a second line
  TRANSPORT_LEFT_WRAPPED: 41,  // the transport chip itself breaks (e.g. at a hyphen)
  NETWORK: 24,       // resource network chip
  BOX: 30,           // health error, needs authorization, drift, or blocked box
  HEALTHY: 16,       // "Healthy" line
  STATUS: 29,        // status badge row, including its pt-1
  STATUS_DISABLED: 32, // the disabled badge sits in a block wrapper
} as const;

function usable(value: unknown): boolean {
  return typeof value === 'string' && value !== '' && value !== 'unknown';
}

// Transport row content width: 256 px card less its border and p-3 padding,
// with a small margin so near-full rows are treated as wrapping.
const TRANSPORT_ROW_WIDTH = 226;
// Rendered widths of the uppercase text-3xs labels, rounded up. Other labels
// fall back to a per-character width above the widest measured letter.
const LABEL_WIDTH: Record<string, number> = {
  http: 28, stdio: 33, sse: 19, 'streamable-http': 101,
  toon: 31, csv: 21, text: 27,
  prompts: 50, resources: 61,
};
const labelWidth = (text: string) => LABEL_WIDTH[text] ?? text.length * 10;
const MONO_2XS = 6.6; // IBM Plex Mono advance at 11 px
const MONO_3XS = 6;   // and at 10 px
const ITEM_GAP = 6;   // gap-1.5

const sideWidth = (items: number[]) =>
  items.reduce((sum, w) => sum + w, 0) + Math.max(0, items.length - 1) * ITEM_GAP;

/**
 * Height of the transport row. The row is a nowrap flex line of a transport
 * chip and output-format chip on the left and count labels on the right;
 * when their natural widths overflow the card, the flex items shrink and
 * their text breaks onto a second line.
 */
function transportRowHeight(data: Record<string, unknown>): number {
  const transport = String(data.transport);
  const format = typeof data.outputFormat === 'string' && data.outputFormat !== 'json' ? data.outputFormat : '';
  const left = [33 + labelWidth(transport)]; // px-2, icon, gap
  if (format) left.push(30 + labelWidth(format));

  const toolCount = typeof data.toolCount === 'number' ? data.toolCount : null;
  const whitelist = Array.isArray(data.toolWhitelist) ? data.toolWhitelist.length : 0;
  const right: number[] = [];
  if (whitelist > 0) right.push(25 + `${whitelist}/${toolCount ?? 0}`.length * MONO_3XS); // px-1.5, icon, gap
  if (toolCount !== null) right.push(`${toolCount} tools`.length * MONO_2XS);
  const prompts = Number(data.promptCount ?? 0);
  if (prompts > 0) right.push(`${prompts} prompts`.length * MONO_2XS);
  const resources = Number(data.mcpResourceCount ?? 0);
  if (resources > 0) right.push(`${resources} MCP resources`.length * MONO_2XS);
  const capabilities = (data.capabilities ?? {}) as Record<string, unknown>;
  if (capabilities.prompts) right.push(labelWidth('prompts'));
  if (capabilities.resources) right.push(labelWidth('resources'));

  if (sideWidth(left) + sideWidth(right) <= TRANSPORT_ROW_WIDTH) return FULL_CARD.TRANSPORT;
  return /[\s-]/.test(transport) || /[\s-]/.test(format)
    ? FULL_CARD.TRANSPORT_LEFT_WRAPPED
    : FULL_CARD.TRANSPORT_RIGHT_WRAPPED;
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
    if (data.transport) rows.push(transportRowHeight(data));
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
