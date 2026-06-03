import { create } from 'zustand';
import type { StateCreator } from 'zustand';
import { persist } from 'zustand/middleware';
import { WORKSPACES, type Workspace } from '../types/workspace';

type SidebarTab = 'details' | 'tools' | 'logs';
type BottomPanelTab = 'logs' | 'metrics' | 'spec' | 'traces' | 'pins';
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
  activeWorkspace: 'topology',
  setActiveWorkspace: (activeWorkspace) => set({ activeWorkspace }),
});

// Compact Mode is workspace-scoped — both workspaces default to roomier
// layouts; flip per-workspace via toggleCompactMode.
export type CompactModeMap = Record<Workspace, boolean>;

export const COMPACT_MODE_DEFAULTS: CompactModeMap = {
  topology: false,
  library: false,
  vault: false,
  tools: false,
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

  // Skill editor view preferences
  editorPrefs: EditorPrefs;
  setEditorPrefs: (prefs: Partial<EditorPrefs>) => void;

  // Compact card mode
  compactCards: boolean;

  // Token heat overlay on graph nodes
  showHeatMap: boolean;

  // Drift detection overlay on canvas
  showDriftOverlay: boolean;

  // Canvas spec mode — shows ghost nodes for undeployed spec items
  showSpecMode: boolean;

  // Canvas wiring mode — drag connections between nodes
  showWiringMode: boolean;

  // Secret heatmap overlay
  showSecretHeatmap: boolean;

  // Latency heat overlay on canvas edges
  showLatencyHeat: boolean;

  // Bottom panel state
  bottomPanelOpen: boolean;
  bottomPanelTab: BottomPanelTab;

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
  toggleDriftOverlay: () => void;
  toggleSpecMode: () => void;
  toggleWiringMode: () => void;
  toggleSecretHeatmap: () => void;
  toggleLatencyHeat: () => void;

  // Bottom panel actions
  setBottomPanelOpen: (open: boolean) => void;
  toggleBottomPanel: () => void;
  setBottomPanelTab: (tab: BottomPanelTab) => void;

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

  // Vault panel visibility (lifted from Header local state for command palette access)
  showVault: boolean;
  setShowVault: (show: boolean) => void;
  toggleVault: () => void;

  // Per-client access editor: opened from the Topology inspector ("Edit Scope")
  // seeded to a specific client. Transient (not persisted).
  accessEditorOpen: boolean;
  accessEditorSeedSlug: string | null;
  openAccessEditor: (slug?: string | null) => void;
  closeAccessEditor: () => void;
}

export const useUIStore = create<UIState>()(
  persist(
    (set, get, store) => ({
      ...createWorkspaceSlice(set, get, store),
      ...createCompactModeSlice(set, get, store),
      sidebarOpen: false,
      activeTab: 'details',
      edgeStyle: 'default', // Bezier curves

      // Skill editor view preferences
      editorPrefs: { ...EDITOR_PREFS_DEFAULTS },
      setEditorPrefs: (prefs) =>
        set((s) => ({ editorPrefs: { ...s.editorPrefs, ...prefs } })),

      // Compact cards default
      compactCards: false,

      // Token heat overlay default
      showHeatMap: false,

      // Drift overlay default
      showDriftOverlay: false,

      // Spec mode default
      showSpecMode: false,

      // Wiring mode default
      showWiringMode: false,

      // Secret heatmap default
      showSecretHeatmap: false,

      // Latency heat overlay default
      showLatencyHeat: false,

      // Bottom panel defaults
      bottomPanelOpen: false,
      bottomPanelTab: 'logs',

      // Detached window defaults
      logsDetached: false,
      sidebarDetached: false,
      editorDetached: false,
      registryDetached: false,
      metricsDetached: false,
      tracesDetached: false,

      // Command palette (always starts closed)
      commandPaletteOpen: false,

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
      toggleDriftOverlay: () =>
        set((s) => ({ showDriftOverlay: !s.showDriftOverlay })),
      toggleSpecMode: () =>
        set((s) => ({ showSpecMode: !s.showSpecMode })),
      toggleWiringMode: () =>
        set((s) => ({ showWiringMode: !s.showWiringMode })),
      toggleSecretHeatmap: () =>
        set((s) => ({ showSecretHeatmap: !s.showSecretHeatmap })),
      toggleLatencyHeat: () =>
        set((s) => ({ showLatencyHeat: !s.showLatencyHeat })),

      // Bottom panel actions
      setBottomPanelOpen: (bottomPanelOpen) => set({ bottomPanelOpen }),
      toggleBottomPanel: () => set((s) => ({ bottomPanelOpen: !s.bottomPanelOpen })),
      setBottomPanelTab: (bottomPanelTab) => set({ bottomPanelTab, bottomPanelOpen: true }),

      setCommandPaletteOpen: (commandPaletteOpen) => set({ commandPaletteOpen }),
      toggleCommandPalette: () => set((s) => ({ commandPaletteOpen: !s.commandPaletteOpen })),

      showVault: false,
      setShowVault: (showVault) => set({ showVault }),
      toggleVault: () => set((s) => ({ showVault: !s.showVault })),

      openAccessEditor: (slug) =>
        set({ accessEditorOpen: true, accessEditorSeedSlug: slug ?? null }),
      closeAccessEditor: () => set({ accessEditorOpen: false, accessEditorSeedSlug: null }),

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
      partialize: (state) => ({
        edgeStyle: state.edgeStyle,
        compactCards: state.compactCards,
        compactMode: state.compactMode,
        editorPrefs: state.editorPrefs,
      }),
      merge: (persisted, current) => ({
        ...current,
        ...(persisted as Partial<UIState>),
        // Re-normalize to guarantee every workspace key is present even if a
        // user upgrades from a build that only persisted a subset.
        compactMode: normalizeCompactMode((persisted as { compactMode?: unknown })?.compactMode),
      }),
    }
  )
);
