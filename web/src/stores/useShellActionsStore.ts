import { create } from 'zustand';

interface ShellActionsState {
  reloadConfig: (() => void) | null;
  register: (actions: { reloadConfig: () => void }) => void;
  clear: () => void;
}

// The command palette calls header actions through here, so it runs the same
// path as the header control (including the spec diff modal) without the
// header depending on the command registry. Header registers on mount and
// clears on unmount; until then each slot is null.
export const useShellActionsStore = create<ShellActionsState>((set) => ({
  reloadConfig: null,
  register: (actions) => set({ reloadConfig: actions.reloadConfig }),
  clear: () => set({ reloadConfig: null }),
}));
