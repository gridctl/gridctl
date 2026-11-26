import { describe, expect, it } from 'vitest';
import type { Node } from '@xyflow/react';
import {
  STACK_TABLE_AUTO_THRESHOLD,
  attentionRank,
  buildStackRows,
  facetCounts,
  isFilterActive,
  matchesFilter,
  sortStackRows,
  type StackFacet,
  type StackRow,
} from '../lib/stackRows';

function node(id: string, data: Record<string, unknown>): Node {
  return { id, position: { x: 0, y: 0 }, data };
}

function row(partial: Partial<StackRow> & { name: string }): StackRow {
  return {
    id: `mcp-${partial.name}`,
    kind: 'mcp-server',
    state: 'running',
    transport: 'http',
    toolCount: 1,
    replicas: null,
    pin: null,
    ...partial,
  };
}

function filter(query: string, facets: StackFacet[] = []) {
  return { query, facets: new Set(facets) };
}

const names = (rows: StackRow[]) => rows.map((r) => r.name);

describe('buildStackRows', () => {
  it('builds one row per server, resource, and client and skips other node types', () => {
    const rows = buildStackRows([
      node('gateway', { type: 'gateway', name: 'gw' }),
      node('mcp-github', { type: 'mcp-server', name: 'github', status: 'running', transport: 'stdio', toolCount: 4, pinStatus: 'pinned' }),
      node('resource-db', { type: 'resource', name: 'db', status: 'stopped' }),
      node('client-claude', { type: 'client', name: 'Claude', transport: 'http', status: 'running' }),
      node('tool-mcp-github-a', { type: 'tool', name: 'a', serverNodeId: 'mcp-github' }),
      node('tool-overflow-mcp-github', { type: 'tool-overflow', serverNodeId: 'mcp-github' }),
      node('skill-group-x', { type: 'skill-group', groupName: 'x' }),
    ]);
    expect(rows).toEqual([
      { id: 'mcp-github', kind: 'mcp-server', name: 'github', state: 'running', transport: 'stdio', toolCount: 4, replicas: null, pin: 'pinned' },
      { id: 'resource-db', kind: 'resource', name: 'db', state: 'stopped', transport: null, toolCount: null, replicas: null, pin: null },
      { id: 'client-claude', kind: 'client', name: 'Claude', state: 'linked', transport: 'http', toolCount: null, replicas: null, pin: null },
    ]);
  });

  it('marks a server with enabled false as disabled whatever its status', () => {
    const [r] = buildStackRows([node('mcp-a', { type: 'mcp-server', name: 'a', status: 'error', enabled: false })]);
    expect(r.state).toBe('disabled');
  });

  it('reads replicas from autoscale first, then a positive replica count', () => {
    const rows = buildStackRows([
      node('mcp-a', { type: 'mcp-server', name: 'a', status: 'running', replicaCount: 3, autoscale: { current: 2, target: 4 } }),
      node('mcp-b', { type: 'mcp-server', name: 'b', status: 'running', replicaCount: 3 }),
      node('mcp-c', { type: 'mcp-server', name: 'c', status: 'running', replicaCount: 0 }),
    ]);
    expect(rows.map((r) => r.replicas)).toEqual([{ current: 2, target: 4 }, { current: 3 }, null]);
  });

  it('keeps all four pin values', () => {
    const pins = ['pinned', 'drift', 'blocked', 'approved_pending_redeploy'];
    const rows = buildStackRows(pins.map((p) => node(`mcp-${p}`, { type: 'mcp-server', name: p, status: 'running', pinStatus: p })));
    expect(rows.map((r) => r.pin)).toEqual(pins);
  });
});

describe('client rows', () => {
  const rows = buildStackRows([
    node('mcp-a', { type: 'mcp-server', name: 'a', status: 'running' }),
    node('client-c', { type: 'client', name: 'c', status: 'running', transport: 'http' }),
  ]);
  const client = rows[1];

  it('are linked, match the linked facet, and not running', () => {
    expect(client.state).toBe('linked');
    expect(matchesFilter(client, filter('', ['linked']))).toBe(true);
    expect(matchesFilter(client, filter('', ['running']))).toBe(false);
  });

  it('count under linked in facetCounts', () => {
    const counts = facetCounts(rows);
    expect(counts.running).toBe(1);
    expect(counts.linked).toBe(1);
  });

  it('sort after running rows by status', () => {
    expect(names(sortStackRows([client, rows[0]], 'status', 'asc'))).toEqual(['a', 'c']);
  });
});

describe('attentionRank', () => {
  it('assigns every bucket', () => {
    expect(attentionRank(row({ name: 'a', state: 'error' }))).toBe(0);
    expect(attentionRank(row({ name: 'a', state: 'restarting' }))).toBe(0);
    expect(attentionRank(row({ name: 'a', pin: 'blocked' }))).toBe(0);
    expect(attentionRank(row({ name: 'a', state: 'needs-auth' }))).toBe(1);
    expect(attentionRank(row({ name: 'a', pin: 'drift' }))).toBe(2);
    expect(attentionRank(row({ name: 'a' }))).toBe(3);
    expect(attentionRank(row({ name: 'a', state: 'stopped' }))).toBe(3);
  });

  it('keeps a disabled row at 3 even with a blocked pin', () => {
    expect(attentionRank(row({ name: 'a', state: 'disabled', pin: 'blocked' }))).toBe(3);
  });
});

