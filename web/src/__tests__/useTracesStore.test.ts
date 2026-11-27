import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useTracesStore, TRACES_PAGE_SIZE } from '../stores/useTracesStore';
import { useAuthStore } from '../stores/useAuthStore';
import type { TraceDetail, TraceSummary, TraceListResponse } from '../lib/api';

vi.mock('../lib/api', async (importActual) => {
  const actual = await importActual<typeof import('../lib/api')>();
  return {
    ...actual,
    fetchTraces: vi.fn(),
    fetchTraceDetail: vi.fn(),
  };
});

import { fetchTraces, fetchTraceDetail } from '../lib/api';

interface Deferred<T> {
  promise: Promise<T>;
  resolve: (value: T) => void;
  reject: (err: Error) => void;
}

function deferred<T>(): Deferred<T> {
  let resolve: (value: T) => void = () => {};
  let reject: (err: Error) => void = () => {};
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

function detailFor(traceId: string): TraceDetail {
  return { traceId, spans: [] };
}

function summary(traceId: string): TraceSummary {
  return {
    traceId,
    rootSpanId: 's1',
    operation: 'tools/call create_issue',
    tool: 'create_issue',
    client: 'claude-code',
    server: 'github',
    startTime: '2026-07-23T10:00:00Z',
    duration: 42,
    spanCount: 1,
    hasError: false,
    status: 'ok',
  };
}

function envelope(traces: TraceSummary[], over?: Partial<TraceListResponse>): TraceListResponse {
  return {
    traces,
    total: traces.length,
    tracingEnabled: true,
    bufferSize: traces.length,
    bufferCapacity: 1000,
    ...over,
  };
}

// Lets every pending microtask (awaited fetches and their continuations) run.
async function flush() {
  await new Promise((resolve) => setTimeout(resolve, 0));
}

beforeEach(() => {
  vi.mocked(fetchTraces).mockReset();
  vi.mocked(fetchTraceDetail).mockReset();
  useAuthStore.setState({ authRequired: false });
  useTracesStore.setState({
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
    filters: { segment: 'tool-calls', server: '', errorsOnly: false, minDuration: null, timeRange: 'all', search: '' },
    limit: TRACES_PAGE_SIZE,
    selectedTraceId: null,
    traceDetail: null,
    isLoadingDetail: false,
    detailError: null,
  });
});

describe('useTracesStore detail selection', () => {
  function deferPerId() {
    const pending = new Map<string, Deferred<TraceDetail>>();
    vi.mocked(fetchTraceDetail).mockImplementation((id: string) => {
      const d = deferred<TraceDetail>();
      pending.set(id, d);
      return d.promise;
    });
    return pending;
  }

  it('shows the second trace when detail fetches resolve in reverse order', async () => {
    const pending = deferPerId();
    useTracesStore.getState().selectTrace('A');
    useTracesStore.getState().selectTrace('B');

    pending.get('B')!.resolve(detailFor('B'));
    await flush();
    pending.get('A')!.resolve(detailFor('A'));
    await flush();

    const s = useTracesStore.getState();
    expect(s.selectedTraceId).toBe('B');
    expect(s.traceDetail?.traceId).toBe('B');
    expect(s.isLoadingDetail).toBe(false);
    expect(s.detailError).toBeNull();
  });

  it('ignores a stale rejection that lands after the current trace resolved', async () => {
    const pending = deferPerId();
    useTracesStore.getState().selectTrace('A');
    useTracesStore.getState().selectTrace('B');

    pending.get('B')!.resolve(detailFor('B'));
    await flush();
    pending.get('A')!.reject(new Error('trace not found'));
    await flush();

    const s = useTracesStore.getState();
    expect(s.detailError).toBeNull();
    expect(s.traceDetail?.traceId).toBe('B');
  });

  it('keeps the waterfall closed when a fetch resolves after closing', async () => {
    const pending = deferPerId();
    useTracesStore.getState().selectTrace('A');
    expect(useTracesStore.getState().isLoadingDetail).toBe(true);
    useTracesStore.getState().selectTrace(null);

    pending.get('A')!.resolve(detailFor('A'));
    await flush();

    const s = useTracesStore.getState();
    expect(s.traceDetail).toBeNull();
    expect(s.isLoadingDetail).toBe(false);
  });

  it('applies a re-request for the same selected trace', async () => {
    vi.mocked(fetchTraceDetail).mockResolvedValue(detailFor('A'));
    useTracesStore.setState({ selectedTraceId: 'A' });
    await useTracesStore.getState().loadTraceDetail('A');
    expect(useTracesStore.getState().traceDetail?.traceId).toBe('A');
  });
});

describe('useTracesStore list loading', () => {
  it('keeps the later call rows when list fetches resolve in reverse order', async () => {
    const first = deferred<TraceListResponse>();
    const second = deferred<TraceListResponse>();
    vi.mocked(fetchTraces).mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);

    const loadA = useTracesStore.getState().loadTraces();
    const loadB = useTracesStore.getState().loadTraces();
    second.resolve(envelope([summary('bbbb')]));
    await loadB;
    first.resolve(envelope([summary('aaaa')]));
    await loadA;

    const s = useTracesStore.getState();
    expect(s.traces.map((t) => t.traceId)).toEqual(['bbbb']);
    expect(s.isLoading).toBe(false);
  });

  it('does not let an early-returning call supersede the one in flight', async () => {
    const first = deferred<TraceListResponse>();
    vi.mocked(fetchTraces).mockReturnValueOnce(first.promise);

    const loadA = useTracesStore.getState().loadTraces();
    useAuthStore.setState({ authRequired: true });
    await useTracesStore.getState().loadTraces();
    useAuthStore.setState({ authRequired: false });

    first.resolve(envelope([summary('aaaa')]));
    await loadA;

    const s = useTracesStore.getState();
    expect(vi.mocked(fetchTraces)).toHaveBeenCalledTimes(1);
    expect(s.traces.map((t) => t.traceId)).toEqual(['aaaa']);
    expect(s.isLoading).toBe(false);
  });

  it('sends the page size as the initial limit', async () => {
    vi.mocked(fetchTraces).mockResolvedValue(envelope([]));
    await useTracesStore.getState().loadTraces();
    expect(vi.mocked(fetchTraces)).toHaveBeenLastCalledWith(expect.objectContaining({ limit: 100 }));
  });

  it('raises the limit by one page on Load more', async () => {
    vi.mocked(fetchTraces).mockResolvedValue(envelope([], { bufferCapacity: 1000 }));
    await useTracesStore.getState().loadTraces();
    await useTracesStore.getState().loadMore();
    expect(useTracesStore.getState().limit).toBe(200);
    expect(vi.mocked(fetchTraces)).toHaveBeenLastCalledWith(expect.objectContaining({ limit: 200 }));
  });

  it('clamps Load more to the buffer capacity', async () => {
    vi.mocked(fetchTraces).mockResolvedValue(envelope([], { bufferCapacity: 150 }));
    await useTracesStore.getState().loadTraces();
    await useTracesStore.getState().loadMore();
    expect(useTracesStore.getState().limit).toBe(150);
    expect(vi.mocked(fetchTraces)).toHaveBeenLastCalledWith(expect.objectContaining({ limit: 150 }));
  });

  it('resets the limit on a server-side filter change only', async () => {
    vi.mocked(fetchTraces).mockResolvedValue(envelope([], { bufferCapacity: 1000 }));
    await useTracesStore.getState().loadTraces();

    await useTracesStore.getState().loadMore();
    useTracesStore.getState().setFilters({ search: 'x', timeRange: '5m' });
    expect(useTracesStore.getState().limit).toBe(200);

    useTracesStore.getState().setFilters({ server: 'github' });
    expect(useTracesStore.getState().limit).toBe(100);

    await useTracesStore.getState().loadMore();
    useTracesStore.getState().setFilters({ errorsOnly: true });
    expect(useTracesStore.getState().limit).toBe(100);

    await useTracesStore.getState().loadMore();
    useTracesStore.getState().setFilters({ minDuration: 50 });
    expect(useTracesStore.getState().limit).toBe(100);

    await useTracesStore.getState().loadMore();
    // Setting the same server value is not a change.
    useTracesStore.getState().setFilters({ server: 'github' });
    expect(useTracesStore.getState().limit).toBe(200);
  });
});
