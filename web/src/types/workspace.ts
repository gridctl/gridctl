import type { LucideIcon } from 'lucide-react';
import { BarChart3, Library, Lock, Network, Pin, Wrench } from 'lucide-react';

// Top-level workspaces in the unified shell. Routed at /topology, /library,
// /vault, /tools, /metrics, and /pins inside AppShell.
export type Workspace = 'topology' | 'library' | 'vault' | 'tools' | 'metrics' | 'pins';

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
  { id: 'vault',    label: 'Variables', icon: Lock,     shortcutKey: '3' },
  { id: 'tools',    label: 'Tools',     icon: Wrench,    shortcutKey: '4' },
  { id: 'metrics',  label: 'Metrics',   icon: BarChart3, shortcutKey: '5' },
  { id: 'pins',     label: 'Pins',      icon: Pin,       shortcutKey: '6' },
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

// Base product name used for the document title.
export const DOCUMENT_TITLE_BASE = 'Gridctl';

// Builds the browser tab title for the active workspace, e.g. "Gridctl -
// Variables". Falls back to the base name for non-workspace or transitional
// paths so the tab never shows "Gridctl - undefined".
export function documentTitleForWorkspace(ws: Workspace | null): string {
  return ws ? `${DOCUMENT_TITLE_BASE} - ${WORKSPACE_LABELS[ws]}` : DOCUMENT_TITLE_BASE;
}
