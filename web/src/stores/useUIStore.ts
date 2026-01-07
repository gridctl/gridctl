import { create } from 'zustand';

type SidebarTab = 'details' | 'tools' | 'logs';
type EdgeStyle = 'default' | 'straight'; // 'default' = Bezier curves

interface UIState {
  sidebarOpen: boolean;
  activeTab: SidebarTab;
  edgeStyle: EdgeStyle;

  // Actions
  setSidebarOpen: (open: boolean) => void;
  toggleSidebar: () => void;
  setActiveTab: (tab: SidebarTab) => void;
  setEdgeStyle: (style: EdgeStyle) => void;
  toggleEdgeStyle: () => void;
}

export const useUIStore = create<UIState>((set) => ({
  sidebarOpen: false,
  activeTab: 'details',
  edgeStyle: 'default', // Bezier curves (organic noodles)

  setSidebarOpen: (sidebarOpen) => set({ sidebarOpen }),
  toggleSidebar: () => set((s) => ({ sidebarOpen: !s.sidebarOpen })),
  setActiveTab: (activeTab) => set({ activeTab }),
  setEdgeStyle: (edgeStyle) => set({ edgeStyle }),
  toggleEdgeStyle: () => set((s) => ({
    edgeStyle: s.edgeStyle === 'default' ? 'straight' : 'default',
  })),
}));
