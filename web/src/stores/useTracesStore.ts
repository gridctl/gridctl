import { create } from 'zustand';
import { fetchTraces, fetchTraceDetail } from '../lib/api';
import type { TraceSummary, TraceDetail } from '../lib/api';
import { useAuthStore } from './useAuthStore';

export type { TraceSummary, TraceDetail };

/** Which traces the list shows: real tool calls, or everything in the buffer. */
export type TracesSegment = 'tool-calls' | 'all';

/** Client-side time window over the ring buffer. */
export type TracesTimeRange = '5m' | '15m' | '1h' | 'all';

export const TRACES_TIME_RANGES: { value: TracesTimeRange; label: string }[] = [
  { value: '5m', label: '5m' },
  { value: '15m', label: '15m' },
  { value: '1h', label: '1h' },
  { value: 'all', label: 'Buffer' },
];

export const TRACES_TIME_RANGE_MS: Record<TracesTimeRange, number | null> = {
  '5m': 5 * 60 * 1000,
  '15m': 15 * 60 * 1000,
  '1h': 60 * 60 * 1000,
  all: null,
};

interface TracesFilters {
  segment: TracesSegment;
  server: string;
  errorsOnly: boolean;
  minDuration: number | null;
  timeRange: TracesTimeRange;
  search: string;
}

/** Server-side filters, the ones a reload sends to the API. */
type LoadedTraceFilters = Pick<TracesFilters, 'server' | 'errorsOnly' | 'minDuration'>;

// Only the newest list request may write results; a superseded one is dropped.
let tracesLoadSeq = 0;

function sameServerFilters(a: LoadedTraceFilters, b: LoadedTraceFilters): boolean {
  return a.server === b.server && a.errorsOnly === b.errorsOnly && a.minDuration === b.minDuration;
}

interface TracesState {
  traces: TraceSummary[];
  total: number;
  tracingEnabled: boolean;
  bufferSize: number;
  bufferCapacity: number;
  isLoading: boolean;
  isPaused: boolean;
  error: string | null;
  /** Wall-clock ms of the last completed load; anchors the time-range filter. */
  lastLoadedAt: number;
  /** Server-side filters of the last successful load; null before one. */
  loadedFilters: LoadedTraceFilters | null;
  filters: TracesFilters;

  // Selected trace detail
  selectedTraceId: string | null;
  traceDetail: TraceDetail | null;
  isLoadingDetail: boolean;
  detailError: string | null;

  // Actions
  setFilters: (filters: Partial<TracesFilters>) => void;
  setPaused: (paused: boolean) => void;
  selectTrace: (traceId: string | null) => void;
  loadTraces: () => Promise<void>;
  loadTraceDetail: (traceId: string) => Promise<void>;
}

export const useTracesStore = create<TracesState>()((set, get) => ({
  traces: [],
  total: 0,
  tracingEnabled: true,
  bufferSize: 0,
  bufferCapacity: 0,
  isLoading: false,
  isPaused: false,
  error: null,
  lastLoadedAt: 0,
  loadedFilters: null,
  filters: {
    segment: 'tool-calls',
    server: '',
    errorsOnly: false,
    minDuration: null,
    timeRange: 'all',
    search: '',
  },
  selectedTraceId: null,
  traceDetail: null,
  isLoadingDetail: false,
  detailError: null,

  setFilters: (filters) =>
    set((s) => ({ filters: { ...s.filters, ...filters } })),

  setPaused: (paused) => set({ isPaused: paused }),

  selectTrace: (traceId) => {
    set({ selectedTraceId: traceId, traceDetail: null, detailError: null });
    if (traceId) {
      get().loadTraceDetail(traceId);
    }
  },

  loadTraces: async () => {
    const generation = useAuthStore.getState().generation;
    if (useAuthStore.getState().authRequired) return;
    const seq = ++tracesLoadSeq;
    const { filters } = get();
    const requested: LoadedTraceFilters = {
      server: filters.server,
      errorsOnly: filters.errorsOnly,
      minDuration: filters.minDuration,
    };
    // The error stays until a load succeeds, so a failing poll does not
    // flicker between the banner and a clean view.
    set({ isLoading: true });
    try {
      const result = await fetchTraces({
        server: filters.server || undefined,
        errors: filters.errorsOnly || undefined,
        minDuration: filters.minDuration ?? undefined,
        limit: 100,
      });
      if (seq !== tracesLoadSeq) return;
      if (useAuthStore.getState().generation !== generation) return;
      set({
        traces: result.traces,
        total: result.total,
        tracingEnabled: result.tracingEnabled,
        bufferSize: result.bufferSize,
        bufferCapacity: result.bufferCapacity,
        isLoading: false,
        error: null,
        loadedFilters: requested,
        lastLoadedAt: Date.now(),
      });
    } catch (err) {
      if (seq !== tracesLoadSeq) return;
      if (useAuthStore.getState().generation !== generation) return;
      // Rows from other server-side filters are never shown as last-known
      // data for the requested ones.
      const loaded = get().loadedFilters;
      const keep = loaded !== null && sameServerFilters(loaded, requested);
      set({
        error: err instanceof Error ? err.message : 'Failed to fetch traces',
        isLoading: false,
        ...(keep ? {} : { traces: [], total: 0, loadedFilters: null }),
      });
    }
  },

  loadTraceDetail: async (traceId) => {
    const generation = useAuthStore.getState().generation;
    if (useAuthStore.getState().authRequired) return;
    set({ isLoadingDetail: true, detailError: null });
    try {
      const detail = await fetchTraceDetail(traceId);
      if (useAuthStore.getState().generation !== generation) return;
      set({ traceDetail: detail, isLoadingDetail: false });
    } catch (err) {
      if (useAuthStore.getState().generation !== generation) return;
      set({
        detailError: err instanceof Error ? err.message : 'Failed to fetch trace detail',
        isLoadingDetail: false,
      });
    }
  },
}));

/**
 * A trace counts as a tool call when routing resolved a tool, or when the
 * root kept its pre-routing name (a call that errored before routing).
 */
export function isToolCallTrace(tr: TraceSummary): boolean {
  return tr.tool !== '' || tr.operation === 'tools/call' || tr.operation === 'mcp.tools.call' || tr.operation.startsWith('tools/call ');
}
