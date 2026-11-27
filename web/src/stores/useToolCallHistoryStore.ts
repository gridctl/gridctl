import { create } from 'zustand';
import { useAuthStore } from './useAuthStore';
import { canonicalJSON } from '../lib/toolSchemaForm';

// Argument sets may contain secrets, so this history is memory-only: no
// persist middleware, never written to storage, the URL, or logs.

export const TOOL_CALL_HISTORY_LIMIT = 5;

export interface ToolCallHistoryEntry {
  args: Record<string, unknown>;
  at: number;
}

interface ToolCallHistoryState {
  byTool: Record<string, ToolCallHistoryEntry[]>;
  push: (tool: string, args: Record<string, unknown>) => void;
  list: (tool: string) => ToolCallHistoryEntry[];
  clear: () => void;
  /** Whether the Try it disclosure is expanded; shared across tools for the page session. */
  tryItOpen: boolean;
  setTryItOpen: (open: boolean) => void;
}

const EMPTY: ToolCallHistoryEntry[] = [];

export const useToolCallHistoryStore = create<ToolCallHistoryState>()((set, get) => ({
  byTool: {},
  push: (tool, args) =>
    set((state) => {
      const key = canonicalJSON(args);
      const rest = (state.byTool[tool] ?? EMPTY).filter((e) => canonicalJSON(e.args) !== key);
      const next = [{ args, at: Date.now() }, ...rest].slice(0, TOOL_CALL_HISTORY_LIMIT);
      return { byTool: { ...state.byTool, [tool]: next } };
    }),
  list: (tool) => get().byTool[tool] ?? EMPTY,
  clear: () => set({ byTool: {} }),
  tryItOpen: false,
  setTryItOpen: (tryItOpen) => set({ tryItOpen }),
}));

useAuthStore.subscribe((next, prev) => {
  if (next.generation !== prev.generation) useToolCallHistoryStore.getState().clear();
});
