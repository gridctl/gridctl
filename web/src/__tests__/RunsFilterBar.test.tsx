import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor, act, within } from '@testing-library/react';
import '@testing-library/jest-dom';
import { MemoryRouter, useLocation } from 'react-router';
import { RunsView } from '../components/runs/RunsView';
import { emptyFilters, useRunsStore } from '../stores/useRunsStore';
import type { RunRecord, RunStatusResponse } from '../lib/api';

vi.mock('../lib/api', async () => {
  const actual = await vi.importActual<typeof import('../lib/api')>('../lib/api');
  return {
    ...actual,
    fetchRuns: vi.fn(),
    fetchRunsStatus: vi.fn(),
    updateStackRuns: vi.fn(),
    wipeRuns: vi.fn(),
    exportRuns: vi.fn(),
  };
});

import { exportRuns, fetchRuns, fetchRunsStatus } from '../lib/api';

const record: RunRecord = {
  schemaVersion: 1,
  recorderInstanceId: 'r',
  sequence: 1,
  attemptId: 'a1',
  startedAt: '2026-01-01T00:00:00Z',
  returnedAt: '2026-01-01T00:00:01Z',
  durationMs: 12,
  requestedName: 'github__create_issue',
  resolvedServer: 'github',
  resolvedTool: 'create_issue',
  disposition: 'completed',
  stage: 'downstream',
  reason: 'ok',
};

const status: RunStatusResponse = {
  enabled: true,
  effective: true,
  writer_health: 'ok',
  queue_depth: 0,
  queue_capacity: 1024,
  drops: {},
  failures: {},
  historical_loss: 'unknown',
  wipe_epoch: 0,
  recordingNote: 'Records are saved after dispatch attempts return.',
  privacyNote: 'Metadata only.',
  retentionNote: 'Seven days and 100 MiB.',
  inventory: { stack: 'demo', signal: 'runs', path: '/tmp', sizeBytes: 10, fileCount: 1 },
};

const page = (over: Partial<Awaited<ReturnType<typeof fetchRuns>>> = {}) => ({
  records: [record],
  warnings: [],
  partial: false,
  wipeEpoch: 0,
  ...over,
});

function LocationProbe() {
  const location = useLocation();
  return <span data-testid="location-search">{location.search}</span>;
}

function renderAt(entry = '/traces?view=runs') {
  return render(
    <MemoryRouter initialEntries={[entry]}>
      <LocationProbe />
      <RunsView servers={['github', 'zapier']} />
    </MemoryRouter>,
  );
}

const search = () => screen.getByTestId('location-search').textContent ?? '';
const lastCall = () => vi.mocked(fetchRuns).mock.calls.at(-1)?.[0];

async function settled(calls = 1) {
  await waitFor(() => expect(fetchRuns).toHaveBeenCalledTimes(calls));
  await waitFor(() => expect(useRunsStore.getState().isLoading).toBe(false));
}

beforeEach(() => {
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
  useRunsStore.setState({
    records: [],
    warnings: [],
    partial: false,
    status: null,
    isLoading: false,
    hasLoaded: false,
    loadedFilters: null,
    lastQuery: null,
    error: null,
    selectedId: null,
    filters: emptyFilters(),
    nextCursor: null,
    statusError: null,
    mutationError: null,
    loadSeq: 0,
  });
  vi.mocked(fetchRuns).mockReset();
  vi.mocked(fetchRuns).mockResolvedValue(page());
  vi.mocked(fetchRunsStatus).mockResolvedValue(status);
  vi.mocked(exportRuns).mockReset();
  vi.mocked(exportRuns).mockResolvedValue(new Blob(['[]']));
});

afterEach(() => {
  vi.useRealTimers();
});

