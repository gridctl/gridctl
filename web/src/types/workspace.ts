import type { LucideIcon } from 'lucide-react';
import { Code, Library, Network, PlayCircle } from 'lucide-react';

// Top-level workspaces in the unified shell. Routed at /topology, /skills,
// /library, /runs inside AppShell. The workspace id `skills` keeps the
// historical route + storage value even though the user-facing label is
// "Stage" — see WORKSPACE_CONFIG below.
export type Workspace = 'topology' | 'skills' | 'library' | 'runs';

export interface WorkspaceConfig {
  id: Workspace;
  label: string;
  icon: LucideIcon;
  // Single character matched against KeyboardEvent.key with Cmd/Ctrl pressed.
  shortcutKey: string;
}

// Single source of truth for workspace metadata. Adding a workspace = append
// here; the switcher, shortcuts, and labels follow automatically. Array order
// is the rendered top-nav order, so shortcuts read left-to-right as 1-2-3-4.
export const WORKSPACE_CONFIG: readonly WorkspaceConfig[] = [
  { id: 'topology', label: 'Topology', icon: Network,    shortcutKey: '1' },
  { id: 'skills',   label: 'Stage',    icon: Code,       shortcutKey: '2' },
  { id: 'library',  label: 'Library',  icon: Library,    shortcutKey: '3' },
  { id: 'runs',     label: 'Runs',     icon: PlayCircle, shortcutKey: '4' },
] as const;

// Derived for back-compat with existing call-sites in useUIStore, AppShell,
// landing-workspace, etc. Migrate them to WORKSPACE_CONFIG opportunistically.
export const WORKSPACES: readonly Workspace[] = WORKSPACE_CONFIG.map((w) => w.id);

export const WORKSPACE_LABELS: Record<Workspace, string> = Object.fromEntries(
  WORKSPACE_CONFIG.map((w) => [w.id, w.label]),
) as Record<Workspace, string>;

export function isWorkspace(value: unknown): value is Workspace {
  return typeof value === 'string' && (WORKSPACES as readonly string[]).includes(value);
}
