import { create } from 'zustand';

// Leave guard for the Stack inspector's tool whitelist editor.
//
// The draft lives in useToolsEditor (component state), but the actions that
// move the inspector away from a server (canvas clicks, Escape, the close
// button, palette commands) sit outside the editor's tree and must decide
// synchronously. ToolsEditor mirrors the server that owns an unsaved draft
// into dirtyServer and clears it on unmount; requestInspectorLeave stashes
// one continuation while it is set, which ToolsEditor turns into a confirm.
// Each window has its own instance, so the guard never crosses windows.
interface InspectorToolsGuardState {
  dirtyServer: string | null;
  pendingLeave: (() => void) | null;
  setDirtyServer: (name: string | null) => void;
  requestLeave: (proceed: () => void) => void;
  // Clears the dirty flag before running the continuation so a nested
  // requestInspectorLeave inside it passes through instead of stashing again.
  confirmLeave: () => void;
  cancelLeave: () => void;
}

export const useInspectorToolsGuardStore = create<InspectorToolsGuardState>((set, get) => ({
  dirtyServer: null,
  pendingLeave: null,
  setDirtyServer: (dirtyServer) => set({ dirtyServer }),
  requestLeave: (proceed) => set({ pendingLeave: proceed }),
  confirmLeave: () => {
    const proceed = get().pendingLeave;
    set({ dirtyServer: null, pendingLeave: null });
    proceed?.();
  },
  cancelLeave: () => set({ pendingLeave: null }),
}));

// requestInspectorLeave runs `proceed` now when no inspector draft is dirty,
// or when the action selects the draft's own server (`targetServer`).
// Otherwise it stashes `proceed` until the user discards or keeps editing.
export function requestInspectorLeave(
  proceed: () => void,
  opts?: { targetServer?: string },
): void {
  const { dirtyServer, requestLeave } = useInspectorToolsGuardStore.getState();
  if (dirtyServer === null || opts?.targetServer === dirtyServer) {
    proceed();
    return;
  }
  requestLeave(proceed);
}
