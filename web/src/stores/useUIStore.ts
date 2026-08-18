import { create } from 'zustand';
import type { StateCreator } from 'zustand';
import { persist } from 'zustand/middleware';
import { WORKSPACES, type Workspace } from '../types/workspace';
import { DEFAULT_THEME_MODE, isThemeMode, type ThemeMode } from '../themes/types';
import { DEFAULT_LOG_WINDOW, LOG_WINDOW_SIZES } from '../components/log/logTypes';
import {
  isAuditFilter,
  isToolSortMode,
  type AuditFilter,
  type ToolSortMode,
} from '../lib/toolSort';

type SidebarTab = 'details' | 'tools' | 'logs';
type EdgeStyle = 'default' | 'straight'; // 'default' = Bezier curves

// Cross-workspace shell state. Lives on useUIStore via the Zustand slices
// pattern — never reach into a workspace-specific store from here.
export interface WorkspaceSlice {
  activeWorkspace: Workspace;
  setActiveWorkspace: (ws: Workspace) => void;
}

export const createWorkspaceSlice: StateCreator<
  UIState,
  [['zustand/persist', unknown]],
  [],
  WorkspaceSlice
> = (set) => ({
  activeWorkspace: 'stack',
  setActiveWorkspace: (activeWorkspace) => set({ activeWorkspace }),
});

// Compact Mode is workspace-scoped — both workspaces default to roomier
// layouts; flip per-workspace via toggleCompactMode.
export type CompactModeMap = Record<Workspace, boolean>;

export const COMPACT_MODE_DEFAULTS: CompactModeMap = {
  stack: false,
  library: false,
  vault: false,
  tools: false,
  metrics: false,
  pins: false,
  logs: false,
  traces: false,
  connections: false,
};

export interface CompactModeSlice {
  compactMode: CompactModeMap;
  setCompactMode: (workspace: Workspace, value: boolean) => void;
  toggleCompactMode: (workspace: Workspace) => void;
}

export const createCompactModeSlice: StateCreator<
  UIState,
  [['zustand/persist', unknown]],
  [],
  CompactModeSlice
> = (set) => ({
  compactMode: { ...COMPACT_MODE_DEFAULTS },
  setCompactMode: (workspace, value) =>
    set((s) => ({ compactMode: { ...s.compactMode, [workspace]: value } })),
  toggleCompactMode: (workspace) =>
    set((s) => ({
      compactMode: { ...s.compactMode, [workspace]: !s.compactMode[workspace] },
    })),
});

// Persisted shape may drift from the canonical workspace keys across versions
// — coerce so a stale localStorage payload never leaves a workspace with
// `undefined` compact state at boot.
function normalizeCompactMode(raw: unknown): CompactModeMap {
  const out = { ...COMPACT_MODE_DEFAULTS };
  if (raw && typeof raw === 'object') {
    for (const ws of WORKSPACES) {
      const v = (raw as Record<string, unknown>)[ws];
      if (typeof v === 'boolean') out[ws] = v;
    }
  }
  return out;
}

// Skill editor view preferences, persisted so the editor reopens the way the
// user left it. Body-heavy split and collapsed frontmatter are the defaults for
// existing skills (a new skill forces frontmatter open so its fields are fillable).
export interface EditorPrefs {
  showFrontmatter: boolean;
  showPreview: boolean;
  splitRatio: number;
}

export const EDITOR_PREFS_DEFAULTS: EditorPrefs = {
  showFrontmatter: false,
  showPreview: true,
  splitRatio: 0.62,
};

interface UIState extends WorkspaceSlice, CompactModeSlice {
  sidebarOpen: boolean;
  activeTab: SidebarTab;
  edgeStyle: EdgeStyle;

  // Appearance: light / dark / system. The resolved theme is applied to <html>
  // by themes/useThemeSync; this just holds the user's choice. Persisted.
  themeMode: ThemeMode;
  setThemeMode: (mode: ThemeMode) => void;

  // Skill editor view preferences
  editorPrefs: EditorPrefs;
  setEditorPrefs: (prefs: Partial<EditorPrefs>) => void;

  // Compact card mode
  compactCards: boolean;

  // Token heat overlay on graph nodes
  showHeatMap: boolean;

  // Canvas spec mode — shows ghost nodes for undeployed spec items and
  // drift indicators for items that diverge from the running stack
  showSpecMode: boolean;

  // Detached window state
  logsDetached: boolean;
  sidebarDetached: boolean;
  editorDetached: boolean;
  registryDetached: boolean;
  metricsDetached: boolean;
  tracesDetached: boolean;

