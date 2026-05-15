// The three top-level workspaces in the unified shell. The `/topology`,
// `/skills`, and `/runs` routes each render one workspace inside AppShell.
export type Workspace = 'topology' | 'skills' | 'runs';

export const WORKSPACES: readonly Workspace[] = ['topology', 'skills', 'runs'] as const;

export function isWorkspace(value: unknown): value is Workspace {
  return value === 'topology' || value === 'skills' || value === 'runs';
}

export const WORKSPACE_LABELS: Record<Workspace, string> = {
  topology: 'Topology',
  skills: 'Skills',
  runs: 'Runs',
};
