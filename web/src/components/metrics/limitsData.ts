// Pure helpers for the limits overlay on the metrics surfaces. JSX-free for
// Fast Refresh, mirroring the metricsData.ts / metricsShared.tsx split.
import type { LimitEntry, LimitsReport, LimitState } from '../../lib/api';

// Breakdown scopes map 1:1 onto limit scopes: the clients table matches
// client-scoped entries, and so on. Tool rows use the server-prefixed name,
// which is exactly the tool scope's key format.
export type LimitRowScope = 'client' | 'server' | 'tool';

// slugKey folds a configured key the way the gateway normalizes client
// identities (lowercase, separators to hyphens) so a budget declared as
// "Claude Code" still matches the normalized "claude-code" breakdown row.
// Server and tool keys are matched verbatim; only client keys fold.
function slugKey(key: string): string {
  return key
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9.]+/g, '-')
    .replace(/^-+|-+$/g, '');
}

function entryMatchesRow(entry: LimitEntry, scope: LimitRowScope, rowName: string): boolean {
  if (entry.scope !== scope) return false;
  if (scope === 'client') return slugKey(entry.key) === slugKey(rowName);
  return entry.key === rowName;
}

// budgetForRow returns the budget entry governing a breakdown row, or
// undefined. Duplicate scopes are rejected by config validation, so first
// match is the only match.
export function budgetForRow(
  entries: LimitEntry[] | undefined,
  scope: LimitRowScope,
  rowName: string,
): LimitEntry | undefined {
  return entries?.find((e) => e.kind === 'budget' && entryMatchesRow(e, scope, rowName));
}

// rateForRow returns the rate limit governing a breakdown row, or undefined.
export function rateForRow(
  entries: LimitEntry[] | undefined,
  scope: LimitRowScope,
  rowName: string,
): LimitEntry | undefined {
  return entries?.find((e) => e.kind === 'rate' && entryMatchesRow(e, scope, rowName));
}

// LimitsSummary drives the status-bar chip and the workspace panel ordering.
export interface LimitsSummary {
  configured: boolean;
  entries: LimitEntry[];
  exceededCount: number;
  warnCount: number;
  // Worst state across entries; 'ok' when nothing is elevated.
  worst: LimitState;
}

export function deriveLimitsSummary(report: LimitsReport | null): LimitsSummary {
  const entries = report?.configured ? report.entries : [];
  const exceededCount = entries.filter((e) => e.state === 'exceeded').length;
  const warnCount = entries.filter((e) => e.state === 'warn').length;
  return {
    configured: report?.configured ?? false,
    entries,
    exceededCount,
    warnCount,
    worst: exceededCount > 0 ? 'exceeded' : warnCount > 0 ? 'warn' : 'ok',
  };
}

// State color classes shared by the bars, the panel, and the chip. Amber and
// red reuse the status-pending / status-error tokens the rest of the UI uses
// for the same severities.
export function limitStateTextClass(state: LimitState): string {
  if (state === 'exceeded') return 'text-status-error';
  if (state === 'warn') return 'text-status-pending';
  return 'text-text-muted';
}

export function limitStateFillClass(state: LimitState): string {
  if (state === 'exceeded') return 'bg-status-error';
  if (state === 'warn') return 'bg-status-pending';
  return 'bg-primary/70';
}
