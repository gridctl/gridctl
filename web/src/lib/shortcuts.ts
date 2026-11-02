import { WORKSPACE_CONFIG } from '../types/workspace';

export interface ShortcutBinding {
  keys: string[];
  label: string;
  scope: string;
}

export const SHORTCUT_SCOPE = {
  global: 'Global',
  workspaces: 'Workspaces',
  panels: 'Panels',
  lists: 'Lists',
  library: 'Library',
  traces: 'Traces',
  logs: 'Logs',
} as const;

export const SHORTCUT_SCOPE_ORDER: readonly string[] = [
  SHORTCUT_SCOPE.global,
  SHORTCUT_SCOPE.workspaces,
  SHORTCUT_SCOPE.panels,
  SHORTCUT_SCOPE.lists,
  SHORTCUT_SCOPE.library,
  SHORTCUT_SCOPE.traces,
  SHORTCUT_SCOPE.logs,
];

// One static table. Scope follows the handler that fires the chord, not the
// palette hint. Workspace rows are generated so a new workspace cannot drift.
const workspaceBindings: ShortcutBinding[] = WORKSPACE_CONFIG.map((workspace) => ({
  keys: ['⌘', workspace.shortcutKey],
  label: workspace.label,
  scope: SHORTCUT_SCOPE.workspaces,
}));

export const SHORTCUT_BINDINGS: ShortcutBinding[] = [
  { keys: ['f'], label: 'Fit view', scope: SHORTCUT_SCOPE.global },
  { keys: ['⌘', '0'], label: 'Fit view', scope: SHORTCUT_SCOPE.global },
  { keys: ['Escape'], label: 'Close panel', scope: SHORTCUT_SCOPE.global },
  { keys: ['⌘', '+'], label: 'Zoom in', scope: SHORTCUT_SCOPE.global },
  { keys: ['⌘', '−'], label: 'Zoom out', scope: SHORTCUT_SCOPE.global },
  { keys: ['⌘', '⇧', 'R'], label: 'Refresh', scope: SHORTCUT_SCOPE.global },
  { keys: ['⌘', 'J'], label: 'Open Logs', scope: SHORTCUT_SCOPE.global },
  { keys: ['⌘', 'K'], label: 'Command palette', scope: SHORTCUT_SCOPE.global },
  { keys: ['⌘', '\\'], label: 'Toggle compact mode', scope: SHORTCUT_SCOPE.global },
  { keys: ['?'], label: 'Keyboard shortcuts', scope: SHORTCUT_SCOPE.global },
  ...workspaceBindings,
  { keys: ['['], label: 'Toggle left rail', scope: SHORTCUT_SCOPE.panels },
  { keys: [']'], label: 'Toggle right rail', scope: SHORTCUT_SCOPE.panels },
  { keys: ['j'], label: 'Move down', scope: SHORTCUT_SCOPE.lists },
  { keys: ['k'], label: 'Move up', scope: SHORTCUT_SCOPE.lists },
  { keys: ['↓'], label: 'Move down', scope: SHORTCUT_SCOPE.lists },
  { keys: ['↑'], label: 'Move up', scope: SHORTCUT_SCOPE.lists },
  { keys: ['Home'], label: 'Jump to first', scope: SHORTCUT_SCOPE.lists },
  { keys: ['End'], label: 'Jump to last', scope: SHORTCUT_SCOPE.lists },
  { keys: ['Enter'], label: 'Open', scope: SHORTCUT_SCOPE.lists },
  { keys: ['e'], label: 'Edit', scope: SHORTCUT_SCOPE.lists },
  { keys: ['d'], label: 'Toggle', scope: SHORTCUT_SCOPE.lists },
  { keys: ['/'], label: 'Search', scope: SHORTCUT_SCOPE.lists },
  { keys: ['Escape'], label: 'Close', scope: SHORTCUT_SCOPE.lists },
  { keys: ['n'], label: 'New skill', scope: SHORTCUT_SCOPE.library },
  { keys: ['/'], label: 'Search', scope: SHORTCUT_SCOPE.library },
  { keys: ['['], label: 'Toggle trace list', scope: SHORTCUT_SCOPE.traces },
  { keys: ['/'], label: 'Search', scope: SHORTCUT_SCOPE.logs },
  { keys: ['Enter'], label: 'Expand', scope: SHORTCUT_SCOPE.logs },
];
