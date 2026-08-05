import type { AgentProjectionStatus, AgentSyncResult, RegistryAgent } from '../../../types';

/** Display names for the projection targets; falls back to the slug. */
const CLIENT_NAMES: Record<string, string> = {
  'claude-code': 'Claude Code',
  opencode: 'OpenCode',
  copilot: 'GitHub Copilot',
  gemini: 'Gemini CLI',
};

export function agentClientName(slug: string): string {
  return CLIENT_NAMES[slug] ?? slug;
}

/** States that need the user's attention and feed the "Sync stale" pill. */
export function needsSync(s: AgentProjectionStatus): boolean {
  return s.state === 'stale' || s.state === 'target-missing';
}

/** Per-agent status rows, keyed by agent name. */
export function statusesByAgent(
  statuses: AgentProjectionStatus[] | null,
): Map<string, AgentProjectionStatus[]> {
  const map = new Map<string, AgentProjectionStatus[]>();
  for (const s of statuses ?? []) {
    const rows = map.get(s.agent);
    if (rows) rows.push(s);
    else map.set(s.agent, [s]);
  }
  return map;
}

/** Per-client status rows, keyed by client slug (the transpose of
 *  statusesByAgent, for surfaces that answer "what does this client
 *  receive?"). */
export function statusesByClient(
  statuses: AgentProjectionStatus[] | null,
): Map<string, AgentProjectionStatus[]> {
  const map = new Map<string, AgentProjectionStatus[]>();
  for (const s of statuses ?? []) {
    const rows = map.get(s.client);
    if (rows) rows.push(s);
    else map.set(s.client, [s]);
  }
  return map;
}

/** Slugs agentsync can project to. Derived from the status rows plus the
 *  known target table; used to distinguish "not an agent projection
 *  target" from "target with nothing projected yet". */
export const AGENT_TARGET_SLUGS = new Set(Object.keys(CLIENT_NAMES));

/**
 * Honest classification of a sync pass. The engine reports skips without
 * an error field (skipped-unavailable for undetected clients,
 * skipped-drift for hand edits), so keying a toast on `error` alone can
 * announce success while nothing was written. Mirrors the vocabulary in
 * pkg/agentsync ops.go.
 */
export function describeSyncResults(results: AgentSyncResult[]): {
  kind: 'success' | 'warning';
  message: string;
} {
  let applied = 0;
  let failed = 0;
  let skippedDrift = 0;
  let unavailable = 0;
  let unchanged = 0;
  for (const r of results) {
    switch (r.action) {
      case 'copied':
      case 'updated':
      case 'removed':
      case 'would-copy':
      case 'would-update':
      case 'would-remove':
        applied++;
        break;
      case 'skipped-drift':
        skippedDrift++;
        break;
      case 'skipped-unavailable':
        unavailable++;
        break;
      case 'error':
      case 'skipped-unmanaged':
        failed++;
        break;
      default:
        unchanged++;
    }
  }
  if (failed > 0 || skippedDrift > 0) {
    const parts: string[] = [];
    if (applied > 0) parts.push(`${applied} synced`);
    if (skippedDrift > 0) parts.push(`${skippedDrift} skipped (local edits; use Review)`);
    if (failed > 0) parts.push(`${failed} failed`);
    return { kind: 'warning', message: parts.join(', ') };
  }
  if (applied === 0) {
    if (unavailable > 0 && unchanged === 0) {
      return { kind: 'warning', message: 'No agent clients detected; nothing was projected' };
    }
    return { kind: 'success', message: 'Already in sync' };
  }
  return { kind: 'success', message: `Synced ${applied} projection${applied === 1 ? '' : 's'}` };
}

/** The frontmatter keys the UI treats as portable-but-translated. */
export function agentExtraValue(agent: RegistryAgent, key: string): string | null {
  const field = agent.extra?.find((f) => f.key === key);
  if (field === undefined || field.value === null || field.value === undefined) return null;
  return typeof field.value === 'string' ? field.value : JSON.stringify(field.value);
}

/** Render one extra value for the read-only frontmatter list. */
export function formatExtraValue(value: unknown): string {
  if (value === null || value === undefined) return '';
  if (typeof value === 'string') return value;
  return JSON.stringify(value);
}

/**
 * Body extracted from a raw AGENT.md for previewing: everything after the
 * closing frontmatter delimiter. Falls back to the whole text when no
 * frontmatter block is present (the server rejects such saves anyway).
 */
export function bodyFromRaw(raw: string): string {
  const normalized = raw.replace(/\r\n/g, '\n');
  if (!normalized.trimStart().startsWith('---')) return normalized;
  const lines = normalized.split('\n');
  let delimiters = 0;
  for (let i = 0; i < lines.length; i++) {
    if (lines[i].trim() === '---') {
      delimiters++;
      if (delimiters === 2) return lines.slice(i + 1).join('\n').replace(/^\n/, '');
    }
  }
  return normalized;
}
