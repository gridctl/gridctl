import type { LucideIcon } from 'lucide-react';
import { Library, Network } from 'lucide-react';

// Top-level workspaces in the unified shell. Routed at /topology and /library
// inside AppShell.
export type Workspace = 'topology' | 'library';

export interface WorkspaceConfig {
  id: Workspace;
  label: string;
  icon: LucideIcon;
  // Single character matched against KeyboardEvent.key with Cmd/Ctrl pressed.
  shortcutKey: string;
}

// Single source of truth for workspace metadata. Adding a workspace = append
// here; the switcher, shortcuts, and labels follow automatically. Array order
// is the rendered top-nav order, so shortcuts read left-to-right as 1-2.
export const WORKSPACE_CONFIG: readonly WorkspaceConfig[] = [
  { id: 'topology', label: 'Topology', icon: Network, shortcutKey: '1' },
  { id: 'library',  label: 'Library',  icon: Library, shortcutKey: '2' },
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
