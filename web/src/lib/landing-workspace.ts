// Pure helpers for resolving the `/` landing workspace. Lives outside the
// React component file so Fast Refresh and tree-shaking stay happy.
import { isWorkspace, type Workspace } from '../types/workspace';

export const LAST_WORKSPACE_GLOBAL_KEY = 'gridctl:last-workspace';
export const LAST_WORKSPACE_PER_STACK_PREFIX = 'gridctl:last-workspace:';

function readStoredWorkspace(stackId: string | null): Workspace | null {
  try {
    if (stackId) {
      const perStack = localStorage.getItem(`${LAST_WORKSPACE_PER_STACK_PREFIX}${stackId}`);
      if (isWorkspace(perStack)) return perStack;
    }
    const global = localStorage.getItem(LAST_WORKSPACE_GLOBAL_KEY);
    if (isWorkspace(global)) return global;
  } catch {
    // localStorage may be unavailable (private browsing); fall through.
  }
  return null;
}

// Resolve the landing workspace for `/` in priority order:
//   1. Per-stack localStorage override (set when the user switches workspaces)
//   2. Global localStorage override
//   3. Heuristic: stack declares typed skills → /skills
//   4. Default: /topology
export function resolveLandingWorkspace(opts: {
  stackId: string | null;
  hasSkills: boolean;
}): Workspace {
  const stored = readStoredWorkspace(opts.stackId);
  if (stored) return stored;
  if (opts.hasSkills) return 'skills';
  return 'topology';
}
