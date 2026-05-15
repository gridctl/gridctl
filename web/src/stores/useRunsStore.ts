import { create } from 'zustand';
import type { AgentRunSummary } from '../lib/agent-runs';
import { fetchAgentRuns, fetchAgentRun } from '../lib/agent-runs';
import type { RunEvent } from '../lib/agent-api';

/**
 * RunsFilters mirrors the server-side query parameters. Each field
 * binds to a URL search param so the grid is shareable / bookmarkable
 * — the workspace component reads these from `?status=…&skill=…` on
 * mount and writes them back as the user changes filters.
 *
 * `since` accepts the relative shapes the backend recognises (`5m`,
 * `1h`, `24h`, `7d`) plus the empty string ("all time"). RFC 3339
 * timestamps work too but the bar UI only emits the relative forms.
 */
export interface RunsFilters {
  status: string;
  skill: string;
  since: string;
  parent: string;
}

export const RUNS_DEFAULT_FILTERS: RunsFilters = {
  status: '',
  skill: '',
  since: '24h',
  parent: '',
};

const PAGE_SIZE = 100;

interface RunsState {
  /**
   * Ordered list of run summaries. The map keyed by `run_id` lives
   * inside the array — order matters (newest first) so we don't keep
   * a secondary index.
   */
  runs: AgentRunSummary[];
  /**
   * Forward cursor for the next page. Empty string = no more pages.
   */
  nextCursor: string;
  /** True while the initial page or a filter change is loading. */
  loading: boolean;
  /** True while a "load more" page is loading. */
  loadingMore: boolean;
  /** Latest fetch error, surfaced as an inline retry affordance. */
  error: string | null;

  /** Current filter state. Mirrors URL search params on the workspace. */
  filters: RunsFilters;

  /** Selected run in the grid (right-rail inspector). */
  selectedRunID: string | null;

  /**
   * Per-run `seq` watermark. Used to dedupe events arriving on the
   * global SSE bus against the per-run replay endpoint — clients see
   * the same event on both streams and must take the first.
   */
  lastSeenSeq: Record<string, number>;

  /**
   * Set of currently in-flight runs (started but not yet completed).
   * Powers the BottomPanel runs-tab badge.
   */
  inFlightRuns: Set<string>;

  /** Status of the global event stream. */
  streamStatus: 'idle' | 'connecting' | 'open' | 'restarted' | 'error';

  // Actions ─────────────────────────────────────────────────────────────────

  setFilters: (partial: Partial<RunsFilters>) => void;
  resetFilters: () => void;
  setSelectedRun: (runID: string | null) => void;

  /** Fresh fetch — replaces the current page. */
  loadRuns: () => Promise<void>;
  /** Append the next page using the stored cursor. */
  loadMore: () => Promise<void>;

  /** Apply a single event observed on the global SSE stream. */
  applyRunEvent: (ev: RunEvent) => void;
  /** The server signalled a gap — drop watermarks and refetch. */
  handleStreamRestart: () => void;
  setStreamStatus: (status: RunsState['streamStatus']) => void;
}

function mergeRuns(
  existing: AgentRunSummary[],
  incoming: AgentRunSummary[],
): AgentRunSummary[] {
  const seen = new Map<string, AgentRunSummary>();
  for (const r of existing) seen.set(r.run_id, r);
  for (const r of incoming) seen.set(r.run_id, r);
  // Preserve newest-first ordering: sort by started_at desc, falling
  // back to run_id lexicographic (run IDs embed the start timestamp).
  return [...seen.values()].sort((a, b) => {
    const at = a.started_at ?? '';
    const bt = b.started_at ?? '';
    if (at && bt && at !== bt) return at > bt ? -1 : 1;
    return a.run_id > b.run_id ? -1 : 1;
  });
}

function upsertRunInOrder(
  runs: AgentRunSummary[],
  next: AgentRunSummary,
): AgentRunSummary[] {
  const idx = runs.findIndex((r) => r.run_id === next.run_id);
  if (idx === -1) {
    return mergeRuns(runs, [next]);
  }
  const updated = runs.slice();
  updated[idx] = { ...updated[idx], ...next };
  return updated;
}

interface StartedPayload {
  skill?: string;
  parent_run_id?: string;
  trace_id?: string;
  flavor?: string;
}
interface CompletedPayload {
  status?: string;
  error?: string;
}
interface ApprovalPayload {
  approval_id?: string;
}