describe('RunsFilterBar', () => {
  it('shows five filter controls and keeps the ID filters behind More filters', async () => {
    renderAt();
    await settled();
    expect(screen.getByRole('combobox', { name: 'Filter by server' })).toBeInTheDocument();
    expect(screen.getByRole('combobox', { name: 'Filter by disposition' })).toBeInTheDocument();
    expect(screen.getByRole('combobox', { name: 'Time range' })).toBeInTheDocument();
    expect(screen.getAllByRole('combobox')).toHaveLength(3);
    // Retention inputs are outside the filter bar.
    const textboxes = screen.getAllByRole('textbox').filter((el) => !/Retention/.test(el.getAttribute('aria-label') ?? ''));
    expect(textboxes.map((el) => el.getAttribute('aria-label'))).toEqual(['Filter by tool', 'Filter by client']);
    expect(screen.getByLabelText('Filter by tool')).toHaveAttribute('placeholder', expect.stringMatching(/exact name/));
    expect(screen.getByLabelText('Filter by client')).toHaveAttribute('placeholder', expect.stringMatching(/exact name/));
    expect(screen.queryByLabelText('Attempt ID')).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Clear filters' })).not.toBeInTheDocument();

    const more = screen.getByRole('button', { name: 'More filters' });
    expect(more).toHaveAttribute('aria-expanded', 'false');
    fireEvent.click(more);
    expect(more).toHaveAttribute('aria-expanded', 'true');
    const panel = screen.getByRole('dialog', { name: 'More filters' });
    for (const label of ['Requested name', 'Access label', 'Attempt ID', 'Parent ID', 'Root ID', 'Previous ID', 'Trace ID']) {
      expect(within(panel).getByLabelText(label)).toBeInTheDocument();
    }
    expect(within(panel).getByLabelText('Attempt ID')).toHaveAttribute('placeholder', 'exact ID');

    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByRole('dialog', { name: 'More filters' })).not.toBeInTheDocument();
    expect(document.activeElement).toBe(more);
    expect(screen.getAllByRole('combobox', { name: 'Time range' })[0].querySelectorAll('option')).toHaveLength(6);
  });

  it('debounces typing into one request and commits on Enter without waiting', async () => {
    renderAt();
    await settled();
    vi.useFakeTimers();
    const tool = screen.getByLabelText('Filter by tool');
    for (const v of ['c', 'cr', 'cre', 'crea', 'creat ']) {
      fireEvent.change(tool, { target: { value: v } });
      act(() => {
        vi.advanceTimersByTime(50);
      });
    }
    expect(fetchRuns).toHaveBeenCalledTimes(1);
    act(() => {
      vi.advanceTimersByTime(300);
    });
    vi.useRealTimers();
    await settled(2);
    expect(lastCall()).toMatchObject({ tool: 'creat' });
    expect(useRunsStore.getState().filters.tool).toBe('creat');

    fireEvent.change(tool, { target: { value: 'create_issue' } });
    fireEvent.keyDown(tool, { key: 'Enter' });
    expect(useRunsStore.getState().filters.tool).toBe('create_issue');
    await settled(3);
    expect(lastCall()).toMatchObject({ tool: 'create_issue' });
    // The cancelled timer must not commit again.
    await new Promise((r) => setTimeout(r, 350));
    expect(fetchRuns).toHaveBeenCalledTimes(3);
  });

  it('issues no request for an unchanged blur or Enter', async () => {
    renderAt('/traces?view=runs&tool=create_issue');
    await settled();
    const tool = screen.getByLabelText('Filter by tool');
    expect(tool).toHaveValue('create_issue');
    tool.focus();
    fireEvent.blur(tool);
    fireEvent.keyDown(tool, { key: 'Enter' });
    fireEvent.change(tool, { target: { value: 'create_issue ' } });
    fireEvent.keyDown(tool, { key: 'Enter' });
    await new Promise((r) => setTimeout(r, 350));
    expect(fetchRuns).toHaveBeenCalledTimes(1);
  });

  it('resolves Last hour on each fresh load and reuses it for Load older', async () => {
    vi.mocked(fetchRuns).mockResolvedValue(page({ nextCursor: { sequence: 1 } as never }));
    renderAt();
    await settled();
    fireEvent.change(screen.getByLabelText('Time range'), { target: { value: '1h' } });
    await settled(2);
    const first = lastCall()!;
    expect(first.until).toBeUndefined();
    expect(first.cursor).toBeUndefined();
    expect(Math.abs(Date.parse(first.since!) - (Date.now() - 3600_000))).toBeLessThan(5000);
    expect(search()).toBe('?view=runs&range=1h');

    await new Promise((r) => setTimeout(r, 5));
    fireEvent.click(screen.getByRole('button', { name: 'Load older' }));
    await settled(3);
    expect(lastCall()).toEqual({ since: first.since, cursor: JSON.stringify({ sequence: 1 }) });

    fireEvent.click(screen.getByRole('button', { name: 'Refresh run records' }));
    await settled(4);
    expect(lastCall()!.since).not.toBe(first.since);
    expect(Date.parse(lastCall()!.since!)).toBeGreaterThan(Date.parse(first.since!));
  });

  it('shows datetime-local inputs for a custom range and sends RFC3339', async () => {
    renderAt();
    await settled();
    expect(screen.queryByLabelText('From')).not.toBeInTheDocument();
    fireEvent.change(screen.getByLabelText('Time range'), { target: { value: 'custom' } });
    await settled(2);
    const from = screen.getByLabelText('From');
    const to = screen.getByLabelText('To');
    expect(from).toHaveAttribute('type', 'datetime-local');
    expect(to).toHaveAttribute('type', 'datetime-local');
    expect(screen.queryByPlaceholderText('RFC3339')).not.toBeInTheDocument();

    fireEvent.change(from, { target: { value: '2026-11-27T09:05' } });
    await settled(3);
    const since = lastCall()!.since!;
    expect(since).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/);
    expect(Date.parse(since)).toBe(new Date('2026-11-27T09:05').getTime());
    expect(lastCall()!.until).toBeUndefined();
    expect(from).toHaveValue('2026-11-27T09:05');

    fireEvent.change(to, { target: { value: '2026-11-26T09:05' } });
    await settled(4);
    expect(screen.getByText('From is after To')).toBeInTheDocument();
    expect(lastCall()!.until).toBeDefined();

    fireEvent.change(from, { target: { value: '' } });
    await settled(5);
    expect(lastCall()!.since).toBeUndefined();
    expect(screen.queryByText('From is after To')).not.toBeInTheDocument();

    fireEvent.change(screen.getByLabelText('Time range'), { target: { value: 'all' } });
    await settled(6);
    expect(screen.queryByLabelText('From')).not.toBeInTheDocument();
    expect(lastCall()!.until).toBeUndefined();
  });

  it('shows a chip and count for a hidden filter and removes it', async () => {
    renderAt();
    await settled();
    fireEvent.click(screen.getByRole('button', { name: 'More filters' }));
    const trace = screen.getByLabelText('Trace ID');
    fireEvent.change(trace, { target: { value: '4bf92f3577b34da6a3ce929d0e0e4736' } });
    fireEvent.keyDown(trace, { key: 'Enter' });
    await settled(2);
    expect(lastCall()).toMatchObject({ trace: '4bf92f3577b34da6a3ce929d0e0e4736' });
    expect(screen.getByRole('button', { name: 'More filters (1)' })).toBeInTheDocument();
    const remove = screen.getByRole('button', { name: 'Remove Trace ID filter' });
    expect(remove.parentElement).toHaveAttribute('title', 'Trace ID: 4bf92f3577b34da6a3ce929d0e0e4736');

    fireEvent.click(remove);
    await settled(3);
    expect(lastCall()!.trace).toBeUndefined();
    expect(screen.queryByRole('button', { name: /Remove Trace ID filter/ })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'More filters' })).toBeInTheDocument();
    // Visible filters get no chip.
    fireEvent.change(screen.getByLabelText('Filter by server'), { target: { value: 'github' } });
    await settled(4);
    expect(screen.queryByRole('button', { name: /^Remove/ })).not.toBeInTheDocument();
  });

  it('clears every filter with one reload and keeps the selection', async () => {
    renderAt(
      '/traces?view=runs&server=github&disposition=denied&tool=t&client=c&requested=r&access=a&attempt=x&parent=p&root=o&previous=v&trace=abc&range=custom&since=2026-01-01T00:00:00Z&until=2026-01-02T00:00:00Z&run=a1',
    );
    await settled();
    const tool = screen.getByLabelText('Filter by tool');
    expect(tool).toHaveValue('t');
    tool.focus();
    fireEvent.change(screen.getByLabelText('Filter by client'), { target: { value: 'pending draft' } });
    const clear = screen.getByRole('button', { name: 'Clear filters' });
    // A real pointer press blurs the focused input unless mousedown is prevented.
    if (fireEvent.mouseDown(clear)) tool.blur();
    fireEvent.click(clear);
    await settled(2);
    await new Promise((r) => setTimeout(r, 350));
    expect(fetchRuns).toHaveBeenCalledTimes(2);
    expect(lastCall()).toEqual({});
    expect(useRunsStore.getState().filters).toEqual(emptyFilters());
    expect(useRunsStore.getState().selectedId).toBe('a1');
    expect(tool).toHaveValue('');
    expect(screen.getByLabelText('Filter by client')).toHaveValue('');
    expect(screen.getByLabelText('Time range')).toHaveValue('all');
    expect(screen.queryByRole('button', { name: 'Clear filters' })).not.toBeInTheDocument();
    expect(search()).toBe('?view=runs&run=a1');
  });

  it('clears with one reload when the focused input blurs first', async () => {
    renderAt('/traces?view=runs&tool=t');
    await settled();
    const tool = screen.getByLabelText('Filter by tool');
    tool.focus();
    tool.blur();
    fireEvent.click(screen.getByRole('button', { name: 'Clear filters' }));
    await settled(2);
    await new Promise((r) => setTimeout(r, 350));
    expect(fetchRuns).toHaveBeenCalledTimes(2);
  });
});

