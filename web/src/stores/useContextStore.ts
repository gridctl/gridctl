import { create } from 'zustand';
import { fetchGlobalContext, type ContextDoc } from '../lib/api';

interface ContextStoreState {
  /** null means "not loaded yet"; the dialog shows a loading state. */
  doc: ContextDoc | null;
  loading: boolean;
  error: string | null;
  /** Replace the document (mutation endpoints return the refreshed doc). */
  setDoc: (doc: ContextDoc) => void;
  /** Re-fetch canonical content + per-client state. */
  refresh: () => Promise<void>;
}

/**
 * Global-context state shared by the Library's Global Context dialog and
 * any status badge that wants the needs_sync signal. Fetched on demand
 * (dialog open), not in the global polling cycle — context state only
 * changes through explicit user actions.
 */
export const useContextStore = create<ContextStoreState>()((set) => ({
  doc: null,
  loading: false,
  error: null,
  setDoc: (doc) => set({ doc, error: null }),
  refresh: async () => {
    set({ loading: true });
    try {
      const doc = await fetchGlobalContext();
      set({ doc, loading: false, error: null });
    } catch (err) {
      set({ loading: false, error: err instanceof Error ? err.message : 'Failed to load context' });
    }
  },
}));
