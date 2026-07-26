import { create } from 'zustand';

// Navigate-away guard state for the Tools workspace's whitelist editor.
//
// The editor's dirty flag lives in useToolsEditor (component state), but the
// WorkspaceSwitcher sits outside the workspace tree and must read it
// synchronously inside a NavLink click handler. ToolsWorkspace mirrors the
// flag here; the switcher cancels the NavLink and stashes the target in
// exitNavTarget, which ToolsWorkspace turns into a discard-with-confirm.
// Same contract as useAccessLensStore.exitNavTarget — BrowserRouter has no
// useBlocker, so the guard is manual.
interface ToolsDirtyState {
  dirty: boolean;
  exitNavTarget: string | null;
  setDirty: (dirty: boolean) => void;
  requestExitNav: (path: string) => void;
  clearExitNav: () => void;
}

export const useToolsDirtyStore = create<ToolsDirtyState>((set) => ({
  dirty: false,
  exitNavTarget: null,
  setDirty: (dirty) => set({ dirty }),
  requestExitNav: (path) => set({ exitNavTarget: path }),
  clearExitNav: () => set({ exitNavTarget: null }),
}));
