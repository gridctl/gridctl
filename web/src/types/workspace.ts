import type { LucideIcon } from 'lucide-react';
import { KeyRound, Library, Network, Wrench } from 'lucide-react';

// Top-level workspaces in the unified shell. Routed at /topology, /library,
// /vault, and /tools inside AppShell.
export type Workspace = 'topology' | 'library' | 'vault' | 'tools';

export interface WorkspaceConfig {
  id: Workspace;
  label: string;
  icon: LucideIcon;
  // Single character matched against KeyboardEvent.key with Cmd/Ctrl pressed.
  shortcutKey: string;
}

// Single source of truth for workspace metadata. Adding a workspace = append
// here; the switcher, shortcuts, and labels follow automatically. Array order
// is the rendered top-nav order, so shortcuts read left-to-right as 1-2-3.
export const WORKSPACE_CONFIG: readonly WorkspaceConfig[] = [
  { id: 'topology', label: 'Topology',  icon: Network,  shortcutKey: '1' },
  { id: 'library',  label: 'Library',   icon: Library,  shortcutKey: '2' },
  { id: 'vault',    label: 'Variables', icon: KeyRound, shortcutKey: '3' },
  { id: 'tools',    label: 'Tools',     icon: Wrench,   shortcutKey: '4' },
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
