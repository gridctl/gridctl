import { create } from 'zustand';
import { fetchGlobalContext, type ContextDoc } from '../lib/api';
import { describeError, isCancellation } from '../lib/describeError';

interface ContextStoreState {
  /** null means "not loaded yet"; the Context segment shows a loading state. */
  doc: ContextDoc | null;
  loading: boolean;
  error: string | null;
  /** Replace the document (mutation endpoints return the refreshed doc). */
  setDoc: (doc: ContextDoc) => void;
  /** Re-fetch canonical content + per-client state. */
  refresh: () => Promise<void>;
}

/**
 * Global-context state shared by the Library's Context segment and any
 * status badge that wants the needs_sync signal. Fetched on demand when
 * the segment mounts, not in the global polling cycle: context state only
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
      if (isCancellation(err)) set({ loading: false });
      else set({ loading: false, error: describeError(err, 'Load global context').text });
    }
  },
}));