function summaryFromEvent(
  prior: AgentRunSummary | undefined,
  ev: RunEvent,
): AgentRunSummary {
  const base: AgentRunSummary = prior ?? {
    run_id: ev.run_id,
    status: 'running',
    event_count: 0,
  };
  const next: AgentRunSummary = {
    ...base,
    event_count: Math.max(base.event_count, ev.seq),
  };
  const payload = (ev.payload ?? {}) as Record<string, unknown>;
  switch (ev.type) {
    case 'run_started': {
      const p = payload as unknown as StartedPayload;
      next.skill = p.skill ?? next.skill;
      next.flavor = p.flavor ?? next.flavor;
      next.parent_run_id = p.parent_run_id ?? next.parent_run_id;
      next.trace_id = p.trace_id ?? next.trace_id;
      next.started_at = ev.time;
      next.status = 'running';
      break;
    }
    case 'run_completed': {
      const p = payload as unknown as CompletedPayload;
      next.status = p.status ?? 'ok';
      next.completed_at = ev.time;
      if (p.error) next.error = p.error;
      next.pending_approval = '';
      break;
    }
    case 'approval_request': {
      const p = payload as unknown as ApprovalPayload;
      next.pending_approval = p.approval_id;
      next.status = 'awaiting_approval';
      break;
    }
    case 'approval_response': {
      next.pending_approval = '';
      if (next.status === 'awaiting_approval') {
        next.status = 'running';
      }
      break;
    }
    case 'error': {
      // Mid-run errors don't necessarily terminate — leave status
      // alone; the matching run_completed (status=error) is the
      // authoritative terminal signal.
      break;
    }
  }
  return next;
}

export const useRunsStore = create<RunsState>((set, get) => ({
  runs: [],
  nextCursor: '',
  loading: false,
  loadingMore: false,
  error: null,
  filters: { ...RUNS_DEFAULT_FILTERS },
  selectedRunID: null,
  lastSeenSeq: {},
  inFlightRuns: new Set<string>(),
  streamStatus: 'idle',

  setFilters: (partial) =>
    set((s) => ({ filters: { ...s.filters, ...partial } })),
  resetFilters: () => set({ filters: { ...RUNS_DEFAULT_FILTERS } }),
  setSelectedRun: (runID) => set({ selectedRunID: runID }),

  loadRuns: async () => {
    const { filters } = get();
    set({ loading: true, error: null });
    try {
      const { runs, next_cursor } = await fetchAgentRuns({
        limit: PAGE_SIZE,
        status: filters.status,
        skill: filters.skill,
        since: filters.since,
        parent: filters.parent,
      });
      // Rebuild the in-flight set from the freshly fetched page; we
      // don't trust the prior state across a refetch since filters
      // may have excluded an in-flight run we already had.
      const inFlight = new Set<string>();
      for (const r of runs) {
        if (r.status === 'running' || r.status === 'started') {
          inFlight.add(r.run_id);
        }
      }
      set({
        runs,
        nextCursor: next_cursor ?? '',
        loading: false,
        inFlightRuns: inFlight,
      });
    } catch (err) {
      set({
        loading: false,
        error: err instanceof Error ? err.message : String(err),
      });
    }
  },

  loadMore: async () => {
    const { filters, nextCursor, runs } = get();
    if (!nextCursor || get().loadingMore) return;
    set({ loadingMore: true, error: null });
    try {
      const page = await fetchAgentRuns({
        limit: PAGE_SIZE,
        cursor: nextCursor,
        status: filters.status,
        skill: filters.skill,
        since: filters.since,
        parent: filters.parent,
      });
      set({
        runs: mergeRuns(runs, page.runs),
        nextCursor: page.next_cursor ?? '',
        loadingMore: false,
      });
    } catch (err) {
      set({
        loadingMore: false,
        error: err instanceof Error ? err.message : String(err),
      });
    }
  },

  applyRunEvent: (ev) => {
    const state = get();
    const lastSeen = state.lastSeenSeq[ev.run_id] ?? 0;
    // Dedup: per-run seq is monotonic so any seq we've already seen
    // is a replay. The /runs grid sees events twice — once from the
    // global bus, once when an inspector opens the per-run endpoint.
    if (ev.seq <= lastSeen) return;

    const prior = state.runs.find((r) => r.run_id === ev.run_id);
    // run_started for a run we don't have a summary for becomes a
    // synthetic insertion — important for runs launched while the
    // user is sitting on /runs with no manual refresh.
    if (!prior && ev.type !== 'run_started') {
      // Without the head event we can't render a useful row yet; the
      // next fetch will pull it in.
      set((s) => ({ lastSeenSeq: { ...s.lastSeenSeq, [ev.run_id]: ev.seq } }));
      return;
    }

    const nextSummary = summaryFromEvent(prior, ev);
    const inFlight = new Set(state.inFlightRuns);
    if (nextSummary.status === 'running' || nextSummary.status === 'started') {
      inFlight.add(nextSummary.run_id);
    } else {
      inFlight.delete(nextSummary.run_id);
    }

    set({
      runs: upsertRunInOrder(state.runs, nextSummary),
      lastSeenSeq: { ...state.lastSeenSeq, [ev.run_id]: ev.seq },
      inFlightRuns: inFlight,
    });
  },

  handleStreamRestart: () => {
    // The bus dropped events for us; the watermarks are unreliable
    // until we refetch. Clear them and reload the active page —
    // run_completed for completed runs will land via the refetch
    // rather than the (potentially missed) bus event.
    set({ lastSeenSeq: {}, streamStatus: 'restarted' });
    void get().loadRuns();
  },

  setStreamStatus: (streamStatus) => set({ streamStatus }),
}));

/**
 * fetchRunDetail wraps fetchAgentRun for callers (the inspector and
 * the detail page) that want a single helper to call. Re-exported here
 * so workspace code imports from one module rather than reaching into
 * the API client directly.
 */
export { fetchAgentRun as fetchRunDetail };
