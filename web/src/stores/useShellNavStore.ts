import { create } from 'zustand';

interface ShellNavState {
  goToStack: () => void;
  register: (actions: { goToStack: () => void }) => void;
  clear: () => void;
}

const noop = () => {};

// Store actions (the needs-auth toast, the status bar auth chip) open the
// Stack inspector from any workspace, but a zustand store has no router.
// AppShell registers the guarded navigation on mount and clears it on
// unmount; until then, and in detached windows, goToStack is a no-op.
export const useShellNavStore = create<ShellNavState>((set) => ({
  goToStack: noop,
  register: (actions) => set({ goToStack: actions.goToStack }),
  clear: () => set({ goToStack: noop }),
}));
