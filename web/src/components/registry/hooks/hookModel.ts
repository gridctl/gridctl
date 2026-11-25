import type {
  HookProjectionStatus,
  HookSyncResult,
  HookUnsupportedClient,
  HookUnsyncResult,
  RegistryHook,
} from '../../../types';

const CLIENT_NAMES: Record<string, string> = {
  'claude-code': 'Claude Code',
  gemini: 'Gemini CLI',
  cursor: 'Cursor',
  copilot: 'GitHub Copilot CLI',
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

// joinProse joins names as English prose with an Oxford comma.
export function joinProse(names: string[]): string {
  if (names.length <= 1) return names.join('');
  if (names.length === 2) return `${names[0]} and ${names[1]}`;
  return `${names.slice(0, -1).join(', ')}, and ${names[names.length - 1]}`;
}

export interface HookUnsyncPlan {
  /** Clients whose projection unsync removes, in row order. */
  removable: string[];
  /** Drifted clients that unsync without force leaves in place. */
  kept: string[];
  /** Detected clients that read a removable client's projection. */
  covered: { names: string[]; by: string }[];
}

// planHookUnsync splits projection rows into the clients an unsync removes
// and the drifted clients it keeps, plus detected clients that read a
// removed projection and so stop running the hook too.
export function planHookUnsync(
  statuses: HookProjectionStatus[],
  unsupported: HookUnsupportedClient[],
): HookUnsyncPlan {
  const removable: string[] = [];
  const kept: string[] = [];
  for (const row of statuses) {
    const list = row.state === 'drifted' ? kept : removable;
    if (!list.includes(row.client)) list.push(row.client);
  }
  const covered: { names: string[]; by: string }[] = [];
  for (const client of removable) {
    if (kept.includes(client)) continue;
    const names = unsupported.filter((u) => u.detected && u.covered_by === client).map((u) => u.name);
    if (names.length > 0) covered.push({ names, by: client });
  }
  return { removable: removable.filter((c) => !kept.includes(c)), kept, covered };
}

// describeHookUnsync grades an unsync result. A drift or foreign skip is a
// refusal, so it surfaces as an error alongside any clients that were removed.
export function describeHookUnsync(results: HookUnsyncResult[]): { kind: 'success' | 'error'; message: string } {
  if (results.length === 0) return { kind: 'success', message: 'Nothing to unsync' };
  const failed = results.filter((r) => r.action === 'error' || r.action.startsWith('skipped-'));
  if (failed.length === 0) {
    const n = results.length;
    return { kind: 'success', message: `Unsynced ${results[0].hook} from ${n} client${n === 1 ? '' : 's'}` };
  }
  const first = failed[0];
  const reason = first.error || first.detail || first.action;
  const removed = results.filter((r) => r.action === 'removed').length;
  const suffix = removed > 0 ? `; removed from ${removed} other client${removed === 1 ? '' : 's'}` : '';
  return { kind: 'error', message: `${first.hook} → ${hookClientName(first.client)}: ${reason}${suffix}` };
}
