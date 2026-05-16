import { useDefaultLayout } from 'react-resizable-panels';
import type { Workspace } from '../types/workspace';

interface UseWorkspaceLayoutOptions {
  workspace: Workspace;
  key: string;
  /** Panel ids present on this layout — required when panels are conditionally rendered. */
  panelIds?: string[];
}

/**
 * Workspace-scoped wrapper over react-resizable-panels' useDefaultLayout.
 * Storage key is namespaced as `gridctl:layout:${workspace}:${key}:v1` so
 * future schema changes can introduce `:v2` without breaking existing users.
 */
export function useWorkspaceLayout({ workspace, key, panelIds }: UseWorkspaceLayoutOptions) {
  return useDefaultLayout({
    id: `gridctl:layout:${workspace}:${key}:v1`,
    panelIds,
  });
}

export function workspaceLayoutStorageKey(workspace: Workspace, key: string): string {
  return `gridctl:layout:${workspace}:${key}:v1`;
}
