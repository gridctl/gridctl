import type { AgentProjectionStatus, RegistryAgent } from '../../../types';

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