  // Actions
  setSidebarOpen: (open: boolean) => void;
  toggleSidebar: () => void;
  setActiveTab: (tab: SidebarTab) => void;
  setEdgeStyle: (style: EdgeStyle) => void;
  toggleEdgeStyle: () => void;
  toggleCompactCards: () => void;
  toggleHeatMap: () => void;
  toggleSpecMode: () => void;

  // Detached window actions
  setLogsDetached: (detached: boolean) => void;
  setSidebarDetached: (detached: boolean) => void;
  setEditorDetached: (detached: boolean) => void;
  setRegistryDetached: (detached: boolean) => void;
  setMetricsDetached: (detached: boolean) => void;
  setTracesDetached: (detached: boolean) => void;

  // Command palette state (not persisted)
  commandPaletteOpen: boolean;
  setCommandPaletteOpen: (open: boolean) => void;
  toggleCommandPalette: () => void;

  // Reset dialog (the machine-wide `gridctl reset` surface): opened from
  // the Connections danger zone or the command palette. Transient.
  resetDialogOpen: boolean;
  setResetDialogOpen: (open: boolean) => void;

  // Per-client access editor: opened from the Stack inspector ("Edit Scope")
  // seeded to a specific client. Transient (not persisted).
  accessEditorOpen: boolean;
  accessEditorSeedSlug: string | null;
  openAccessEditor: (slug?: string | null) => void;
  closeAccessEditor: () => void;

  // Traces list preferences shared by the workspace and the detached window.
  // Persisted; URL params still win on the in-shell workspace.
  tracesPrefs: TracesPrefs;
  setTracesPrefs: (prefs: Partial<TracesPrefs>) => void;

  // Logs view preferences shared by the workspace and the detached window.
  // Persisted; URL params always win when present.
  logsPrefs: LogsPrefs;
  setLogsPrefs: (prefs: Partial<LogsPrefs>) => void;

  // Pins workspace view preferences. Persisted; URL params always win.
  pinsPrefs: PinsPrefs;
  setPinsPrefs: (prefs: Partial<PinsPrefs>) => void;

  // Tools workspace list preferences (audit filter, sort, risk facet).
  // Persisted; URL params always win.
  toolsPrefs: ToolsPrefs;
  setToolsPrefs: (prefs: Partial<ToolsPrefs>) => void;

  // Library workspace view preferences (select mode).
  libraryPrefs: LibraryPrefs;
  setLibraryPrefs: (prefs: Partial<LibraryPrefs>) => void;
}

interface TracesPrefs {
  segment: 'tool-calls' | 'all';
  server: string;
  /** Waterfall span-name column width as a percentage of the waterfall pane. */
  nameColPct: number;
}

export const TRACES_NAME_COL_MIN_PCT = 18;
export const TRACES_NAME_COL_MAX_PCT = 45;
const TRACES_NAME_COL_DEFAULT_PCT = 30;

function clampNameColPct(pct: number): number {
  if (!Number.isFinite(pct)) return TRACES_NAME_COL_DEFAULT_PCT;
  return Math.min(TRACES_NAME_COL_MAX_PCT, Math.max(TRACES_NAME_COL_MIN_PCT, pct));
}

const TRACES_PREFS_DEFAULTS: TracesPrefs = {
  segment: 'tool-calls',
  server: '',
  nameColPct: TRACES_NAME_COL_DEFAULT_PCT,
};

function normalizeTracesPrefs(value: unknown): TracesPrefs {
  const v = (value ?? {}) as Partial<TracesPrefs>;
  return {
    segment: v.segment === 'all' ? 'all' : 'tool-calls',
    server: typeof v.server === 'string' ? v.server : '',
    nameColPct: typeof v.nameColPct === 'number' ? clampNameColPct(v.nameColPct) : TRACES_NAME_COL_DEFAULT_PCT,
  };
}

export interface LogsPrefs {
  /** Serialized ?level= param value ('' = all levels; round-trips `none`). */
  levelParam: string;
  /** Preferred source token ('' = all sources). */
  source: string;
  /** Soft-wrap long messages in collapsed rows. */
  wrap: boolean;
  /** Show relative timestamps instead of absolute HH:MM:SS.mmm. */
  relativeTime: boolean;
  /** Poll window size (one of LOG_WINDOW_SIZES). */
  windowSize: number;
}

const LOGS_PREFS_DEFAULTS: LogsPrefs = {
  levelParam: '',
  source: '',
  wrap: false,
  relativeTime: false,
  windowSize: DEFAULT_LOG_WINDOW,
};

