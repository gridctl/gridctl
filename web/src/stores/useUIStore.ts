import { create } from 'zustand';
import { persist } from 'zustand/middleware';

type SidebarTab = 'details' | 'tools' | 'logs';
type EdgeStyle = 'default' | 'straight'; // 'default' = Bezier curves

interface UIState {
  sidebarOpen: boolean;
  activeTab: SidebarTab;
  edgeStyle: EdgeStyle;

  // Bottom panel state
  bottomPanelOpen: boolean;

  // Detached window state
  logsDetached: boolean;
  sidebarDetached: boolean;
  editorDetached: boolean;
  registryDetached: boolean;

  // Actions
  setSidebarOpen: (open: boolean) => void;
  toggleSidebar: () => void;
  setActiveTab: (tab: SidebarTab) => void;
  setEdgeStyle: (style: EdgeStyle) => void;
  toggleEdgeStyle: () => void;

  // Bottom panel actions
  setBottomPanelOpen: (open: boolean) => void;
  toggleBottomPanel: () => void;

  // Detached window actions
  setLogsDetached: (detached: boolean) => void;
  setSidebarDetached: (detached: boolean) => void;
  setEditorDetached: (detached: boolean) => void;
  setRegistryDetached: (detached: boolean) => void;
}

export const useUIStore = create<UIState>()(
  persist(
    (set) => ({
      sidebarOpen: false,
      activeTab: 'details',
      edgeStyle: 'default', // Bezier curves

      // Bottom panel defaults
      bottomPanelOpen: false,

      // Detached window defaults
      logsDetached: false,
      sidebarDetached: false,
      editorDetached: false,
      registryDetached: false,

      setSidebarOpen: (sidebarOpen) => set({ sidebarOpen }),
      toggleSidebar: () => set((s) => ({ sidebarOpen: !s.sidebarOpen })),
      setActiveTab: (activeTab) => set({ activeTab }),
      setEdgeStyle: (edgeStyle) => set({ edgeStyle }),
      toggleEdgeStyle: () =>
        set((s) => ({
          edgeStyle: s.edgeStyle === 'default' ? 'straight' : 'default',
        })),

      // Bottom panel actions
      setBottomPanelOpen: (bottomPanelOpen) => set({ bottomPanelOpen }),
      toggleBottomPanel: () => set((s) => ({ bottomPanelOpen: !s.bottomPanelOpen })),

      // Detached window actions
      setLogsDetached: (logsDetached) => set({ logsDetached }),
      setSidebarDetached: (sidebarDetached) => set({ sidebarDetached }),
      setEditorDetached: (editorDetached) => set({ editorDetached }),
      setRegistryDetached: (registryDetached) => set({ registryDetached }),
    }),
    {
      name: 'gridctl-ui-storage',
      partialize: (state) => ({
        // Only persist edge style preference
        edgeStyle: state.edgeStyle,
        // Don't persist panel open state - start fresh each session
      }),
    }
  )
);
