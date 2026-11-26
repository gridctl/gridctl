/**
 * Row model for the Stack workspace's toolbar and table view.
 *
 * Pure functions over the stack store's React Flow nodes: one row per MCP
 * server, resource, and linked client, plus the filter, facet counts, and
 * sort the toolbar, the table, and the canvas fade share. Row ids are node
 * ids, so a row and its graph node select the same inspector target.
 */

import type { Node } from '@xyflow/react';
import type { AutoscaleStatus, NodeStatus } from '../types';

export type StackRowKind = 'mcp-server' | 'resource' | 'client';

/** Every node status plus the client-only `linked`. */
export type RowState = NodeStatus | 'linked';

export type StackRowPin = 'pinned' | 'drift' | 'blocked' | 'approved_pending_redeploy';

export interface StackRow {
  id: string;
  kind: StackRowKind;
  name: string;
  state: RowState;
  transport: string | null;
  toolCount: number | null;
  replicas: { current: number; target?: number } | null;
  pin: StackRowPin | null;
}

export type StackFacet = 'attention' | RowState | 'drift' | 'blocked';

export interface StackFilter {
  query: string;
  facets: ReadonlySet<StackFacet>;
}

export type StackSortKey = 'name' | 'kind' | 'status' | 'transport' | 'tools' | 'replicas' | 'pin';
export type StackSortDirection = 'asc' | 'desc';

/** Above this many MCP servers the workspace opens on the table until the user picks a view. */
export const STACK_TABLE_AUTO_THRESHOLD = 24;

export const EMPTY_STACK_FILTER: StackFilter = { query: '', facets: new Set() };

/** Facet order in the Status menu, with its human label. */
export const STACK_FACETS: ReadonlyArray<{ facet: StackFacet; label: string }> = [
  { facet: 'attention', label: 'Needs attention' },
  { facet: 'running', label: 'Running' },
  { facet: 'error', label: 'Error' },
  { facet: 'restarting', label: 'Restarting' },
  { facet: 'needs-auth', label: 'Needs authorization' },
  { facet: 'initializing', label: 'Initializing' },
  { facet: 'idle', label: 'Idle' },
  { facet: 'stopped', label: 'Stopped' },
  { facet: 'disabled', label: 'Disabled' },
  { facet: 'linked', label: 'Linked' },
  { facet: 'drift', label: 'Pin drift' },
  { facet: 'blocked', label: 'Pin blocked' },
];

export const ROW_STATE_LABELS: Record<RowState, string> = {
  running: 'Running',
  stopped: 'Stopped',
  error: 'Error',
  initializing: 'Initializing',
  idle: 'Idle',
  'needs-auth': 'Needs authorization',
  restarting: 'Restarting',
  disabled: 'Disabled',
  linked: 'Linked',
};

export const ROW_KIND_LABELS: Record<StackRowKind, string> = {
  'mcp-server': 'MCP server',
  resource: 'Resource',
  client: 'Client',
};

export const ROW_PIN_LABELS: Record<StackRowPin, string> = {
  pinned: 'Pinned',
  drift: 'Drift',
  blocked: 'Blocked',
  approved_pending_redeploy: 'Pending redeploy',
};

const STATE_ORDER: readonly RowState[] = [
  'error',
  'restarting',
  'needs-auth',
  'initializing',
  'stopped',
  'idle',
  'disabled',
  'running',
  'linked',
];

const KIND_ORDER: readonly StackRowKind[] = ['mcp-server', 'resource', 'client'];

const PIN_ORDER: readonly StackRowPin[] = ['blocked', 'drift', 'approved_pending_redeploy', 'pinned'];

const PIN_VALUES = new Set<string>(PIN_ORDER);

type RowNodeData = {
  type?: string;
  name?: string;
  status?: NodeStatus;
  enabled?: boolean;
  transport?: string;
  toolCount?: number;
  replicaCount?: number;
  autoscale?: AutoscaleStatus;
  pinStatus?: string;
};

function toRow(node: Node): StackRow | null {
  const data = (node.data ?? {}) as RowNodeData;
  const name = data.name ?? node.id;
  switch (data.type) {
    case 'mcp-server': {
      let replicas: StackRow['replicas'] = null;
      if (data.autoscale) {
        replicas = { current: data.autoscale.current, target: data.autoscale.target };
      } else if (typeof data.replicaCount === 'number' && data.replicaCount > 0) {
        replicas = { current: data.replicaCount };
      }
      return {
        id: node.id,
        kind: 'mcp-server',
        name,
        state: data.enabled === false ? 'disabled' : (data.status ?? 'stopped'),
        transport: data.transport ?? null,
        toolCount: typeof data.toolCount === 'number' ? data.toolCount : null,
        replicas,
        pin: data.pinStatus && PIN_VALUES.has(data.pinStatus) ? (data.pinStatus as StackRowPin) : null,
      };
    }
    case 'resource':
      return {
        id: node.id,
        kind: 'resource',
        name,
        state: data.status ?? 'stopped',
        transport: null,
        toolCount: null,
        replicas: null,
        pin: null,
      };
    case 'client':
      // Client nodes carry status 'running' for the canvas; a client is
      // linked, never a running server.
      return {
        id: node.id,
        kind: 'client',
        name,
        state: 'linked',
        transport: data.transport ?? null,
        toolCount: null,
        replicas: null,
        pin: null,
      };
    default:
      return null;
  }
}

