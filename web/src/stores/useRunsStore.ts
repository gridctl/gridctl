import { create } from 'zustand';
import {
  HTTPError,
  fetchRuns,
  fetchRunsStatus,
  type RunCursor,
  type RunListResponse,
  type RunRecord,
  type RunStatusResponse,
} from '../lib/api';
import { useAuthStore } from './useAuthStore';
import {
  emptyFilters,
  resolveRunsQuery,
  sameRunsFilters,
  type RunsFilters,
  type RunsQuery,
} from '../lib/runsFilters';

export type { RunsFilters, RunsQuery, RunsTimeRange } from '../lib/runsFilters';
export { emptyFilters };

interface RunsState {
  records: RunRecord[];
  warnings: RunListResponse['warnings'];
  partial: boolean;
  nextCursor: RunCursor | null;
  status: RunStatusResponse | null;
  isLoading: boolean;
  /** A list load has completed successfully for the current filters. */
  hasLoaded: boolean;
  /** Filters of the last successful non-append load. */
  loadedFilters: RunsFilters | null;
  error: string | null;
  /** HTTP status of the failed list load, when the server returned one. */
  errorStatus: number | null;
  /** The failed list load was a Load older request. */
  appendFailed: boolean;
  statusError: string | null;
  mutationError: string | null;
  selectedId: string | null;
  filters: RunsFilters;
  /**
   * Resolved parameters of the last fresh load that succeeded, so it always
   * describes the rows on screen. Load older and Export reuse it.
   */
  lastQuery: RunsQuery | null;
  loadSeq: number;
  setFilters: (filters: Partial<RunsFilters>) => void;
  replaceFilters: (filters: RunsFilters) => void;
  clearFilters: () => void;
  select: (id: string | null) => void;
  setMutationError: (error: string | null) => void;
  load: (opts?: { append?: boolean }) => Promise<void>;
  /**
   * Repeats the failed load. A failed Load older is repeated, except after a
   * 409: a wipe invalidated the cursor, so the first page is reloaded.
   */
  retry: () => Promise<void>;
}

export const useRunsStore = create<RunsState>()((set, get) => ({
  records: [],
  warnings: [],
  partial: false,
  nextCursor: null,
  status: null,
  isLoading: false,
  hasLoaded: false,
  loadedFilters: null,
  error: null,
  errorStatus: null,
  appendFailed: false,
  statusError: null,
  mutationError: null,
  selectedId: null,
  filters: emptyFilters(),
  lastQuery: null,
  loadSeq: 0,
  // Setters keep the filters identity when nothing changed: the load effect
  // keys on it, so a no-op commit must not reload.
  setFilters: (filters) => {
    const next = { ...get().filters, ...filters };
    if (!sameRunsFilters(next, get().filters)) set({ filters: next });
  },
  replaceFilters: (filters) => {
    const next = { ...emptyFilters(), ...filters };
    if (!sameRunsFilters(next, get().filters)) set({ filters: next });
  },
  clearFilters: () => {
    const next = emptyFilters();
    if (!sameRunsFilters(next, get().filters)) set({ filters: next });
  },
  select: (id) => set({ selectedId: id }),
  setMutationError: (error) => set({ mutationError: error }),
  load: async (opts) => {
    const seq = get().loadSeq + 1;
    const generation = useAuthStore.getState().generation;
    // The error stays until a load succeeds, so a retry while stale does not
    // briefly report the history as readable.
    set({ loadSeq: seq, isLoading: true });
    const append = Boolean(opts?.append);
    const { filters, nextCursor, lastQuery } = get();
    // A fresh load resolves a relative preset against now; Load older reuses
    // the resolved window so it does not move between pages.
    const query = append ? (lastQuery ?? resolveRunsQuery(filters, Date.now())) : resolveRunsQuery(filters, Date.now());
    let list: RunListResponse | null = null;
    let listError: string | null = null;
    let listErrorStatus: number | null = null;
    try {
      list = await fetchRuns({ ...query, cursor: append && nextCursor ? JSON.stringify(nextCursor) : undefined });
    } catch (err) {
      listError = err instanceof Error ? err.message : 'Failed to load run history';
      listErrorStatus = err instanceof HTTPError ? err.status : null;
    }
    let status = get().status;
    let statusError: string | null = null;
    try {
      status = await fetchRunsStatus();
    } catch (err) {
      statusError = err instanceof Error ? err.message : 'Failed to load recorder status';
    }
    if (get().loadSeq !== seq) return;
    if (useAuthStore.getState().generation !== generation) return;
    if (listError) {
      // Rows loaded under the same filters stay as last-known data; rows
      // from other filters are never shown under the new ones.
      const loaded = get().loadedFilters;
      const keep = append || (loaded !== null && sameRunsFilters(loaded, filters));
      set({
        isLoading: false,
        error: listError,
        errorStatus: listErrorStatus,
        appendFailed: append,
        statusError,
        status: status ?? get().status,
        ...(keep
          ? {}
          : { records: [], warnings: [], partial: false, nextCursor: null, hasLoaded: false, loadedFilters: null, lastQuery: null }),
      });
      return;
    }
    const incoming = list?.records ?? [];
    set({
      records: append ? [...get().records, ...incoming] : incoming,
      warnings: list?.warnings ?? [],
      partial: Boolean(list?.partial),
      nextCursor: list?.nextCursor ?? null,
      status: status ?? get().status,
      statusError,
      isLoading: false,
      hasLoaded: true,
      ...(append ? {} : { loadedFilters: { ...filters }, lastQuery: query }),
      error: null,
      errorStatus: null,
      appendFailed: false,
    });
  },
  retry: () => {
    const { appendFailed, errorStatus, nextCursor, load } = get();
    return load({ append: appendFailed && errorStatus !== 409 && nextCursor !== null });
  },
}));
