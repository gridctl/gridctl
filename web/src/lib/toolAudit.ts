import type { MCPServerStatus, ToolUsageStat } from '../types';

// Audit Mode classifies each tool into one of three states relative to a
// lookback window of recorded usage. The logic is pure (no React, no clock
// reads beyond an injected `now`) so it can be unit-tested directly and
// memoized in the workspace without rebuilding on every poll.

export type AuditWindow = '24h' | '7d' | '30d';

export type AuditState = 'used' | 'unused' | 'disabled';

export interface AuditWindowOption {
  id: AuditWindow;
  label: string;
  ms: number;
}

const HOUR = 60 * 60 * 1000;
const DAY = 24 * HOUR;

// Selectable lookback windows. Default is 7d — it matches the gateway's
// `unused_tool` optimize heuristic (defaultFreshnessWindow = 7 * 24h) so the
// two surfaces agree on what "unused" means.
export const AUDIT_WINDOWS: AuditWindowOption[] = [
  { id: '24h', label: '24 hours', ms: 24 * HOUR },
  { id: '7d', label: '7 days', ms: 7 * DAY },
  { id: '30d', label: '30 days', ms: 30 * DAY },
];

export const DEFAULT_AUDIT_WINDOW: AuditWindow = '7d';

export function auditWindowMs(window: AuditWindow): number {
  return AUDIT_WINDOWS.find((w) => w.id === window)?.ms ?? 7 * DAY;
}

// effectiveEnabledTools returns the set of tool names a server actually
// exposes. An empty (or absent) whitelist means "expose all", so every
// advertised tool is enabled — matching the gateway's whitelist semantics
// and the enabled/total badge math.
export function effectiveEnabledTools(server: MCPServerStatus): Set<string> {
  const wl = server.toolWhitelist;
  if (wl && wl.length > 0) return new Set(wl);
  return new Set(server.tools);
}

// classifyTool decides a tool's audit state.
//   - disabled: not currently exposed (precedence over usage — a disabled
//     tool's past activity is irrelevant to whether it's exposed now).
//   - used:     exposed and called within the window.
//   - unused:   exposed but no call within the window (includes "never
//     called", which the UI labels distinctly so it never implies a longer
//     disuse history than the data supports).
export function classifyTool(
  enabled: boolean,
  lastCalledAt: string | undefined,
  windowMs: number,
  now: number,
): AuditState {
  if (!enabled) return 'disabled';
  if (lastCalledAt) {
    const t = Date.parse(lastCalledAt);
    if (!Number.isNaN(t) && now - t <= windowMs) return 'used';
  }
  return 'unused';
}

// unusedEnabledTools returns the names of a server's exposed tools that have
// no activity within the window — the remediation target for "disable unused".
// `usage` is the per-tool map for this server from GET /api/tools/usage.
export function unusedEnabledTools(
  server: MCPServerStatus,
  usage: Record<string, ToolUsageStat> | undefined,
  windowMs: number,
  now: number,
): string[] {
  const enabled = effectiveEnabledTools(server);
  const out: string[] = [];
  for (const name of enabled) {
    const last = usage?.[name]?.lastCalledAt;
    if (classifyTool(true, last, windowMs, now) === 'unused') out.push(name);
  }
  return out;
}

// formatLastUsed renders a tool's last-call time for the audit row. Extends
// the shared formatRelativeTime vocabulary to days/weeks since audit windows
// span up to 30 days (where "720h ago" would read poorly). Undefined input
// (never recorded) yields a caller-supplied placeholder.
export function formatLastUsed(lastCalledAt: string | undefined): string {
  if (!lastCalledAt) return 'no recorded calls';
  const t = Date.parse(lastCalledAt);
  if (Number.isNaN(t)) return 'no recorded calls';
  const seconds = Math.floor((Date.now() - t) / 1000);
  if (seconds < 10) return 'just now';
  if (seconds < 60) return `${seconds}s ago`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  if (days < 7) return `${days}d ago`;
  const weeks = Math.floor(days / 7);
  return `${weeks}w ago`;
}
