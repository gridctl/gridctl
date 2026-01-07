import { create } from 'zustand';

type SidebarTab = 'details' | 'tools' | 'logs';
type EdgeStyle = 'default' | 'straight'; // 'default' = Bezier curves

interface UIState {
  sidebarOpen: boolean;
  activeTab: SidebarTab;
  edgeStyle: EdgeStyle;

  // Bottom panel state
  bottomPanelOpen: boolean;
  bottomPanelHeight: number;

  // Actions
  setSidebarOpen: (open: boolean) => void;
  toggleSidebar: () => void;
  setActiveTab: (tab: SidebarTab) => void;
  setEdgeStyle: (style: EdgeStyle) => void;
  toggleEdgeStyle: () => void;

  // Bottom panel actions
  setBottomPanelOpen: (open: boolean) => void;
  toggleBottomPanel: () => void;
  setBottomPanelHeight: (height: number) => void;
}

export const useUIStore = create<UIState>((set) => ({
  sidebarOpen: false,
  activeTab: 'details',
  edgeStyle: 'default', // Bezier curves

  // Bottom panel defaults
  bottomPanelOpen: false,
  bottomPanelHeight: 300,

  setSidebarOpen: (sidebarOpen) => set({ sidebarOpen }),
  toggleSidebar: () => set((s) => ({ sidebarOpen: !s.sidebarOpen })),
  setActiveTab: (activeTab) => set({ activeTab }),
  setEdgeStyle: (edgeStyle) => set({ edgeStyle }),
  toggleEdgeStyle: () => set((s) => ({
    edgeStyle: s.edgeStyle === 'default' ? 'straight' : 'default',
  })),

  // Bottom panel actions
  setBottomPanelOpen: (bottomPanelOpen) => set({ bottomPanelOpen }),
  toggleBottomPanel: () => set((s) => ({ bottomPanelOpen: !s.bottomPanelOpen })),
  setBottomPanelHeight: (bottomPanelHeight) => set({ bottomPanelHeight }),
}));
