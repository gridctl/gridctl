import { create } from 'zustand';
import { fetchRuns, fetchRunsStatus, type RunListResponse, type RunRecord, type RunStatusResponse } from '../lib/api';
import { useAuthStore } from './useAuthStore';

interface RunsFilters {
  server: string;
  disposition: string;
}

interface RunsState {
  records: RunRecord[];
  warnings: RunListResponse['warnings'];
  partial: boolean;
  status: RunStatusResponse | null;
  isLoading: boolean;
  error: string | null;
  selectedId: string | null;
  filters: RunsFilters;
  setFilters: (filters: Partial<RunsFilters>) => void;
  select: (id: string | null) => void;
  load: () => Promise<void>;
}

export const useRunsStore = create<RunsState>()((set, get) => ({
  records: [],
  warnings: [],
  partial: false,
  status: null,
  isLoading: false,
  error: null,
  selectedId: null,
  filters: { server: '', disposition: '' },
  setFilters: (filters) => set({ filters: { ...get().filters, ...filters } }),
  select: (id) => set({ selectedId: id }),
  load: async () => {
    const generation = useAuthStore.getState().generation;
    set({ isLoading: true, error: null });
    try {
      const { filters } = get();
      const [list, status] = await Promise.all([
        fetchRuns({
          server: filters.server || undefined,
          disposition: filters.disposition || undefined,
        }),
        fetchRunsStatus(),
      ]);
      if (useAuthStore.getState().generation !== generation) return;
      set({
        records: list.records ?? [],
        warnings: list.warnings ?? [],
        partial: list.partial,
        status,
        isLoading: false,
        error: null,
      });
    } catch (err) {
      if (useAuthStore.getState().generation !== generation) return;
      set({
        isLoading: false,
        error: err instanceof Error ? err.message : 'Failed to load run history',
        records: [],
      });
    }
  },
}));
