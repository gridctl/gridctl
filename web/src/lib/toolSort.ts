import type { ToolAnnotations, ToolUsageStat } from '../types';
import type { AuditState } from './toolAudit';

// Filter + sort vocabulary for the Tools workspace list. Pure (no React, no
// clock) so the comparators are unit-testable and the workspace can memoize
// the derived list.

export type AuditFilter = 'all' | 'used' | 'unused' | 'disabled';

export type ToolSortMode = 'default' | 'name' | 'recent' | 'calls';

export const AUDIT_FILTERS: { id: AuditFilter; label: string }[] = [
  { id: 'all', label: 'All' },
  { id: 'used', label: 'Used' },
  { id: 'unused', label: 'Unused' },
  { id: 'disabled', label: 'Disabled' },
];

// `usage: true` marks sorts that need the per-tool usage snapshot; picking
// one widens the workspace's usage-fetch gate even outside Audit Mode.
export const TOOL_SORT_MODES: { id: ToolSortMode; label: string; usage: boolean }[] = [
  { id: 'default', label: 'Default order', usage: false },
  { id: 'name', label: 'Name (A–Z)', usage: false },
  { id: 'recent', label: 'Recently used', usage: true },
  { id: 'calls', label: 'Most calls', usage: true },
];

export function isAuditFilter(v: unknown): v is AuditFilter {
  return AUDIT_FILTERS.some((f) => f.id === v);
}

export function isToolSortMode(v: unknown): v is ToolSortMode {
  return TOOL_SORT_MODES.some((m) => m.id === v);
}

export function sortNeedsUsage(mode: ToolSortMode): boolean {
  return TOOL_SORT_MODES.find((m) => m.id === mode)?.usage ?? false;
}

interface FilterableRow {
  name: string;
  annotations?: ToolAnnotations;
}

// filterToolRows narrows rows by audit state and the destructive risk facet
// (AND semantics). `auditFor` returns null when Audit Mode is off or usage
// hasn't loaded; audit filters are inert then, but the risk facet still
// applies (annotations don't depend on usage).
export function filterToolRows<T extends FilterableRow>(
  rows: T[],
  filter: AuditFilter,
  destructiveOnly: boolean,
  auditFor: (name: string) => AuditState | null,
): T[] {
  let out = rows;
  if (filter !== 'all') {
    out = out.filter((r) => auditFor(r.name) === filter);
  }
  if (destructiveOnly) {
    out = out.filter((r) => r.annotations?.destructiveHint === true);
  }
  return out;
}

// sortToolRows returns a sorted copy (or the input array untouched for
// 'default', preserving server-advertised order). Rows missing the sorted-by
// value sink to the bottom; ties break by name so the order is deterministic.
export function sortToolRows<T extends { name: string }>(
  rows: T[],
  mode: ToolSortMode,
  usage: Record<string, ToolUsageStat> | undefined,
): T[] {
  if (mode === 'default') return rows;
  const byName = (a: T, b: T) => a.name.localeCompare(b.name);
  const sorted = [...rows];
  switch (mode) {
    case 'name':
      sorted.sort(byName);
      break;
    case 'recent':
      sorted.sort((a, b) => {
        const ta = parseTime(usage?.[a.name]?.lastCalledAt);
        const tb = parseTime(usage?.[b.name]?.lastCalledAt);
        if (ta !== tb) return tb - ta;
        return byName(a, b);
      });
      break;
    case 'calls':
      sorted.sort((a, b) => {
        const ca = usage?.[a.name]?.calls ?? 0;
        const cb = usage?.[b.name]?.calls ?? 0;
        if (ca !== cb) return cb - ca;
        return byName(a, b);
      });
      break;
  }
  return sorted;
}

function parseTime(iso: string | undefined): number {
  if (!iso) return Number.NEGATIVE_INFINITY;
  const t = Date.parse(iso);
  return Number.isNaN(t) ? Number.NEGATIVE_INFINITY : t;
}