function normalizeLogsPrefs(value: unknown): LogsPrefs {
  const v = (value ?? {}) as Partial<LogsPrefs>;
  return {
    levelParam: typeof v.levelParam === 'string' ? v.levelParam : '',
    source: typeof v.source === 'string' ? v.source : '',
    wrap: typeof v.wrap === 'boolean' ? v.wrap : false,
    relativeTime: typeof v.relativeTime === 'boolean' ? v.relativeTime : false,
    windowSize: (LOG_WINDOW_SIZES as readonly number[]).includes(v.windowSize as number)
      ? (v.windowSize as number)
      : DEFAULT_LOG_WINDOW,
  };
}

export interface PinsPrefs {
  /**
   * Rail attention filter: true/false is an explicit user choice; null means
   * automatic (on exactly when any server has drift or warn+ findings).
   */
  attentionOnly: boolean | null;
  /** Pinned-records table filtered to tools with findings. */
  findingsOnly: boolean;
}

const PINS_PREFS_DEFAULTS: PinsPrefs = {
  attentionOnly: null,
  findingsOnly: false,
};

function normalizePinsPrefs(value: unknown): PinsPrefs {
  const v = (value ?? {}) as Partial<PinsPrefs>;
  return {
    attentionOnly: typeof v.attentionOnly === 'boolean' ? v.attentionOnly : null,
    findingsOnly: typeof v.findingsOnly === 'boolean' ? v.findingsOnly : false,
  };
}

export interface ToolsPrefs {
  /** Audit-state filter chip (only bites while Audit Mode is on). */
  filter: AuditFilter;
  /** List sort mode ('default' = server-advertised order). */
  sort: ToolSortMode;
  /** Risk facet: only tools reported destructive by their server. */
  destructiveOnly: boolean;
}

const TOOLS_PREFS_DEFAULTS: ToolsPrefs = {
  filter: 'all',
  sort: 'default',
  destructiveOnly: false,
};

function normalizeToolsPrefs(value: unknown): ToolsPrefs {
  const v = (value ?? {}) as Partial<ToolsPrefs>;
  return {
    filter: isAuditFilter(v.filter) ? v.filter : 'all',
    sort: isToolSortMode(v.sort) ? v.sort : 'default',
    destructiveOnly: typeof v.destructiveOnly === 'boolean' ? v.destructiveOnly : false,
  };
}

export interface LibraryPrefs {
  /**
   * Pin the multi-select checkboxes visible. Off by default, matching the
   * hover-reveal the cards shipped with; on, the checkboxes stay put so bulk
   * actions are discoverable without a pointer. A display preference, not a
   * view facet, so it lives here rather than in the URL.
   */
  selectMode: boolean;
}

const LIBRARY_PREFS_DEFAULTS: LibraryPrefs = {
  selectMode: false,
};

function normalizeLibraryPrefs(value: unknown): LibraryPrefs {
  const v = (value ?? {}) as Partial<LibraryPrefs>;
  return {
    selectMode: typeof v.selectMode === 'boolean' ? v.selectMode : false,
  };
}