/** One row per MCP server, resource, and client node; other node types are skipped. */
export function buildStackRows(nodes: Node[]): StackRow[] {
  const rows: StackRow[] = [];
  for (const node of nodes) {
    const row = toRow(node);
    if (row) rows.push(row);
  }
  return rows;
}

/**
 * Attention bucket, lower first: 0 error, restarting, or pin-blocked; 1 needs
 * authorization; 2 pin drift; 3 everything else. A disabled row is always 3.
 * Same buckets as the canvas server order (lib/graph/serverOrder.ts), over rows.
 */
export function attentionRank(row: StackRow): 0 | 1 | 2 | 3 {
  if (row.state === 'disabled') return 3;
  if (row.state === 'error' || row.state === 'restarting' || row.pin === 'blocked') return 0;
  if (row.state === 'needs-auth') return 1;
  if (row.pin === 'drift') return 2;
  return 3;
}

export function isFilterActive(filter: StackFilter): boolean {
  return filter.query.trim() !== '' || filter.facets.size > 0;
}

function facetApplies(row: StackRow, facet: StackFacet): boolean {
  if (facet === 'attention') return attentionRank(row) < 3;
  if (facet === 'drift' || facet === 'blocked') return row.pin === facet;
  return row.state === facet;
}

/** Name contains every query term (case-insensitive), and any selected facet applies. */
export function matchesFilter(row: StackRow, filter: StackFilter): boolean {
  const terms = filter.query.toLowerCase().split(/\s+/).filter(Boolean);
  if (terms.length > 0) {
    const name = row.name.toLowerCase();
    if (!terms.every((term) => name.includes(term))) return false;
  }
  if (filter.facets.size === 0) return true;
  for (const facet of filter.facets) {
    if (facetApplies(row, facet)) return true;
  }
  return false;
}

/** Count per facet over every row, unfiltered. */
export function facetCounts(rows: StackRow[]): Record<StackFacet, number> {
  const counts = Object.fromEntries(STACK_FACETS.map(({ facet }) => [facet, 0])) as Record<StackFacet, number>;
  for (const row of rows) {
    for (const { facet } of STACK_FACETS) {
      if (facetApplies(row, facet)) counts[facet] += 1;
    }
  }
  return counts;
}

function compareNullable<T>(a: T | null, b: T | null, cmp: (x: T, y: T) => number, dir: number): number | null {
  if (a === null && b === null) return null;
  if (a === null) return 1;
  if (b === null) return -1;
  const c = cmp(a, b) * dir;
  return c === 0 ? null : c;
}

const byName = (a: StackRow, b: StackRow) => a.name.localeCompare(b.name) || a.id.localeCompare(b.id);

/**
 * Sort rows by a column. Nulls sort last in both directions, and every key
 * breaks ties by name ascending. The input is not mutated.
 */
export function sortStackRows(rows: StackRow[], key: StackSortKey, direction: StackSortDirection): StackRow[] {
  const dir = direction === 'asc' ? 1 : -1;
  const primary = (a: StackRow, b: StackRow): number | null => {
    switch (key) {
      case 'name': {
        const c = a.name.localeCompare(b.name) * dir;
        return c === 0 ? null : c;
      }
      case 'kind': {
        const c = (KIND_ORDER.indexOf(a.kind) - KIND_ORDER.indexOf(b.kind)) * dir;
        return c === 0 ? null : c;
      }
      case 'status': {
        const c =
          (attentionRank(a) - attentionRank(b) || STATE_ORDER.indexOf(a.state) - STATE_ORDER.indexOf(b.state)) * dir;
        return c === 0 ? null : c;
      }
      case 'transport':
        return compareNullable(a.transport, b.transport, (x, y) => x.localeCompare(y), dir);
      case 'tools':
        return compareNullable(a.toolCount, b.toolCount, (x, y) => x - y, dir);
      case 'replicas':
        return compareNullable(a.replicas, b.replicas, (x, y) => x.current - y.current || (x.target ?? 0) - (y.target ?? 0), dir);
      case 'pin':
        return compareNullable(a.pin, b.pin, (x, y) => PIN_ORDER.indexOf(x) - PIN_ORDER.indexOf(y), dir);
    }
  };
  return [...rows].sort((a, b) => primary(a, b) ?? byName(a, b));
}
