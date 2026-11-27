import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router';
import { TracesWorkspace } from '../components/workspaces/TracesWorkspace';
import { useStackStore } from '../stores/useStackStore';
import { useTracesStore } from '../stores/useTracesStore';
import { emptyFilters, useRunsStore } from '../stores/useRunsStore';
import { useUIStore } from '../stores/useUIStore';
import type { MCPServerStatus } from '../types';

vi.mock('../hooks/useWindowManager', () => ({
  useWindowManager: () => ({
    openDetachedWindow: vi.fn(),
    focusDetachedWindow: vi.fn(),
    closeDetachedWindow: vi.fn(),
    broadcastStateUpdate: vi.fn(),
    broadcastSelectionChange: vi.fn(),
  }),
}));

vi.mock('../lib/api', async () => {
  const actual = await vi.importActual<typeof import('../lib/api')>('../lib/api');
  return {
    ...actual,
    fetchTraces: vi.fn(),
    fetchTraceDetail: vi.fn(),
    fetchRuns: vi.fn(),
    fetchRunsStatus: vi.fn(),
  };
});

import { fetchRuns, fetchRunsStatus, fetchTraceDetail, fetchTraces } from '../lib/api';

function LocationProbe() {
  const location = useLocation();
  return <span data-testid="location-search">{location.search}</span>;
}

function renderAt(entry: string) {
  return render(
    <MemoryRouter initialEntries={[entry]}>
      <LocationProbe />
      <Routes>
        <Route path="/traces" element={<TracesWorkspace />} />
      </Routes>
    </MemoryRouter>,
  );
}

const search = () => screen.getByTestId('location-search').textContent ?? '';

beforeEach(() => {
  localStorage.clear();
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: (query: string) => ({
      matches: false,
      media: query,
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
      addListener: () => undefined,
      removeListener: () => undefined,
      dispatchEvent: () => false,
    }),
  });
  vi.mocked(fetchTraces).mockResolvedValue({ traces: [], total: 0, tracingEnabled: true, bufferSize: 0, bufferCapacity: 1000 });
  vi.mocked(fetchTraceDetail).mockReturnValue(new Promise(() => {}));
  vi.mocked(fetchRuns).mockReset();
  vi.mocked(fetchRuns).mockResolvedValue({ records: [], warnings: [], partial: false, wipeEpoch: 0 });
  vi.mocked(fetchRunsStatus).mockResolvedValue({
    enabled: true,
    effective: true,
    writer_health: 'ok',
    queue_depth: 0,
    queue_capacity: 1024,
    drops: {},
    failures: {},
    historical_loss: 'unknown',
    wipe_epoch: 0,
    recordingNote: '',
    privacyNote: '',
    retentionNote: '',
    inventory: { stack: 'demo', signal: 'runs', path: '/tmp', sizeBytes: 0, fileCount: 0 },
  });
  useStackStore.setState({
    mcpServers: [{ name: 'github', transport: 'stdio', initialized: true, tools: [], healthy: true } as unknown as MCPServerStatus],
  });
  useTracesStore.setState({
    traces: [],
    loadedFilters: null,
    error: null,
    filters: { segment: 'tool-calls', server: '', errorsOnly: false, minDuration: null, timeRange: 'all', search: '' },
    selectedTraceId: null,
    traceDetail: null,
  });
  useRunsStore.setState({
    records: [],
    hasLoaded: false,
    loadedFilters: null,
    lastQuery: null,
    error: null,
    selectedId: null,
    filters: emptyFilters(),
    nextCursor: null,
    loadSeq: 0,
  });
});

describe('TracesWorkspace view-aware URL sync', () => {
  it('keeps Traces and Runs keys apart when switching tabs', async () => {
    // TracesView reapplies the saved server preference when it remounts.
    useUIStore.setState({ tracesPrefs: { ...useUIStore.getState().tracesPrefs, server: 'github' } });
    renderAt('/traces?server=github&trace=abc123def4567890');
    await waitFor(() => expect(search()).toContain('trace=abc123def4567890'));
    expect(useTracesStore.getState().filters.server).toBe('github');

    fireEvent.click(screen.getByRole('tab', { name: 'Runs' }));
    await waitFor(() => expect(fetchRuns).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(search()).toBe('?view=runs'));
    expect(useRunsStore.getState().filters).toEqual(emptyFilters());
    expect(vi.mocked(fetchRuns).mock.calls[0][0]).not.toHaveProperty('server');

    // A Runs filter never reaches the Traces store.
    fireEvent.change(screen.getByLabelText('Filter by server'), { target: { value: 'github' } });
    fireEvent.change(screen.getByLabelText('Filter by disposition'), { target: { value: 'denied' } });
    await waitFor(() => expect(search()).toContain('disposition=denied'));

    fireEvent.click(screen.getByRole('tab', { name: 'Traces' }));
    await waitFor(() => expect(search()).toContain('trace=abc123def4567890'));
    const params = new URLSearchParams(search());
    expect(params.get('view')).toBeNull();
    expect(params.get('disposition')).toBeNull();
    expect(params.get('server')).toBe('github');
    expect(useTracesStore.getState().filters.server).toBe('github');
    expect(useTracesStore.getState().selectedTraceId).toBe('abc123def4567890');
  });

  it('does not set the Runs server filter from a Traces server filter, or the reverse', async () => {
    useTracesStore.setState({
      filters: { segment: 'tool-calls', server: 'zapier', errorsOnly: false, minDuration: null, timeRange: 'all', search: '' },
    });
    renderAt('/traces?view=runs&server=github');
    await waitFor(() => expect(fetchRuns).toHaveBeenCalledTimes(1));
    expect(useRunsStore.getState().filters.server).toBe('github');
    expect(useTracesStore.getState().filters.server).toBe('zapier');
  });

  it('does not select a trace or change Traces filters from a Runs trace filter', async () => {
    const selectTrace = vi.spyOn(useTracesStore.getState(), 'selectTrace');
    const before = useTracesStore.getState().filters;
    renderAt('/traces?view=runs&trace=abc');
    await waitFor(() => expect(fetchRuns).toHaveBeenCalledTimes(1));
    expect(vi.mocked(fetchRuns).mock.calls[0][0]).toMatchObject({ trace: 'abc' });
    expect(selectTrace).not.toHaveBeenCalled();
    expect(useTracesStore.getState().selectedTraceId).toBeNull();
    expect(useTracesStore.getState().filters).toBe(before);
    expect(new URLSearchParams(search()).get('trace')).toBe('abc');
    selectTrace.mockRestore();
  });
});
