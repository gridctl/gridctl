import { create } from 'zustand';
import { persist } from 'zustand/middleware';

type SidebarTab = 'details' | 'tools' | 'logs';
type BottomPanelTab = 'logs' | 'metrics' | 'spec';
type EdgeStyle = 'default' | 'straight'; // 'default' = Bezier curves

interface UIState {
  sidebarOpen: boolean;
  activeTab: SidebarTab;
  edgeStyle: EdgeStyle;

  // Compact card mode
  compactCards: boolean;

  // Token heat overlay on graph nodes
  showHeatMap: boolean;

  // Drift detection overlay on canvas
  showDriftOverlay: boolean;

  // Bottom panel state
  bottomPanelOpen: boolean;
  bottomPanelTab: BottomPanelTab;

  // Detached window state
  logsDetached: boolean;
  sidebarDetached: boolean;
  editorDetached: boolean;
  registryDetached: boolean;
  workflowDetached: boolean;
  metricsDetached: boolean;

  // Actions
  setSidebarOpen: (open: boolean) => void;
  toggleSidebar: () => void;
  setActiveTab: (tab: SidebarTab) => void;
  setEdgeStyle: (style: EdgeStyle) => void;
  toggleEdgeStyle: () => void;
  toggleCompactCards: () => void;
  toggleHeatMap: () => void;
  toggleDriftOverlay: () => void;

  // Bottom panel actions
  setBottomPanelOpen: (open: boolean) => void;
  toggleBottomPanel: () => void;
  setBottomPanelTab: (tab: BottomPanelTab) => void;

  // Detached window actions
  setLogsDetached: (detached: boolean) => void;
  setSidebarDetached: (detached: boolean) => void;
  setEditorDetached: (detached: boolean) => void;
  setRegistryDetached: (detached: boolean) => void;
  setWorkflowDetached: (detached: boolean) => void;
  setMetricsDetached: (detached: boolean) => void;
}

export const useUIStore = create<UIState>()(
  persist(
    (set) => ({
      sidebarOpen: false,
      activeTab: 'details',
      edgeStyle: 'default', // Bezier curves

      // Compact cards default
      compactCards: false,

      // Token heat overlay default
      showHeatMap: false,

      // Drift overlay default
      showDriftOverlay: false,

      // Bottom panel defaults
      bottomPanelOpen: false,
      bottomPanelTab: 'logs',

      // Detached window defaults
      logsDetached: false,
      sidebarDetached: false,
      editorDetached: false,
      registryDetached: false,
      workflowDetached: false,
      metricsDetached: false,

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

      // Bottom panel actions
      setBottomPanelOpen: (bottomPanelOpen) => set({ bottomPanelOpen }),
      toggleBottomPanel: () => set((s) => ({ bottomPanelOpen: !s.bottomPanelOpen })),
      setBottomPanelTab: (bottomPanelTab) => set({ bottomPanelTab, bottomPanelOpen: true }),

      // Detached window actions
      setLogsDetached: (logsDetached) => set({ logsDetached }),
      setSidebarDetached: (sidebarDetached) => set({ sidebarDetached }),
      setEditorDetached: (editorDetached) => set({ editorDetached }),
      setRegistryDetached: (registryDetached) => set({ registryDetached }),
      setWorkflowDetached: (workflowDetached) => set({ workflowDetached }),
      setMetricsDetached: (metricsDetached) => set({ metricsDetached }),
    }),
    {
      name: 'gridctl-ui-storage',
      partialize: (state) => ({
        edgeStyle: state.edgeStyle,
        compactCards: state.compactCards,
      }),
    }
  )
);
