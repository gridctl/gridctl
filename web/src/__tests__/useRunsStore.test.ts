import { describe, it, expect, beforeEach } from 'vitest';
import { useRunsStore, RUNS_DEFAULT_FILTERS } from '../stores/useRunsStore';
import type { RunEvent } from '../lib/agent-api';

// fresh resets every store field between tests — useRunsStore is a
// process-wide singleton so without this the "Set" instances leak.
function resetStore() {
  useRunsStore.setState({
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
  });
}

function ev(runID: string, seq: number, type: string, payload: Record<string, unknown> = {}, time = '2026-05-14T10:00:00Z'): RunEvent {
  return { run_id: runID, seq, type, time, payload };
}

describe('useRunsStore.applyRunEvent', () => {
  beforeEach(resetStore);

  it('inserts a new run on run_started and tracks it as in-flight', () => {
    useRunsStore.getState().applyRunEvent(
      ev('run_a', 1, 'run_started', { skill: 'audit', flavor: 'ts' }),
    );

    const state = useRunsStore.getState();
    expect(state.runs).toHaveLength(1);
    expect(state.runs[0].run_id).toBe('run_a');
    expect(state.runs[0].skill).toBe('audit');
    expect(state.runs[0].status).toBe('running');
    expect(state.inFlightRuns.has('run_a')).toBe(true);
    expect(state.lastSeenSeq['run_a']).toBe(1);
  });

  it('marks the run completed on run_completed and clears in-flight', () => {
    const s = useRunsStore.getState();
    s.applyRunEvent(ev('run_a', 1, 'run_started', { skill: 'audit' }));
    s.applyRunEvent(ev('run_a', 2, 'run_completed', { status: 'ok' }));

    const state = useRunsStore.getState();
    expect(state.runs[0].status).toBe('ok');
    expect(state.inFlightRuns.has('run_a')).toBe(false);
  });

  it('dedupes events by (run_id, seq) so replays do not double-count', () => {
    const s = useRunsStore.getState();
    s.applyRunEvent(ev('run_a', 1, 'run_started', { skill: 'audit' }));
    s.applyRunEvent(ev('run_a', 2, 'run_completed', { status: 'ok' }));

    // Replay the same seq=2 event — should be a no-op.
    s.applyRunEvent(ev('run_a', 2, 'run_completed', { status: 'error' }));

    // Status stays at "ok" because the duplicate was ignored.
    expect(useRunsStore.getState().runs[0].status).toBe('ok');
  });

  it('ignores out-of-order events whose seq is below the watermark', () => {
    const s = useRunsStore.getState();
    s.applyRunEvent(ev('run_a', 1, 'run_started', { skill: 'audit' }));
    s.applyRunEvent(ev('run_a', 3, 'run_completed', { status: 'ok' }));

    // A stale node_enter for seq=2 — should be dropped.
    s.applyRunEvent(ev('run_a', 2, 'node_enter', { node_id: 'n1' }));

    expect(useRunsStore.getState().lastSeenSeq['run_a']).toBe(3);
  });

  it('promotes status to awaiting_approval on approval_request', () => {
    const s = useRunsStore.getState();
    s.applyRunEvent(ev('run_a', 1, 'run_started', { skill: 'audit' }));
    s.applyRunEvent(ev('run_a', 2, 'approval_request', { approval_id: 'ap_1' }));

    const run = useRunsStore.getState().runs[0];
    expect(run.status).toBe('awaiting_approval');
    expect(run.pending_approval).toBe('ap_1');
  });

  it('clears watermarks on stream restart and surfaces the new status', () => {
    const s = useRunsStore.getState();
    s.applyRunEvent(ev('run_a', 1, 'run_started', { skill: 'audit' }));
    s.handleStreamRestart();

    const state = useRunsStore.getState();
    expect(state.lastSeenSeq).toEqual({});
    expect(state.streamStatus).toBe('restarted');
  });
});

describe('useRunsStore.setFilters', () => {
  beforeEach(resetStore);

  it('merges partial updates over the current filter state', () => {
    const s = useRunsStore.getState();
    s.setFilters({ status: 'error' });
    expect(useRunsStore.getState().filters).toEqual({
      ...RUNS_DEFAULT_FILTERS,
      status: 'error',
    });

    s.setFilters({ skill: 'audit' });
    expect(useRunsStore.getState().filters).toEqual({
      ...RUNS_DEFAULT_FILTERS,
      status: 'error',
      skill: 'audit',
    });
  });

  it('resetFilters restores the defaults', () => {
    const s = useRunsStore.getState();
    s.setFilters({ status: 'error', skill: 'audit', parent: 'run_x' });
    s.resetFilters();
    expect(useRunsStore.getState().filters).toEqual(RUNS_DEFAULT_FILTERS);
  });
});