describe('filtering', () => {
  it('reports activity from a trimmed query or any facet', () => {
    expect(isFilterActive(filter(''))).toBe(false);
    expect(isFilterActive(filter('   '))).toBe(false);
    expect(isFilterActive(filter(' a '))).toBe(true);
    expect(isFilterActive(filter('', ['error']))).toBe(true);
  });

  it('requires every term, case-insensitive', () => {
    const r = row({ name: 'GitHub-Enterprise' });
    expect(matchesFilter(r, filter('git ENTER'))).toBe(true);
    expect(matchesFilter(r, filter('git lab'))).toBe(false);
    expect(matchesFilter(r, filter(''))).toBe(true);
  });

  it('applies each facet kind', () => {
    expect(matchesFilter(row({ name: 'a', state: 'error' }), filter('', ['attention']))).toBe(true);
    expect(matchesFilter(row({ name: 'a' }), filter('', ['attention']))).toBe(false);
    expect(matchesFilter(row({ name: 'a', state: 'idle' }), filter('', ['idle']))).toBe(true);
    expect(matchesFilter(row({ name: 'a', pin: 'drift' }), filter('', ['drift']))).toBe(true);
    expect(matchesFilter(row({ name: 'a', pin: 'blocked' }), filter('', ['blocked']))).toBe(true);
    expect(matchesFilter(row({ name: 'a', pin: 'drift' }), filter('', ['blocked']))).toBe(false);
  });

  it('ORs across facets and ANDs with the query', () => {
    const rows = [
      row({ name: 'alpha', state: 'error' }),
      row({ name: 'beta', state: 'stopped' }),
      row({ name: 'alps', state: 'running' }),
    ];
    expect(names(rows.filter((r) => matchesFilter(r, filter('', ['error', 'stopped']))))).toEqual(['alpha', 'beta']);
    expect(names(rows.filter((r) => matchesFilter(r, filter('al', ['error', 'stopped']))))).toEqual(['alpha']);
  });

  it('counts every facet over unfiltered rows', () => {
    const counts = facetCounts([
      row({ name: 'a', state: 'error', pin: 'drift' }),
      row({ name: 'b', state: 'needs-auth' }),
      row({ name: 'c', state: 'disabled', pin: 'blocked' }),
      row({ name: 'd', kind: 'resource', state: 'running' }),
    ]);
    expect(counts).toMatchObject({
      attention: 2,
      error: 1,
      'needs-auth': 1,
      disabled: 1,
      running: 1,
      drift: 1,
      blocked: 1,
      linked: 0,
    });
  });
});

describe('sortStackRows', () => {
  it('orders status by attention, then the fixed state order, then name', () => {
    const rows = [
      row({ name: 'run' }),
      row({ name: 'link', kind: 'client', state: 'linked' }),
      row({ name: 'dis', state: 'disabled' }),
      row({ name: 'idle', state: 'idle' }),
      row({ name: 'stop', state: 'stopped' }),
      row({ name: 'init', state: 'initializing' }),
      row({ name: 'drift', pin: 'drift' }),
      row({ name: 'auth', state: 'needs-auth' }),
      row({ name: 'restart', state: 'restarting' }),
      row({ name: 'blocked', pin: 'blocked' }),
      row({ name: 'err', state: 'error' }),
    ];
    expect(names(sortStackRows(rows, 'status', 'asc'))).toEqual([
      'err', 'restart', 'blocked', 'auth', 'drift', 'init', 'stop', 'idle', 'dis', 'run', 'link',
    ]);
  });

  it('does not mutate its input', () => {
    const rows = [row({ name: 'b' }), row({ name: 'a' })];
    sortStackRows(rows, 'name', 'asc');
    expect(names(rows)).toEqual(['b', 'a']);
  });

  it('sorts name and kind both ways', () => {
    const rows = [
      row({ name: 'b', kind: 'client' }),
      row({ name: 'a', kind: 'resource' }),
      row({ name: 'c' }),
    ];
    expect(names(sortStackRows(rows, 'name', 'asc'))).toEqual(['a', 'b', 'c']);
    expect(names(sortStackRows(rows, 'name', 'desc'))).toEqual(['c', 'b', 'a']);
    expect(names(sortStackRows(rows, 'kind', 'asc'))).toEqual(['c', 'a', 'b']);
    expect(names(sortStackRows(rows, 'kind', 'desc'))).toEqual(['b', 'a', 'c']);
  });

  it('keeps nulls last in both directions and breaks ties by name', () => {
    const rows = [
      row({ name: 'none', transport: null, toolCount: null, replicas: null, pin: null }),
      row({ name: 'b', transport: 'stdio', toolCount: 2, replicas: { current: 1 }, pin: 'pinned' }),
      row({ name: 'a', transport: 'stdio', toolCount: 2, replicas: { current: 1 }, pin: 'pinned' }),
      row({ name: 'c', transport: 'http', toolCount: 9, replicas: { current: 3, target: 4 }, pin: 'drift' }),
    ];
    expect(names(sortStackRows(rows, 'transport', 'asc'))).toEqual(['c', 'a', 'b', 'none']);
    expect(names(sortStackRows(rows, 'transport', 'desc'))).toEqual(['a', 'b', 'c', 'none']);
    expect(names(sortStackRows(rows, 'tools', 'asc'))).toEqual(['a', 'b', 'c', 'none']);
    expect(names(sortStackRows(rows, 'tools', 'desc'))).toEqual(['c', 'a', 'b', 'none']);
    expect(names(sortStackRows(rows, 'replicas', 'asc'))).toEqual(['a', 'b', 'c', 'none']);
    expect(names(sortStackRows(rows, 'replicas', 'desc'))).toEqual(['c', 'a', 'b', 'none']);
    expect(names(sortStackRows(rows, 'pin', 'asc'))).toEqual(['c', 'a', 'b', 'none']);
    expect(names(sortStackRows(rows, 'pin', 'desc'))).toEqual(['a', 'b', 'c', 'none']);
  });

  it('exports the automatic table threshold', () => {
    expect(STACK_TABLE_AUTO_THRESHOLD).toBe(24);
  });
});
