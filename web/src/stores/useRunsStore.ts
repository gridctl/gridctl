import { create } from 'zustand';
import {
  fetchRuns,
  fetchRunsStatus,
  type RunCursor,
  type RunListResponse,
  type RunRecord,
  type RunStatusResponse,
} from '../lib/api';
import { useAuthStore } from './useAuthStore';

export interface RunsFilters {
  server: string;
  tool: string;
  disposition: string;
  requested: string;
  client: string;
  access: string;
  attempt: string;
  parent: string;
  root: string;
  previous: string;
  trace: string;
  since: string;
  until: string;
}

const emptyFilters = (): RunsFilters => ({
  server: '',
  tool: '',
  disposition: '',
  requested: '',
  client: '',
  access: '',
  attempt: '',
  parent: '',
  root: '',
  previous: '',
  trace: '',
  since: '',
  until: '',
});

interface RunsState {
  records: RunRecord[];
  warnings: RunListResponse['warnings'];
  partial: boolean;
  nextCursor: RunCursor | null;
  status: RunStatusResponse | null;
  isLoading: boolean;
  error: string | null;
  statusError: string | null;
  mutationError: string | null;
  selectedId: string | null;
  filters: RunsFilters;
  loadSeq: number;
  setFilters: (filters: Partial<RunsFilters>) => void;
  select: (id: string | null) => void;
  setMutationError: (error: string | null) => void;
  load: (opts?: { append?: boolean }) => Promise<void>;
}

function filterParams(filters: RunsFilters, cursor?: RunCursor | null) {
  return {
    server: filters.server || undefined,
    tool: filters.tool || undefined,
    disposition: filters.disposition || undefined,
    requested: filters.requested || undefined,
    client: filters.client || undefined,
    access: filters.access || undefined,
    attempt: filters.attempt || undefined,
    parent: filters.parent || undefined,
    root: filters.root || undefined,
    previous: filters.previous || undefined,
    trace: filters.trace || undefined,
    since: filters.since || undefined,
    until: filters.until || undefined,
    cursor: cursor ? JSON.stringify(cursor) : undefined,
  };
}

export const useRunsStore = create<RunsState>()((set, get) => ({
  records: [],
  warnings: [],
  partial: false,
  nextCursor: null,
  status: null,
  isLoading: false,
  error: null,
  statusError: null,
  mutationError: null,
  selectedId: null,
  filters: emptyFilters(),
  loadSeq: 0,
  setFilters: (filters) => set({ filters: { ...get().filters, ...filters } }),
  select: (id) => set({ selectedId: id }),
  setMutationError: (error) => set({ mutationError: error }),
  load: async (opts) => {
    const seq = get().loadSeq + 1;
    const generation = useAuthStore.getState().generation;
    set({ loadSeq: seq, isLoading: true, error: null });
    const append = Boolean(opts?.append);
    const { filters, nextCursor } = get();
    let list: RunListResponse | null = null;
    let listError: string | null = null;
    try {
      list = await fetchRuns(filterParams(filters, append ? nextCursor : null));
    } catch (err) {
      listError = err instanceof Error ? err.message : 'Failed to load run history';
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
      set({
        isLoading: false,
        error: listError,
        statusError,
        status: status ?? get().status,
        records: append ? get().records : [],
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
      error: null,
    });
  },
}));
