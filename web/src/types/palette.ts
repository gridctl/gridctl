import type { ReactNode } from 'react';

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
  icon?: ReactNode;     // Lucide icon element
  shortcut?: string[];  // e.g., ['Cmd', '0'] for Zoom to fit
  keywords?: string[];  // additional fuzzy match terms
  onSelect: () => void; // action to execute
}
