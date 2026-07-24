import { create } from 'zustand';
import { fetchTraces, fetchTraceDetail } from '../lib/api';
import type { TraceSummary, TraceDetail } from '../lib/api';

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
    const { filters } = get();
    set({ isLoading: true, error: null });
    try {
      const result = await fetchTraces({
        server: filters.server || undefined,
        errors: filters.errorsOnly || undefined,
        minDuration: filters.minDuration ?? undefined,
        limit: 100,
      });
      set({
        traces: result.traces,
        total: result.total,
        tracingEnabled: result.tracingEnabled,
        bufferSize: result.bufferSize,
        bufferCapacity: result.bufferCapacity,
        isLoading: false,
        lastLoadedAt: Date.now(),
      });
    } catch (err) {
      set({
        error: err instanceof Error ? err.message : 'Failed to fetch traces',
        isLoading: false,
      });
    }
  },

  loadTraceDetail: async (traceId) => {
    set({ isLoadingDetail: true, detailError: null });
    try {
      const detail = await fetchTraceDetail(traceId);
      set({ traceDetail: detail, isLoadingDetail: false });
    } catch (err) {
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
  return tr.tool !== '' || tr.operation === 'mcp.tools.call';
}