describe('RunsView URL state', () => {
  it('mirrors committed filters and the selection into the URL', async () => {
    renderAt();
    await settled();
    expect(search()).toBe('?view=runs');
    fireEvent.change(screen.getByLabelText('Filter by server'), { target: { value: 'github' } });
    fireEvent.change(screen.getByLabelText('Filter by disposition'), { target: { value: 'denied' } });
    fireEvent.click(screen.getByRole('button', { name: 'More filters' }));
    const attempt = screen.getByLabelText('Attempt ID');
    fireEvent.change(attempt, { target: { value: 'draft-only' } });
    expect(search()).not.toContain('draft-only');
    fireEvent.keyDown(attempt, { key: 'Enter' });
    fireEvent.keyDown(document, { key: 'Escape' });
    await waitFor(() => expect(screen.getByText(/github › create_issue/)).toBeInTheDocument());
    fireEvent.click(screen.getByText(/github › create_issue/));
    await waitFor(() => expect(search()).toContain('run=a1'));
    const params = new URLSearchParams(search());
    expect(params.get('view')).toBe('runs');
    expect(params.get('server')).toBe('github');
    expect(params.get('disposition')).toBe('denied');
    expect(params.get('attempt')).toBe('draft-only');

    fireEvent.click(screen.getByRole('button', { name: 'Clear filters' }));
    act(() => {
      useRunsStore.getState().select(null);
    });
    await waitFor(() => expect(search()).toBe('?view=runs'));
  });

  it('restores a deep link with one request and opens the selected run', async () => {
    renderAt('/traces?view=runs&server=github&disposition=denied&range=24h&trace=abc&run=a1');
    await settled();
    expect(fetchRuns).toHaveBeenCalledTimes(1);
    const q = lastCall()!;
    expect(q).toMatchObject({ server: 'github', disposition: 'denied', trace: 'abc' });
    expect(Math.abs(Date.parse(q.since!) - (Date.now() - 24 * 3600_000))).toBeLessThan(5000);
    expect(q.until).toBeUndefined();
    expect(screen.getByLabelText('Filter by server')).toHaveValue('github');
    expect(screen.getByLabelText('Filter by disposition')).toHaveValue('denied');
    expect(screen.getByLabelText('Time range')).toHaveValue('24h');
    expect(screen.getByRole('button', { name: 'Remove Trace ID filter' })).toBeInTheDocument();
    expect(screen.getByLabelText('Run detail')).toBeInTheDocument();
    const params = new URLSearchParams(search());
    expect(params.get('range')).toBe('24h');
    expect(params.get('run')).toBe('a1');
  });

  it('issues exactly one request on a plain mount and on a filtered deep link', async () => {
    renderAt('/traces?view=runs');
    await settled();
    await new Promise((r) => setTimeout(r, 20));
    expect(fetchRuns).toHaveBeenCalledTimes(1);
  });

  it('issues exactly one request on a filtered deep link', async () => {
    useRunsStore.setState({ filters: { ...emptyFilters(), tool: 'leftover' } });
    renderAt('/traces?view=runs&server=github&disposition=denied');
    await settled();
    await new Promise((r) => setTimeout(r, 20));
    expect(fetchRuns).toHaveBeenCalledTimes(1);
    expect(lastCall()).toEqual({ server: 'github', disposition: 'denied' });
  });

  it('applies only the URL server filter over leftover store filters', async () => {
    useRunsStore.setState({
      filters: { ...emptyFilters(), tool: 'old', trace: 'old-trace', range: '7d', disposition: 'denied' },
    });
    renderAt('/traces?view=runs&server=github');
    await settled();
    expect(useRunsStore.getState().filters).toEqual({ ...emptyFilters(), server: 'github' });
    expect(lastCall()).toEqual({ server: 'github' });
    expect(search()).toBe('?view=runs&server=github');
  });

  it('keeps store filters when the URL has no filter key', async () => {
    useRunsStore.setState({ filters: { ...emptyFilters(), tool: 'kept' } });
    renderAt('/traces?view=runs');
    await settled();
    expect(lastCall()).toEqual({ tool: 'kept' });
    expect(search()).toBe('?view=runs&tool=kept');
  });

  it('ignores invalid since, range, and disposition values', async () => {
    renderAt('/traces?view=runs&since=garbage&range=nope&disposition=nope');
    await settled();
    expect(lastCall()).toEqual({});
    expect(useRunsStore.getState().filters).toEqual(emptyFilters());
  });

  it('shows a notice for a selected run that is not loaded and clears it', async () => {
    renderAt('/traces?view=runs&run=missing');
    await settled();
    const notice = await screen.findByText('The selected run is not in the current results.');
    expect(notice).toHaveAttribute('role', 'status');
    expect(search()).toContain('run=missing');
    fireEvent.click(within(notice).getByRole('button', { name: 'Clear selection' }));
    expect(screen.queryByText('The selected run is not in the current results.')).not.toBeInTheDocument();
    await waitFor(() => expect(search()).toBe('?view=runs'));
  });

  it('exports the query behind the rows on screen', async () => {
    const createObjectURL = vi.fn().mockReturnValue('blob:runs');
    const revokeObjectURL = vi.fn();
    vi.stubGlobal('URL', Object.assign(URL, { createObjectURL, revokeObjectURL }));
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined);
    try {
      renderAt('/traces?view=runs&range=1h&server=github');
      await settled();
      const shown = lastCall()!;

      vi.mocked(fetchRuns).mockReturnValue(new Promise(() => {}));
      fireEvent.change(screen.getByLabelText('Filter by disposition'), { target: { value: 'denied' } });
      await waitFor(() => expect(fetchRuns).toHaveBeenCalledTimes(2));

      fireEvent.click(screen.getByRole('button', { name: 'Export' }));
      await waitFor(() => expect(exportRuns).toHaveBeenCalledTimes(1));
      expect(exportRuns).toHaveBeenCalledWith(shown);
      expect(vi.mocked(exportRuns).mock.calls[0][0]).not.toHaveProperty('cursor');
    } finally {
      click.mockRestore();
      vi.unstubAllGlobals();
    }
  });

  it('exports the current filters when no fresh load has succeeded', async () => {
    vi.mocked(fetchRuns).mockRejectedValue(new Error('boom'));
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined);
    vi.stubGlobal('URL', Object.assign(URL, { createObjectURL: vi.fn(() => 'blob:x'), revokeObjectURL: vi.fn() }));
    try {
      renderAt('/traces?view=runs&tool=t');
      await settled();
      fireEvent.click(screen.getByRole('button', { name: 'Export' }));
      await waitFor(() => expect(exportRuns).toHaveBeenCalledWith({ tool: 't' }));
    } finally {
      click.mockRestore();
      vi.unstubAllGlobals();
    }
  });
});
