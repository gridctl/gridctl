import type { HookProjectionStatus, HookSyncResult, RegistryHook } from '../../../types';

const CLIENT_NAMES: Record<string, string> = {
  'claude-code': 'Claude Code',
  gemini: 'Gemini CLI',
  cursor: 'Cursor',
  copilot: 'GitHub Copilot',
  windsurf: 'Windsurf (Devin Local)',
  antigravity: 'Antigravity',
  opencode: 'OpenCode',
};

export function hookClientName(slug: string): string {
  return CLIENT_NAMES[slug] ?? slug;
}

export function hookSourceKey(hook: RegistryHook): string {
  return hook.source && hook.source !== '' ? hook.source : 'Local';
}

// projectedScriptPath joins a definition command onto the copied script
// directory. Without a projection, the definition path is all that exists.
export function projectedScriptPath(command: string, scriptDir?: string): string {
  if (!scriptDir) return command;
  const rel = command.replace(/^\.\//, '');
  return `${scriptDir.replace(/\/$/, '')}/${rel}`;
}

export function statusesByHook(rows: HookProjectionStatus[] | null): Map<string, HookProjectionStatus[]> {
  const out = new Map<string, HookProjectionStatus[]>();
  for (const row of rows ?? []) {
    const list = out.get(row.hook);
    if (list) list.push(row);
    else out.set(row.hook, [row]);
  }
  return out;
}

export function needsHookSync(row: HookProjectionStatus): boolean {
  return row.state === 'stale' || row.state === 'target-missing';
}

export function describeHookSync(results: HookSyncResult[]): { kind: 'success' | 'error'; message: string } {
  const failed = results.filter((r) => r.action === 'error' || r.action.startsWith('skipped-'));
  if (failed.length === 0) {
    return { kind: 'success', message: results.length === 0 ? 'Nothing to sync' : 'Hook sync finished' };
  }
  const first = failed[0];
  return {
    kind: 'error',
    message: `${first.hook ?? 'hook'} → ${hookClientName(first.client)}: ${first.detail || first.action}`,
  };
}
