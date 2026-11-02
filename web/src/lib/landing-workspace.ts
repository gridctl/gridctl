// Pure helpers for resolving the `/` landing workspace. Lives outside the
// React component file so Fast Refresh and tree-shaking stay happy.
import { prefKey, readPref } from './preferences';
import { isWorkspace, type Workspace } from '../types/workspace';

export const LAST_WORKSPACE_NAME = 'last-workspace';
export const LAST_WORKSPACE_GLOBAL_KEY = prefKey(LAST_WORKSPACE_NAME);
export const LAST_WORKSPACE_PER_STACK_PREFIX = prefKey('last-workspace:');

function readStoredWorkspace(stackId: string | null): Workspace | null {
  try {
    if (stackId) {
      const perStack = readPref(`last-workspace:${stackId}`);
      if (isWorkspace(perStack)) return perStack;
    }
    const global = readPref(LAST_WORKSPACE_NAME);
    if (isWorkspace(global)) return global;
  } catch {
    // localStorage may be unavailable (private browsing); fall through.
  }
  return null;
}

// Resolve the landing workspace for `/` in priority order:
//   1. Per-stack localStorage override (set when the user switches workspaces)
//   2. Global localStorage override
//   3. Heuristic: stack declares skills → /library
//   4. Default: /stack
export function resolveLandingWorkspace(opts: {
  stackId: string | null;
  hasSkills: boolean;
}): Workspace {
  const stored = readStoredWorkspace(opts.stackId);
  if (stored) return stored;
  if (opts.hasSkills) return 'library';
  return 'stack';
}
