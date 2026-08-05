import type { PackApplyDoc, PackListItem, PackRow } from '../../../lib/api';
import type { ProjectionState } from '../../ui/StatePill';

/** Kind order matches the manifest's own axis. */
export const PACK_KIND_ORDER = ['skill', 'agent', 'rule', 'wiring', 'unresolved'] as const;

const KIND_LABELS: Record<(typeof PACK_KIND_ORDER)[number], string> = {
  skill: 'Skills',
  agent: 'Agents',
  rule: 'Rules',
  wiring: 'Wiring',
  unresolved: 'Unresolved',
};

export function packKindLabel(kind: string): string {
  return KIND_LABELS[kind as (typeof PACK_KIND_ORDER)[number]] ?? kind;
}

/** A row needs attention when its state or action is anything but clean. */
export function rowNeedsAttention(row: PackRow): boolean {
  const v = row.state || row.action || '';
  if (v === '' || v === 'in-sync' || v === 'missing') {
    // "missing" is clean for skills (never projected); pack rules report
    // missing as a real problem, which the backend encodes as attention
    // at the pack level, but row-level ordering treats rule missing as
    // attention so it sorts first.
    return row.kind === 'rule' && v === 'missing';
  }
  return !['synced', 'updated', 'created', 'unchanged', 'removed', 'would-remove', 'adopted', 'linked'].includes(v);
}

/** The list's attention signal: backend attention, a collision, or an
 *  imported-but-never-applied pack (registry-only is not healthy). */
export function packNeedsAttention(p: PackListItem): boolean {
  return p.needs_attention || !!p.collision || !p.applied;
}

/** Attention-first, then name: the rail sort. */
export function sortPacks(packs: PackListItem[]): PackListItem[] {
  return [...packs].sort((a, b) => {
    const aa = packNeedsAttention(a) ? 0 : 1;
    const bb = packNeedsAttention(b) ? 0 : 1;
    if (aa !== bb) return aa - bb;
    return a.name.localeCompare(b.name);
  });
}

export interface PackRowGroup {
  kind: string;
  label: string;
  rows: PackRow[];
}

/** Group detail rows by kind in manifest order, attention-first inside
 *  each group. Stable within equal attention (backend order preserved). */
export function groupPackRows(rows: PackRow[]): PackRowGroup[] {
  const byKind = new Map<string, PackRow[]>();
  for (const r of rows) {
    const group = byKind.get(r.kind);
    if (group) group.push(r);
    else byKind.set(r.kind, [r]);
  }
  const out: PackRowGroup[] = [];
  for (const kind of PACK_KIND_ORDER) {
    const group = byKind.get(kind);
    if (!group) continue;
    const attention = group.filter(rowNeedsAttention);
    const clean = group.filter((r) => !rowNeedsAttention(r));
    out.push({ kind, label: packKindLabel(kind), rows: [...attention, ...clean] });
    byKind.delete(kind);
  }
  for (const [kind, group] of byKind) {
    out.push({ kind, label: packKindLabel(kind), rows: group });
  }
  return out;
}

/** The StatePill value for a row (states win over actions; actions that
 *  are not states render as plain text elsewhere). */
export function rowPillState(row: PackRow): ProjectionState | null {
  const v = row.state || '';
  switch (v) {
    case 'in-sync':
    case 'stale':
    case 'drifted':
    case 'target-missing':
    case 'never-synced':
    case 'unsupported':
    case 'foreign':
    case 'missing':
    case 'unresolved':
      return v;
    default:
      return null;
  }
}

export interface ApplyOutcome {
  kind: 'success' | 'warning' | 'error';
  message: string;
  /** Names of drifted-skip rows, per kind, for the force follow-up. */
  driftedSkips: PackRow[];
  foreignSkips: PackRow[];
}

/**
 * Grade an apply honestly: success only when every applicable resource
 * applied; a warning names the skipped kinds and counts; errors surface
 * as errors. Never one green checkmark over a partial apply.
 */
export function describeApplyDoc(doc: PackApplyDoc): ApplyOutcome {
  const driftedSkips = doc.rows.filter((r) => r.action === 'skipped-drift');
  const foreignSkips = doc.rows.filter(
    (r) => r.action === 'skipped-foreign-pack' || r.action === 'skipped-foreign',
  );
  const errors = doc.rows.filter((r) => r.action === 'error');
  if (doc.applied === doc.total && errors.length === 0) {
    return {
      kind: 'success',
      message: `Applied ${doc.applied}/${doc.total} resources`,
      driftedSkips,
      foreignSkips,
    };
  }
  const skippedByKind = new Map<string, number>();
  for (const r of doc.rows) {
    if (rowSkipped(r)) skippedByKind.set(r.kind, (skippedByKind.get(r.kind) ?? 0) + 1);
  }
  const parts = [...skippedByKind.entries()].map(
    ([kind, n]) => `${n} ${kind}${n === 1 ? '' : 's'}`,
  );
  const skippedText = parts.length ? ` (skipped: ${parts.join(', ')})` : '';
  return {
    kind: errors.length > 0 ? 'error' : 'warning',
    message: `Applied ${doc.applied}/${doc.total} resources${skippedText}`,
    driftedSkips,
    foreignSkips,
  };
}

function rowSkipped(r: PackRow): boolean {
  const v = r.action ?? '';
  return v.startsWith('skipped') || v === 'error' || v === 'unresolved';
}
