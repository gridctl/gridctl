import { describe, it, expect } from 'vitest';
import {
  filterToolRows,
  isAuditFilter,
  isToolSortMode,
  sortNeedsUsage,
  sortToolRows,
} from '../lib/toolSort';
import type { AuditState } from '../lib/toolAudit';
import type { ToolUsageStat } from '../types';

const rows = [
  { name: 'charlie' },
  { name: 'alpha', annotations: { destructiveHint: true } },
  { name: 'bravo', annotations: { destructiveHint: false } },
];

const audit: Record<string, AuditState> = {
  charlie: 'unused',
  alpha: 'used',
  bravo: 'disabled',
};

const usage: Record<string, ToolUsageStat> = {
  alpha: { calls: 3, lastCalledAt: '2026-07-20T00:00:00Z' },
  charlie: { calls: 10, lastCalledAt: '2026-07-01T00:00:00Z' },
};

describe('filterToolRows', () => {
  it('narrows by audit state', () => {
    const out = filterToolRows(rows, 'used', false, (n) => audit[n] ?? null);
    expect(out.map((r) => r.name)).toEqual(['alpha']);
  });

  it('narrows to server-reported destructive tools only', () => {
    const out = filterToolRows(rows, 'all', true, () => null);
    expect(out.map((r) => r.name)).toEqual(['alpha']);
  });

  it('composes audit and risk facets with AND semantics', () => {
    const out = filterToolRows(rows, 'unused', true, (n) => audit[n] ?? null);
    expect(out).toEqual([]);
  });

  it('is inert for audit filters when classification is unavailable', () => {
    const out = filterToolRows(rows, 'unused', false, () => null);
    expect(out).toEqual([]);
  });
});

describe('sortToolRows', () => {
  it('returns the input array untouched for default order', () => {
    expect(sortToolRows(rows, 'default', usage)).toBe(rows);
  });

  it('sorts by name without mutating the input', () => {
    const out = sortToolRows(rows, 'name', usage);
    expect(out.map((r) => r.name)).toEqual(['alpha', 'bravo', 'charlie']);
    expect(rows[0].name).toBe('charlie');
  });

  it('sorts by most calls with missing usage sinking to the bottom', () => {
    const out = sortToolRows(rows, 'calls', usage);
    expect(out.map((r) => r.name)).toEqual(['charlie', 'alpha', 'bravo']);
  });

  it('sorts by recency with never-called tools last', () => {
    const out = sortToolRows(rows, 'recent', usage);
    expect(out.map((r) => r.name)).toEqual(['alpha', 'charlie', 'bravo']);
  });

});

describe('vocabulary guards', () => {
  it('validates filter and sort tokens for URL/pref parsing', () => {
    expect(isAuditFilter('unused')).toBe(true);
    expect(isAuditFilter('bogus')).toBe(false);
    expect(isToolSortMode('cost')).toBe(false);
    expect(isToolSortMode('')).toBe(false);
  });

  it('marks usage-dependent sorts for the fetch gate', () => {
    expect(sortNeedsUsage('calls')).toBe(true);
    expect(sortNeedsUsage('recent')).toBe(true);
    expect(sortNeedsUsage('name')).toBe(false);
    expect(sortNeedsUsage('default')).toBe(false);
  });
});
