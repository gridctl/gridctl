import { create } from 'zustand';

export interface CanvasViewportOptions {
  padding?: number;
  duration?: number;
}

export interface CanvasZoomOptions {
  duration?: number;
}

export interface CanvasActions {
  fitView: (options?: CanvasViewportOptions) => void;
  zoomIn: (options?: CanvasZoomOptions) => void;
  zoomOut: (options?: CanvasZoomOptions) => void;
}

const noop = () => {};

const idle: CanvasActions = {
  fitView: noop,
  zoomIn: noop,
  zoomOut: noop,
};

interface CanvasActionsState extends CanvasActions {
  register: (actions: CanvasActions) => void;
  clear: () => void;
}

// Shell shortcuts and the command palette call through here so AppShell does
// not import React Flow. Canvas registers the real functions on mount and
// clears them on unmount; until then every action is a no-op.
export const useCanvasActionsStore = create<CanvasActionsState>((set) => ({
  ...idle,
  register: (actions) => set({
    fitView: actions.fitView,
    zoomIn: actions.zoomIn,
    zoomOut: actions.zoomOut,
  }),
  clear: () => set({ ...idle }),
}));
