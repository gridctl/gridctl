import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor, within, act } from '@testing-library/react';
import '@testing-library/jest-dom';
import { MemoryRouter } from 'react-router';
import { RunsView } from '../components/runs/RunsView';
import { emptyFilters, useRunsStore } from '../stores/useRunsStore';
import type { RunRecord, RunStatusResponse } from '../lib/api';

vi.mock('../components/ui/Toast', () => ({ showToast: vi.fn() }));

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

import { HTTPError, fetchRuns, fetchRunsStatus, updateStackRuns, wipeRuns } from '../lib/api';
import { showToast } from '../components/ui/Toast';

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

const status = (over: Partial<RunStatusResponse> = {}): RunStatusResponse => ({
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
  ...over,
});

describe('RunsView', () => {
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
      error: null,
      errorStatus: null,
      appendFailed: false,
      selectedId: null,
      filters: emptyFilters(),
      nextCursor: null,
      statusError: null,
      mutationError: null,
      loadSeq: 0,
    });
    vi.mocked(fetchRuns).mockResolvedValue({ records: [record], warnings: [], partial: false, wipeEpoch: 0 });
    vi.mocked(fetchRunsStatus).mockResolvedValue(status());
    vi.mocked(updateStackRuns).mockReset();
    vi.mocked(wipeRuns).mockReset();
    vi.mocked(showToast).mockReset();
  });

  it('renders records and detail without treating missing parent as active', async () => {
    vi.mocked(fetchRuns).mockResolvedValue({
      records: [{ ...record, parentAttemptId: 'missing-parent' }],
      warnings: [],
      partial: false,
      wipeEpoch: 0,
    });
    render(
      <MemoryRouter>
        <RunsView servers={['github']} />
      </MemoryRouter>,
    );
    await waitFor(() => expect(screen.getByText(/github › create_issue/)).toBeInTheDocument());
    fireEvent.click(screen.getByText(/github › create_issue/));
    expect(screen.getByText(/Parent record is unavailable, not still running/)).toBeInTheDocument();
  });

  it('does not render read errors as empty success', async () => {
    vi.mocked(fetchRuns).mockRejectedValue(new Error('boom'));
    render(
      <MemoryRouter>
        <RunsView servers={[]} />
      </MemoryRouter>,
    );
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('boom'));
    expect(screen.getByRole('alert')).toHaveTextContent('Run history unavailable');
    expect(screen.queryByText(/No matching run records/)).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    await waitFor(() => expect(fetchRuns).toHaveBeenCalledTimes(2));
  });

  it('shows a skeleton instead of an empty table while the first load is pending', async () => {
    let resolveRuns: (value: Awaited<ReturnType<typeof fetchRuns>>) => void = () => {};
    vi.mocked(fetchRuns).mockReturnValue(new Promise((resolve) => {
      resolveRuns = resolve;
    }));
    render(
      <MemoryRouter>
        <RunsView servers={[]} />
      </MemoryRouter>,
    );
    expect(await screen.findByTestId('runs-skeleton')).toBeInTheDocument();
    expect(screen.queryByRole('grid')).not.toBeInTheDocument();
    await act(async () => {
      resolveRuns({ records: [], warnings: [], partial: false, wipeEpoch: 0 });
    });
  });

  it('reveals privacy and retention before enable', async () => {
    vi.mocked(fetchRuns).mockResolvedValue({ records: [], warnings: [], partial: false, wipeEpoch: 0 });
    vi.mocked(fetchRunsStatus).mockResolvedValue(status({ enabled: false, effective: false, inventory: { stack: 'demo', signal: 'runs', path: '', sizeBytes: 0, fileCount: 0 } }));
    render(
      <MemoryRouter>
        <RunsView servers={[]} />
      </MemoryRouter>,
    );
    await waitFor(() => expect(screen.getByRole('button', { name: /enable recording/i })).toBeInTheDocument());
    fireEvent.click(screen.getByRole('button', { name: /enable recording/i }));
    expect(screen.getByText(/Metadata only/)).toBeInTheDocument();
    expect(screen.getByText(/Seven days and 100 MiB/)).toBeInTheDocument();
    const dialog = screen.getByRole('alertdialog', { name: /enable run recording/i });
    expect(dialog).toHaveTextContent(/Metadata only/);
    expect(dialog).toHaveTextContent(/Seven days and 100 MiB/);
    fireEvent.keyDown(document.body, { key: 'Escape' });
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(updateStackRuns).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: /enable recording/i }));
    fireEvent.click(within(screen.getByRole('alertdialog')).getByRole('button', { name: /^enable$/i }));
    await waitFor(() => expect(updateStackRuns).toHaveBeenCalledWith({ enabled: true }));
    await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument());
  });

  it('keeps the enable confirm open when the mutation fails', async () => {
    vi.mocked(fetchRuns).mockResolvedValue({ records: [], warnings: [], partial: false, wipeEpoch: 0 });
    vi.mocked(fetchRunsStatus).mockResolvedValue(status({ enabled: false, effective: false, inventory: { stack: 'demo', signal: 'runs', path: '', sizeBytes: 0, fileCount: 0 } }));
    vi.mocked(updateStackRuns).mockRejectedValue(new Error('recorder unavailable'));
    render(
      <MemoryRouter>
        <RunsView servers={[]} />
      </MemoryRouter>,
    );
    await waitFor(() => expect(screen.getByRole('button', { name: /enable recording/i })).toBeInTheDocument());
    fireEvent.click(screen.getByRole('button', { name: /enable recording/i }));
    fireEvent.click(within(screen.getByRole('alertdialog')).getByRole('button', { name: /^enable$/i }));
    const dialog = await screen.findByRole('alertdialog', { name: /enable run recording/i });
    expect(dialog).toHaveTextContent('recorder unavailable');
    expect(within(dialog).getByRole('alert')).toHaveTextContent('recorder unavailable');
    expect(screen.getAllByRole('alert')).toHaveLength(1);
  });

  it('keeps the disable confirm open when the mutation fails and closes it on success', async () => {
    vi.mocked(updateStackRuns).mockResolvedValue({ success: true });
    render(
      <MemoryRouter>
        <RunsView servers={['github']} />
      </MemoryRouter>,
    );
    await waitFor(() => expect(screen.getByRole('button', { name: /disable recording/i })).toBeInTheDocument());
    fireEvent.click(screen.getByRole('button', { name: /disable recording/i }));
    const opened = screen.getByRole('alertdialog', { name: /disable run recording/i });
    expect(opened).toHaveTextContent(/Retained history is kept/);
    fireEvent.click(within(opened).getByRole('button', { name: /^disable$/i }));
    await waitFor(() => expect(updateStackRuns).toHaveBeenCalledWith({ enabled: false }));
    await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument());

    vi.mocked(updateStackRuns).mockRejectedValue(new Error('still recording'));
    fireEvent.click(screen.getByRole('button', { name: /disable recording/i }));
    fireEvent.click(within(screen.getByRole('alertdialog')).getByRole('button', { name: /^disable$/i }));
    const dialog = await screen.findByRole('alertdialog', { name: /disable run recording/i });
    expect(within(dialog).getByRole('alert')).toHaveTextContent('still recording');
    expect(screen.getAllByRole('alert')).toHaveLength(1);
  });

  it('shows drop counts and wipe partial errors', async () => {
    vi.mocked(fetchRunsStatus).mockResolvedValue(status({ drops: { queue_full: 3 }, writer_health: 'ok' }));
    vi.mocked(wipeRuns).mockResolvedValue({ success: false, partial: true, recordingEnabled: true, scope: 'demo', error: 'wipe incomplete' });
    render(
      <MemoryRouter>
        <RunsView servers={['github']} />
      </MemoryRouter>,
    );
    await waitFor(() => expect(screen.getByText(/Loss queue_full: 3/)).toBeInTheDocument());
    fireEvent.click(screen.getByRole('button', { name: /wipe history/i }));
    expect(wipeRuns).not.toHaveBeenCalled();
    const dialog = screen.getByRole('alertdialog');
    expect(dialog).toHaveTextContent(/demo/);
    expect(dialog).toHaveTextContent(/stack-wide/i);
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(wipeRuns).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: /wipe history/i }));
    fireEvent.click(within(screen.getByRole('alertdialog')).getByRole('button', { name: /wipe history/i }));
    await waitFor(() => expect(wipeRuns).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent(/partial/i));
    expect(screen.getByRole('alert')).toHaveTextContent('wipe incomplete');
  });

  it('does not wipe when the confirm is dismissed with Escape', async () => {
    render(
      <MemoryRouter>
        <RunsView servers={['github']} />
      </MemoryRouter>,
    );
    await waitFor(() => expect(screen.getByRole('button', { name: /wipe history/i })).toBeInTheDocument());
    fireEvent.click(screen.getByRole('button', { name: /wipe history/i }));
    fireEvent.keyDown(document.body, { key: 'Escape' });
    expect(wipeRuns).not.toHaveBeenCalled();
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
  });

  it('makes the run list one tab stop with list-level keys', async () => {
    const second: RunRecord = { ...record, attemptId: 'a2', sequence: 2, resolvedTool: 'list_repos' };
    vi.mocked(fetchRuns).mockResolvedValue({ records: [record, second], warnings: [], partial: false, wipeEpoch: 0 });
    render(
      <MemoryRouter>
        <RunsView servers={['github']} />
      </MemoryRouter>,
    );
    const grid = await screen.findByRole('grid', { name: 'Run records' });
    expect(grid).toHaveAttribute('tabindex', '0');
    for (const row of within(grid).getAllByRole('row')) {
      expect(row).not.toHaveAttribute('tabindex');
    }
    expect(grid).not.toHaveAttribute('aria-activedescendant');

    fireEvent.keyDown(document.body, { key: 'ArrowDown' });
    const active = grid.getAttribute('aria-activedescendant');
    expect(active).toMatch(/^run-row-a[12]$/);
    expect(document.getElementById(active!)).not.toBeNull();
    fireEvent.keyDown(document.body, { key: 'Home' });
    expect(grid).toHaveAttribute('aria-activedescendant', 'run-row-a1');
    fireEvent.keyDown(document.body, { key: 'End' });
    expect(grid).toHaveAttribute('aria-activedescendant', 'run-row-a2');
    fireEvent.keyDown(document.body, { key: 'k' });
    expect(grid).toHaveAttribute('aria-activedescendant', 'run-row-a1');
    fireEvent.keyDown(document.body, { key: 'j' });
    expect(grid).toHaveAttribute('aria-activedescendant', 'run-row-a2');
    fireEvent.keyDown(document.body, { key: 'ArrowUp' });
    expect(grid).toHaveAttribute('aria-activedescendant', 'run-row-a1');

    fireEvent.keyDown(document.body, { key: 'Enter' });
    expect(screen.getByLabelText('Run detail')).toBeInTheDocument();
    expect(useRunsStore.getState().selectedId).toBe('a1');
    fireEvent.keyDown(document.body, { key: 'Escape' });
    expect(screen.queryByLabelText('Run detail')).not.toBeInTheDocument();
  });

  it('highlights a clicked row and selects it', async () => {
    render(
      <MemoryRouter>
        <RunsView servers={['github']} />
      </MemoryRouter>,
    );
    const grid = await screen.findByRole('grid', { name: 'Run records' });
    fireEvent.click(screen.getByText(/github › create_issue/));
    expect(grid).toHaveAttribute('aria-activedescendant', 'run-row-a1');
    expect(screen.getByLabelText('Run detail')).toBeInTheDocument();
  });

  it('lets Enter activate a focused button instead of opening a run', async () => {
    render(
      <MemoryRouter>
        <RunsView servers={['github']} />
      </MemoryRouter>,
    );
    const grid = await screen.findByRole('grid', { name: 'Run records' });
    fireEvent.keyDown(document.body, { key: 'ArrowDown' });
    expect(grid).toHaveAttribute('aria-activedescendant', 'run-row-a1');
    const save = screen.getByRole('button', { name: 'Save retention' });
    save.focus();
    expect(fireEvent.keyDown(save, { key: 'Enter' })).toBe(true);
    expect(screen.queryByLabelText('Run detail')).not.toBeInTheDocument();
  });

  it('closes only the confirm when Escape is pressed over the detail', async () => {
    render(
      <MemoryRouter>
        <RunsView servers={['github']} />
      </MemoryRouter>,
    );
    await screen.findByRole('grid', { name: 'Run records' });
    fireEvent.click(screen.getByText(/github › create_issue/));
    expect(screen.getByLabelText('Run detail')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: /wipe history/i }));
    expect(screen.getByRole('alertdialog')).toBeInTheDocument();
    fireEvent.keyDown(document.body, { key: 'Escape' });
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(screen.getByLabelText('Run detail')).toBeInTheDocument();
  });

  it('offers wipe and retention while disabled when records exist', async () => {
    vi.mocked(fetchRunsStatus).mockResolvedValue(
      status({ enabled: false, effective: false, retention_max_age_days: 3, retention_max_bytes: 50 * 1024 * 1024 }),
    );
    vi.mocked(wipeRuns).mockResolvedValue({ success: true, partial: false, recordingEnabled: false, scope: 'demo' });
    vi.mocked(updateStackRuns).mockResolvedValue({ success: true });
    render(
      <MemoryRouter>
        <RunsView servers={['github']} />
      </MemoryRouter>,
    );
    await waitFor(() => expect(screen.getByRole('button', { name: /wipe history/i })).toBeInTheDocument());
    expect(screen.queryByRole('button', { name: /disable recording/i })).not.toBeInTheDocument();
    expect(screen.queryByText('Omit caller labels')).not.toBeInTheDocument();
    expect(screen.getByLabelText('Retention max age days')).toHaveValue('3');
    expect(screen.getByLabelText('Retention max size mebibytes')).toHaveValue('50');
    expect(screen.getByText(/Applies when recording is enabled/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Save retention' }));
    await waitFor(() =>
      expect(updateStackRuns).toHaveBeenCalledWith({ retention: { max_age_days: 3, max_size_mb: 50 } }),
    );

    fireEvent.click(screen.getByRole('button', { name: /wipe history/i }));
    fireEvent.click(within(screen.getByRole('alertdialog')).getByRole('button', { name: /wipe history/i }));
    await waitFor(() => expect(wipeRuns).toHaveBeenCalledTimes(1));
  });

  it('hides wipe and retention while disabled with nothing on disk', async () => {
    vi.mocked(fetchRuns).mockResolvedValue({ records: [], warnings: [], partial: false, wipeEpoch: 0 });
    vi.mocked(fetchRunsStatus).mockResolvedValue(
      status({ enabled: false, effective: false, inventory: { stack: 'demo', signal: 'runs', path: '', sizeBytes: 0, fileCount: 0 } }),
    );
    render(
      <MemoryRouter>
        <RunsView servers={[]} />
      </MemoryRouter>,
    );
    await waitFor(() => expect(screen.getByRole('button', { name: /enable recording/i })).toBeInTheDocument());
    expect(screen.queryByRole('button', { name: /wipe history/i })).not.toBeInTheDocument();
    expect(screen.queryByLabelText('Retention max age days')).not.toBeInTheDocument();
    expect(screen.queryByLabelText('Retention max size mebibytes')).not.toBeInTheDocument();
  });

  it.each(['', 'abc', '0', '-3', '1.5', '99999'])('rejects retention age %j without sending', async (value) => {
    render(
      <MemoryRouter>
        <RunsView servers={['github']} />
      </MemoryRouter>,
    );
    const age = await screen.findByLabelText('Retention max age days');
    fireEvent.change(age, { target: { value } });
    fireEvent.click(screen.getByRole('button', { name: 'Save retention' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(/Max age must be a whole number/);
    expect(age).toHaveAttribute('aria-invalid', 'true');
    expect(screen.getByLabelText('Retention max size mebibytes')).not.toHaveAttribute('aria-invalid');
    expect(updateStackRuns).not.toHaveBeenCalled();
  });

  it.each(['', 'x', '0', '2.5', '1048577'])('rejects retention size %j without sending', async (value) => {
    render(
      <MemoryRouter>
        <RunsView servers={['github']} />
      </MemoryRouter>,
    );
    const size = await screen.findByLabelText('Retention max size mebibytes');
    fireEvent.change(size, { target: { value } });
    fireEvent.click(screen.getByRole('button', { name: 'Save retention' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(/Max size must be a whole number/);
    expect(size).toHaveAttribute('aria-invalid', 'true');
    expect(updateStackRuns).not.toHaveBeenCalled();
  });

  it('saves valid retention with both values and confirms', async () => {
    vi.mocked(updateStackRuns).mockResolvedValue({ success: true });
    render(
      <MemoryRouter>
        <RunsView servers={['github']} />
      </MemoryRouter>,
    );
    const age = await screen.findByLabelText('Retention max age days');
    const size = screen.getByLabelText('Retention max size mebibytes');
    fireEvent.change(age, { target: { value: '14' } });
    fireEvent.change(size, { target: { value: '200' } });
    vi.mocked(fetchRunsStatus).mockResolvedValue(
      status({ retention_max_age_days: 14, retention_max_bytes: 200 * 1024 * 1024 }),
    );
    fireEvent.click(screen.getByRole('button', { name: 'Save retention' }));
    await waitFor(() =>
      expect(updateStackRuns).toHaveBeenCalledWith({ retention: { max_age_days: 14, max_size_mb: 200 } }),
    );
    await waitFor(() => expect(showToast).toHaveBeenCalledWith('success', 'Retention saved'));
    await waitFor(() => expect(age).toHaveValue('14'));
    expect(size).toHaveValue('200');
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('shows the server message when a retention save is rejected', async () => {
    vi.mocked(updateStackRuns).mockRejectedValue(new HTTPError(422, 'stack validation failed after runs patch'));
    render(
      <MemoryRouter>
        <RunsView servers={['github']} />
      </MemoryRouter>,
    );
    await screen.findByLabelText('Retention max age days');
    fireEvent.click(screen.getByRole('button', { name: 'Save retention' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('stack validation failed after runs patch');
    expect(showToast).not.toHaveBeenCalled();
  });

  it('keeps rows and shows the real error when a refresh fails', async () => {
    render(
      <MemoryRouter>
        <RunsView servers={['github']} />
      </MemoryRouter>,
    );
    await waitFor(() => expect(screen.getByText(/github › create_issue/)).toBeInTheDocument());
    vi.mocked(fetchRuns).mockRejectedValue(new Error('runs store offline'));
    fireEvent.click(screen.getByRole('button', { name: 'Refresh run records' }));

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('runs store offline');
    expect(screen.getByText(/github › create_issue/)).toBeInTheDocument();
    expect(screen.getByRole('grid', { name: 'Run records' })).toBeInTheDocument();
    expect(screen.queryByText('Run history unavailable')).not.toBeInTheDocument();
    expect(screen.getByText(/History: refresh failed/)).toBeInTheDocument();

    let resolveRetry: (value: Awaited<ReturnType<typeof fetchRuns>>) => void = () => {};
    vi.mocked(fetchRuns).mockReturnValue(new Promise((resolve) => { resolveRetry = resolve; }));
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect(screen.getByText(/History: refresh failed/)).toBeInTheDocument();
    expect(screen.getByRole('alert')).toHaveTextContent('runs store offline');
    await act(async () => {
      resolveRetry({ records: [record], warnings: [], partial: false, wipeEpoch: 0 });
    });
    await waitFor(() => expect(screen.queryByRole('alert')).not.toBeInTheDocument());
    expect(screen.getByText(/History: readable/)).toBeInTheDocument();
  });

  it('keeps rows when Load older fails and retries the append', async () => {
    const cursor = { sequence: 1 } as never;
    vi.mocked(fetchRuns).mockResolvedValueOnce({
      records: [record],
      warnings: [],
      partial: false,
      wipeEpoch: 0,
      nextCursor: cursor,
    });
    render(
      <MemoryRouter>
        <RunsView servers={['github']} />
      </MemoryRouter>,
    );
    await waitFor(() => expect(screen.getByText(/github › create_issue/)).toBeInTheDocument());
    vi.mocked(fetchRuns).mockRejectedValueOnce(new HTTPError(500, 'Failed to read run history'));
    fireEvent.click(screen.getByRole('button', { name: 'Load older' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('Failed to read run history');
    expect(screen.getByText(/github › create_issue/)).toBeInTheDocument();
    expect(screen.getByRole('grid', { name: 'Run records' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Load older' })).toBeInTheDocument();
    expect(useRunsStore.getState().records).toHaveLength(1);

    vi.mocked(fetchRuns).mockClear();
    vi.mocked(fetchRuns).mockResolvedValueOnce({ records: [], warnings: [], partial: false, wipeEpoch: 0 });
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    await waitFor(() => expect(fetchRuns).toHaveBeenCalledTimes(1));
    expect(vi.mocked(fetchRuns).mock.calls[0][0]?.cursor).toBe(JSON.stringify(cursor));
  });

  it('reloads the first page on Retry after a 409 append failure', async () => {
    vi.mocked(fetchRuns).mockResolvedValueOnce({
      records: [record],
      warnings: [],
      partial: false,
      wipeEpoch: 0,
      nextCursor: { sequence: 1 } as never,
    });
    render(
      <MemoryRouter>
        <RunsView servers={['github']} />
      </MemoryRouter>,
    );
    await waitFor(() => expect(screen.getByText(/github › create_issue/)).toBeInTheDocument());
    vi.mocked(fetchRuns).mockRejectedValueOnce(new HTTPError(409, 'cursor invalidated by wipe'));
    fireEvent.click(screen.getByRole('button', { name: 'Load older' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('cursor invalidated by wipe');
    expect(screen.getByText(/github › create_issue/)).toBeInTheDocument();

    vi.mocked(fetchRuns).mockClear();
    vi.mocked(fetchRuns).mockResolvedValueOnce({ records: [record], warnings: [], partial: false, wipeEpoch: 1 });
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    await waitFor(() => expect(fetchRuns).toHaveBeenCalledTimes(1));
    expect(vi.mocked(fetchRuns).mock.calls[0][0]?.cursor).toBeUndefined();
    await waitFor(() => expect(screen.queryByRole('alert')).not.toBeInTheDocument());
  });

  it('shows the failed state with the real error on a failed first load', async () => {
    vi.mocked(fetchRuns).mockRejectedValue(new Error('disk read failed'));
    render(
      <MemoryRouter>
        <RunsView servers={[]} />
      </MemoryRouter>,
    );
    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('Run history unavailable');
    expect(alert).toHaveTextContent('disk read failed');
    expect(screen.queryByText(/No matching run records/)).not.toBeInTheDocument();
    expect(screen.getByText(/History: unreadable/)).toBeInTheDocument();
  });

  it('does not show rows from other filters after a failed filter change', async () => {
    render(
      <MemoryRouter>
        <RunsView servers={['github']} />
      </MemoryRouter>,
    );
    await waitFor(() => expect(screen.getByText(/github › create_issue/)).toBeInTheDocument());
    vi.mocked(fetchRuns).mockRejectedValue(new Error('filtered read failed'));
    act(() => {
      useRunsStore.getState().setFilters({ tool: 'list_repos' });
    });

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('Run history unavailable');
    expect(alert).toHaveTextContent('filtered read failed');
    expect(screen.queryByText(/github › create_issue/)).not.toBeInTheDocument();
    expect(screen.queryByRole('grid', { name: 'Run records' })).not.toBeInTheDocument();
    expect(useRunsStore.getState().hasLoaded).toBe(false);
  });
});

describe('useRunsStore load failures', () => {
  beforeEach(() => {
    useRunsStore.setState({
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
      filters: emptyFilters(),
      loadSeq: 0,
    });
    vi.mocked(fetchRunsStatus).mockResolvedValue(status());
  });

  it('records the status of a failed append and clears it on success', async () => {
    vi.mocked(fetchRuns).mockResolvedValueOnce({
      records: [record], warnings: [], partial: false, wipeEpoch: 0, nextCursor: { sequence: 1 } as never,
    });
    await useRunsStore.getState().load();
    vi.mocked(fetchRuns).mockRejectedValueOnce(new HTTPError(409, 'cursor invalidated by wipe'));
    await useRunsStore.getState().load({ append: true });
    expect(useRunsStore.getState()).toMatchObject({ appendFailed: true, errorStatus: 409, error: 'cursor invalidated by wipe' });
    vi.mocked(fetchRuns).mockResolvedValueOnce({ records: [record], warnings: [], partial: false, wipeEpoch: 1 });
    await useRunsStore.getState().retry();
    expect(useRunsStore.getState()).toMatchObject({ appendFailed: false, errorStatus: null, error: null });
  });

  it('keeps records on a failed refresh under unchanged filters', async () => {
    vi.mocked(fetchRuns).mockResolvedValueOnce({ records: [record], warnings: [], partial: false, wipeEpoch: 0 });
    await useRunsStore.getState().load();
    expect(useRunsStore.getState().hasLoaded).toBe(true);

    vi.mocked(fetchRuns).mockRejectedValueOnce(new Error('boom'));
    await useRunsStore.getState().load();
    const s = useRunsStore.getState();
    expect(s.records).toHaveLength(1);
    expect(s.hasLoaded).toBe(true);
    expect(s.error).toBe('boom');
  });

  it('drops records on a failed load under changed filters', async () => {
    vi.mocked(fetchRuns).mockResolvedValueOnce({ records: [record], warnings: [], partial: false, wipeEpoch: 0 });
    await useRunsStore.getState().load();
    useRunsStore.getState().setFilters({ server: 'other' });

    vi.mocked(fetchRuns).mockRejectedValueOnce(new Error('boom'));
    await useRunsStore.getState().load();
    const s = useRunsStore.getState();
    expect(s.records).toHaveLength(0);
    expect(s.hasLoaded).toBe(false);
    expect(s.error).toBe('boom');
  });

  it('keeps records on a failed append', async () => {
    vi.mocked(fetchRuns).mockResolvedValueOnce({ records: [record], warnings: [], partial: false, wipeEpoch: 0 });
    await useRunsStore.getState().load();
    vi.mocked(fetchRuns).mockRejectedValueOnce(new Error('boom'));
    await useRunsStore.getState().load({ append: true });
    expect(useRunsStore.getState().records).toHaveLength(1);
    expect(useRunsStore.getState().hasLoaded).toBe(true);
  });
});