export const useUIStore = create<UIState>()(
  persist(
    (set, get, store) => ({
      ...createWorkspaceSlice(set, get, store),
      ...createCompactModeSlice(set, get, store),
      sidebarOpen: false,
      activeTab: 'details',
      edgeStyle: 'default', // Bezier curves

      // Appearance — defaults to 'system' so the OS preference (incl. an
      // accessibility choice) is honored on first run; resolves to dark when
      // the OS has no preference.
      themeMode: DEFAULT_THEME_MODE,
      setThemeMode: (themeMode) => set({ themeMode }),

      // Skill editor view preferences
      editorPrefs: { ...EDITOR_PREFS_DEFAULTS },
      setEditorPrefs: (prefs) =>
        set((s) => ({ editorPrefs: { ...s.editorPrefs, ...prefs } })),

      // Compact cards default — the consolidated node view is the default;
      // full cards are the opt-in.
      compactCards: true,

      // Token heat overlay default
      showHeatMap: false,

      // Spec mode default
      showSpecMode: false,

      // Detached window defaults
      logsDetached: false,
      sidebarDetached: false,
      editorDetached: false,
      registryDetached: false,
      metricsDetached: false,
      tracesDetached: false,

      // Command palette (always starts closed)
      commandPaletteOpen: false,

      // Reset dialog (always starts closed)
      resetDialogOpen: false,
      setResetDialogOpen: (resetDialogOpen) => set({ resetDialogOpen }),

      // Access editor (always starts closed)
      accessEditorOpen: false,
      accessEditorSeedSlug: null,

      setSidebarOpen: (sidebarOpen) => set({ sidebarOpen }),
      toggleSidebar: () => set((s) => ({ sidebarOpen: !s.sidebarOpen })),
      setActiveTab: (activeTab) => set({ activeTab }),
      setEdgeStyle: (edgeStyle) => set({ edgeStyle }),
      toggleEdgeStyle: () =>
        set((s) => ({
          edgeStyle: s.edgeStyle === 'default' ? 'straight' : 'default',
        })),
      toggleCompactCards: () =>
        set((s) => ({ compactCards: !s.compactCards })),
      toggleHeatMap: () =>
        set((s) => ({ showHeatMap: !s.showHeatMap })),
      toggleSpecMode: () =>
        set((s) => ({ showSpecMode: !s.showSpecMode })),

      setCommandPaletteOpen: (commandPaletteOpen) => set({ commandPaletteOpen }),
      toggleCommandPalette: () => set((s) => ({ commandPaletteOpen: !s.commandPaletteOpen })),

      openAccessEditor: (slug) =>
        set({ accessEditorOpen: true, accessEditorSeedSlug: slug ?? null }),
      closeAccessEditor: () => set({ accessEditorOpen: false, accessEditorSeedSlug: null }),

      tracesPrefs: { ...TRACES_PREFS_DEFAULTS },
      setTracesPrefs: (prefs) =>
        set((s) => ({ tracesPrefs: { ...s.tracesPrefs, ...prefs } })),

      logsPrefs: { ...LOGS_PREFS_DEFAULTS },
      setLogsPrefs: (prefs) =>
        set((s) => ({ logsPrefs: { ...s.logsPrefs, ...prefs } })),

      pinsPrefs: { ...PINS_PREFS_DEFAULTS },
      setPinsPrefs: (prefs) =>
        set((s) => ({ pinsPrefs: { ...s.pinsPrefs, ...prefs } })),

      toolsPrefs: { ...TOOLS_PREFS_DEFAULTS },
      setToolsPrefs: (prefs) =>
        set((s) => ({ toolsPrefs: { ...s.toolsPrefs, ...prefs } })),

      libraryPrefs: { ...LIBRARY_PREFS_DEFAULTS },
      setLibraryPrefs: (prefs) =>
        set((s) => ({ libraryPrefs: { ...s.libraryPrefs, ...prefs } })),

      // Detached window actions
      setLogsDetached: (logsDetached) => set({ logsDetached }),
      setSidebarDetached: (sidebarDetached) => set({ sidebarDetached }),
      setEditorDetached: (editorDetached) => set({ editorDetached }),
      setRegistryDetached: (registryDetached) => set({ registryDetached }),
      setMetricsDetached: (metricsDetached) => set({ metricsDetached }),
      setTracesDetached: (tracesDetached) => set({ tracesDetached }),
    }),
    {
      name: 'gridctl-ui-storage',
      // v1 flips the compactCards default to true. v0 payloads persisted the
      // field for every user regardless of an explicit choice, so honoring
      // them would pin every existing install to the old expanded default —
      // drop the stale value once; toggles after the upgrade persist again.
      version: 1,
      migrate: (persisted, version) => {
        if (version === 0 && persisted && typeof persisted === 'object') {
          delete (persisted as Record<string, unknown>).compactCards;
        }
        return persisted;
      },
      partialize: (state) => ({
        edgeStyle: state.edgeStyle,
        compactCards: state.compactCards,
        compactMode: state.compactMode,
        editorPrefs: state.editorPrefs,
        themeMode: state.themeMode,
        tracesPrefs: state.tracesPrefs,
        logsPrefs: state.logsPrefs,
        pinsPrefs: state.pinsPrefs,
        toolsPrefs: state.toolsPrefs,
        libraryPrefs: state.libraryPrefs,
      }),
      merge: (persisted, current) => {
        const p = persisted as Partial<UIState> | undefined;
        return {
          ...current,
          ...(p ?? {}),
          // Re-normalize to guarantee every workspace key is present even if a
          // user upgrades from a build that only persisted a subset.
          compactMode: normalizeCompactMode((p as { compactMode?: unknown })?.compactMode),
          // Guard against a stale/invalid persisted theme (e.g. upgrade from a
          // build without this field) so boot never lands on undefined.
          themeMode: isThemeMode(p?.themeMode) ? p.themeMode : current.themeMode,
          tracesPrefs: normalizeTracesPrefs((p as { tracesPrefs?: unknown })?.tracesPrefs),
          logsPrefs: normalizeLogsPrefs((p as { logsPrefs?: unknown })?.logsPrefs),
          pinsPrefs: normalizePinsPrefs((p as { pinsPrefs?: unknown })?.pinsPrefs),
          toolsPrefs: normalizeToolsPrefs((p as { toolsPrefs?: unknown })?.toolsPrefs),
          libraryPrefs: normalizeLibraryPrefs((p as { libraryPrefs?: unknown })?.libraryPrefs),
        };
      },
    }
  )
);
