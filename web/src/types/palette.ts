import type { ReactNode } from 'react';
import type { Workspace } from './workspace';

export type PaletteSection =
  | 'traces'
  | 'vault'
  | 'registry'
  | 'canvas'
  | 'logs'
  | 'metrics'
  | 'global';

export interface PaletteCommand {
  id: string;           // unique, stable ID for frecency tracking
  label: string;        // display text
  section: PaletteSection;
  // Workspaces in which this command is visible. Omit to show in all
  // workspaces (the default). Cross-workspace navigation commands omit it;
  // workspace-specific commands list the workspaces they belong to.
  workspaces?: Workspace[];
  icon?: ReactNode;     // Lucide icon element
  shortcut?: string[];  // e.g., ['Cmd', '0'] for Zoom to fit
  keywords?: string[];  // additional fuzzy match terms
  onSelect: () => void; // action to execute
  unavailable?: boolean; // show unavailable indicator; toast on select instead of executing
}
