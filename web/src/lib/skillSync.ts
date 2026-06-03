import type { SkillSyncResult } from '../types';

export interface SyncCounts {
  updated: number;
  skipped: number;
  overwritten: number;
  failed: number;
}

/**
 * Tally per-skill sync outcomes into discrete buckets. A force-overwritten
 * skill carries a backup file name (and would also report `imported`), so it is
 * counted as overwritten rather than a plain update; a skipped skill carries a
 * `skipped` reason; the rest with `imported > 0` are plain updates.
 */
export function summarizeSkillResults(results: SkillSyncResult[] | undefined): SyncCounts {
  const c: SyncCounts = { updated: 0, skipped: 0, overwritten: 0, failed: 0 };
  for (const r of results ?? []) {
    if (r.error) c.failed++;
    else if (r.skipped) c.skipped++;
    else if (r.backup) c.overwritten++;
    else if (r.imported) c.updated++;
  }
  return c;
}

/** Add b into a, returning a new total. */
export function addCounts(a: SyncCounts, b: SyncCounts): SyncCounts {
  return {
    updated: a.updated + b.updated,
    skipped: a.skipped + b.skipped,
    overwritten: a.overwritten + b.overwritten,
    failed: a.failed + b.failed,
  };
}

/**
 * Human phrase for a set of sync counts, e.g. "2 updated, 1 kept, 1
 * overwritten". Returns an empty string when nothing changed, so callers can
 * fall back to an "up to date" message.
 */
export function syncCountsMessage(c: SyncCounts): string {
  const parts: string[] = [];
  if (c.updated) parts.push(`${c.updated} updated`);
  if (c.overwritten) parts.push(`${c.overwritten} overwritten`);
  if (c.skipped) parts.push(`${c.skipped} kept`);
  if (c.failed) parts.push(`${c.failed} failed`);
  return parts.join(', ');
}
