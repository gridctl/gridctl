import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor, within, act } from '@testing-library/react';
import '@testing-library/jest-dom';
import { MemoryRouter } from 'react-router';
import { RunsView } from '../components/runs/RunsView';
import { emptyFilters, useRunsStore } from '../stores/useRunsStore';
import type { RunRecord, RunStatusResponse } from '../lib/api';

vi.mock('../components/ui/Toast', () => ({ showToast: vi.fn(), copyWithToast: vi.fn() }));

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
import { copyWithToast, showToast } from '../components/ui/Toast';
import { formatLocalDateTime } from '../lib/time';
import { dispositionTone } from '../components/runs/DispositionBadge';

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

const header = () => screen.getByRole('group', { name: 'Run recording' });

async function openSettings() {
  fireEvent.click(await screen.findByRole('button', { name: 'Recording settings' }));
  return screen.getByRole('dialog', { name: 'Recording settings' });
}

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
    expect(screen.queryByText(/No run records/)).not.toBeInTheDocument();
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
    await waitFor(() => expect(within(header()).getByRole('button', { name: /enable recording/i })).toBeInTheDocument());
    fireEvent.click(within(header()).getByRole('button', { name: /enable recording/i }));
    expect(screen.getByText(/Metadata only/)).toBeInTheDocument();
    expect(screen.getByText(/Seven days and 100 MiB/)).toBeInTheDocument();
    const dialog = screen.getByRole('alertdialog', { name: /enable run recording/i });
    expect(dialog).toHaveTextContent(/Metadata only/);
    expect(dialog).toHaveTextContent(/Seven days and 100 MiB/);
    fireEvent.keyDown(document.body, { key: 'Escape' });
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(updateStackRuns).not.toHaveBeenCalled();

    fireEvent.click(within(header()).getByRole('button', { name: /enable recording/i }));
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
    await waitFor(() => expect(within(header()).getByRole('button', { name: /enable recording/i })).toBeInTheDocument());
    fireEvent.click(within(header()).getByRole('button', { name: /enable recording/i }));
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
    await waitFor(() => expect(screen.getByText('3 records not saved (queue_full: 3)')).toBeInTheDocument());
    expect(screen.getByText('3 records not saved (queue_full: 3)')).toHaveAttribute('role', 'status');
    fireEvent.click(within(await openSettings()).getByRole('button', { name: /wipe history/i }));
    expect(screen.queryByRole('dialog', { name: 'Recording settings' })).not.toBeInTheDocument();
    expect(wipeRuns).not.toHaveBeenCalled();
    const dialog = screen.getByRole('alertdialog');
    expect(dialog).toHaveTextContent(/demo/);
    expect(dialog).toHaveTextContent(/stack-wide/i);
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(wipeRuns).not.toHaveBeenCalled();

    fireEvent.click(within(await openSettings()).getByRole('button', { name: /wipe history/i }));
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
    fireEvent.click(within(await openSettings()).getByRole('button', { name: /wipe history/i }));
    expect(screen.getByRole('alertdialog')).toBeInTheDocument();
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
    const save = within(await openSettings()).getByRole('button', { name: 'Save retention' });
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
    fireEvent.click(within(await openSettings()).getByRole('button', { name: /wipe history/i }));
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
    const settings = await openSettings();
    expect(within(settings).getByRole('button', { name: /wipe history/i })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /disable recording/i })).not.toBeInTheDocument();
    expect(screen.queryByText('Omit caller labels')).not.toBeInTheDocument();
    expect(screen.getByLabelText('Retention max age days')).toHaveValue('3');
    expect(screen.getByLabelText('Retention max size mebibytes')).toHaveValue('50');
    expect(screen.getByText(/Applies when recording is enabled/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Save retention' }));
    await waitFor(() =>
      expect(updateStackRuns).toHaveBeenCalledWith({ retention: { max_age_days: 3, max_size_mb: 50 } }),
    );

    fireEvent.click(within(settings).getByRole('button', { name: /wipe history/i }));
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
    await waitFor(() => expect(within(header()).getByRole('button', { name: /enable recording/i })).toBeInTheDocument());
    expect(screen.queryByRole('button', { name: 'Recording settings' })).not.toBeInTheDocument();
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
    await openSettings();
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
    await openSettings();
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
    await openSettings();
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
    await openSettings();
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
    expect(screen.queryByText(/History:/)).not.toBeInTheDocument();

    let resolveRetry: (value: Awaited<ReturnType<typeof fetchRuns>>) => void = () => {};
    vi.mocked(fetchRuns).mockReturnValue(new Promise((resolve) => { resolveRetry = resolve; }));
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect(screen.getByRole('alert')).toHaveTextContent('runs store offline');
    await act(async () => {
      resolveRetry({ records: [record], warnings: [], partial: false, wipeEpoch: 0 });
    });
    await waitFor(() => expect(screen.queryByRole('alert')).not.toBeInTheDocument());
    expect(screen.queryByText(/History:/)).not.toBeInTheDocument();
  });

  it('shows the full failed state when a refresh of an empty history fails', async () => {
    vi.mocked(fetchRuns).mockResolvedValue({ records: [], warnings: [], partial: false, wipeEpoch: 0 });
    render(
      <MemoryRouter>
        <RunsView servers={['github']} />
      </MemoryRouter>,
    );
    await waitFor(() => expect(screen.getByText('No run records to show')).toBeInTheDocument());
    vi.mocked(fetchRuns).mockRejectedValue(new Error('runs store offline'));
    fireEvent.click(screen.getByRole('button', { name: 'Refresh run records' }));

    await waitFor(() => expect(screen.getByText('Run history unavailable')).toBeInTheDocument());
    expect(screen.getByText('runs store offline')).toBeInTheDocument();
    expect(screen.queryByRole('grid', { name: 'Run records' })).not.toBeInTheDocument();
  });

  it('clears a retention error when the field is edited', async () => {
    render(
      <MemoryRouter>
        <RunsView servers={['github']} />
      </MemoryRouter>,
    );
    await openSettings();
    const age = await screen.findByLabelText('Retention max age days');
    fireEvent.change(age, { target: { value: '0' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save retention' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(/Max age must be a whole number/);
    fireEvent.change(age, { target: { value: '14' } });
    expect(age).not.toHaveAttribute('aria-invalid');
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();

    const size = screen.getByLabelText('Retention max size mebibytes');
    fireEvent.change(size, { target: { value: 'x' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save retention' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(/Max size must be a whole number/);
    fireEvent.change(size, { target: { value: '200' } });
    expect(size).not.toHaveAttribute('aria-invalid');
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
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
    expect(screen.queryByText(/No run records/)).not.toBeInTheDocument();
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


describe('RunsView header, rows, detail, and empty state', () => {
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
    vi.mocked(copyWithToast).mockReset();
  });

  const renderRuns = (props: Partial<Parameters<typeof RunsView>[0]> = {}) =>
    render(
      <MemoryRouter>
        <RunsView servers={['github']} {...props} />
      </MemoryRouter>,
    );

  const empty = { inventory: { stack: 'demo', signal: 'runs', path: '', sizeBytes: 0, fileCount: 0 } };

  it('states where records go and hides writer and history labels', async () => {
    vi.mocked(fetchRunsStatus).mockResolvedValue(
      status({ inventory: { stack: 'demo', signal: 'runs', path: '/home/u/.gridctl/runs/demo/runs.jsonl', sizeBytes: 2048, fileCount: 2 } }),
    );
    renderRuns();
    await waitFor(() => expect(header()).toHaveTextContent('Recording to'));
    expect(within(header()).getByText('/home/u/.gridctl/runs/demo/runs.jsonl')).toHaveAttribute(
      'title',
      '/home/u/.gridctl/runs/demo/runs.jsonl',
    );
    expect(header()).toHaveTextContent('2.0 KiB in 2 files');
    expect(screen.queryByText(/Writer:/)).not.toBeInTheDocument();
    expect(screen.queryByText(/History:/)).not.toBeInTheDocument();
    expect(screen.queryByText('Records are saved after dispatch attempts return.')).not.toBeInTheDocument();
  });

  it('shows a checking line before the status loads', async () => {
    vi.mocked(fetchRunsStatus).mockReturnValue(new Promise(() => {}));
    renderRuns();
    expect(header()).toHaveTextContent('Checking recording status…');
  });

  it('reports a failed status request', async () => {
    vi.mocked(fetchRunsStatus).mockRejectedValue(new Error('no status'));
    renderRuns();
    const line = await screen.findByText('Recording status unavailable');
    expect(line).toHaveAttribute('role', 'status');
  });

  it('says recording is off and states retained size', async () => {
    vi.mocked(fetchRunsStatus).mockResolvedValue(
      status({ enabled: false, effective: false, inventory: { stack: 'demo', signal: 'runs', path: '/p', sizeBytes: 3072, fileCount: 1 } }),
    );
    renderRuns();
    await waitFor(() => expect(header()).toHaveTextContent('Recording is off'));
    expect(header()).toHaveTextContent('3.0 KiB of earlier records kept on disk');
  });

  it('says recording is off without a size when nothing is retained', async () => {
    vi.mocked(fetchRuns).mockResolvedValue({ records: [], warnings: [], partial: false, wipeEpoch: 0 });
    vi.mocked(fetchRunsStatus).mockResolvedValue(status({ enabled: false, effective: false, ...empty }));
    renderRuns();
    await waitFor(() => expect(header()).toHaveTextContent('Recording is off'));
    expect(header()).not.toHaveTextContent('kept on disk');
  });

  it('flags a recorder that is on but not saving', async () => {
    vi.mocked(fetchRunsStatus).mockResolvedValue(status({ effective: false, writer_health: 'degraded' }));
    renderRuns();
    const line = await screen.findByText('Recording is on, but records are not being saved (writer degraded)');
    expect(line).toHaveAttribute('role', 'status');
    expect(line).toHaveClass('text-status-error');
  });

  it('shows partial reads and never says unknown or readable', async () => {
    vi.mocked(fetchRuns).mockResolvedValue({ records: [record], warnings: [], partial: true, wipeEpoch: 0 });
    vi.mocked(fetchRunsStatus).mockResolvedValue(status({ writer_health: '', failures: { write_error: 2 } }));
    renderRuns();
    const partialLine = await screen.findByText('Some records could not be read');
    expect(partialLine).toHaveAttribute('role', 'status');
    expect(screen.getByText('2 records not saved (write_error: 2)')).toHaveAttribute('role', 'status');
    expect(screen.queryByText(/Writer: unknown/)).not.toBeInTheDocument();
    expect(screen.queryByText(/History: readable/)).not.toBeInTheDocument();
  });

  it('counts a failure that shares a drop code once', async () => {
    vi.mocked(fetchRunsStatus).mockResolvedValue(status({ drops: { queue_full: 1 }, failures: { queue_full: 1 } }));
    renderRuns();
    expect(await screen.findByText('1 record not saved (queue_full: 1)')).toBeInTheDocument();
  });

  it('opens About with every note and returns focus on Escape', async () => {
    renderRuns();
    const about = await screen.findByRole('button', { name: 'About run records' });
    expect(about).toHaveAttribute('aria-expanded', 'false');
    fireEvent.click(about);
    expect(about).toHaveAttribute('aria-expanded', 'true');
    const dialog = screen.getByRole('dialog', { name: 'About run records' });
    expect(dialog).toHaveTextContent('Records are saved after dispatch attempts return.');
    expect(dialog).toHaveTextContent('Metadata only.');
    expect(dialog).toHaveTextContent('Seven days and 100 MiB.');
    fireEvent.keyDown(document.activeElement ?? document.body, { key: 'Escape' });
    expect(screen.queryByRole('dialog', { name: 'About run records' })).not.toBeInTheDocument();
    expect(about).toHaveAttribute('aria-expanded', 'false');
    expect(document.activeElement).toBe(about);
  });

  it('puts retention, caller labels, and wipe in Recording settings', async () => {
    vi.mocked(wipeRuns).mockResolvedValue({ success: true, partial: false, recordingEnabled: true, scope: 'demo' });
    renderRuns();
    const trigger = await screen.findByRole('button', { name: 'Recording settings' });
    expect(trigger).toHaveAttribute('aria-expanded', 'false');
    expect(screen.queryByLabelText('Retention max age days')).not.toBeInTheDocument();
    fireEvent.click(trigger);
    expect(trigger).toHaveAttribute('aria-expanded', 'true');
    const settings = screen.getByRole('dialog', { name: 'Recording settings' });
    expect(within(settings).getByLabelText('Retention max age days')).toBeInTheDocument();
    expect(within(settings).getByLabelText('Retention max size mebibytes')).toBeInTheDocument();
    expect(within(settings).getByRole('button', { name: 'Save retention' })).toBeInTheDocument();
    expect(within(settings).getByText('Omit caller labels')).toBeInTheDocument();
    fireEvent.click(within(settings).getByRole('button', { name: 'Wipe history' }));
    expect(screen.queryByRole('dialog', { name: 'Recording settings' })).not.toBeInTheDocument();
    const confirm = screen.getByRole('alertdialog', { name: /wipe run history/i });
    fireEvent.click(within(confirm).getByRole('button', { name: 'Wipe history' }));
    await waitFor(() => expect(wipeRuns).toHaveBeenCalledTimes(1));
  });

  it('renders local time, a formatted duration, and a toned badge per row', async () => {
    const slow: RunRecord = { ...record, attemptId: 'a2', sequence: 2, durationMs: 1500, disposition: 'tool_error' };
    const denied: RunRecord = { ...record, attemptId: 'a3', sequence: 3, durationMs: 3, disposition: 'denied' };
    vi.mocked(fetchRuns).mockResolvedValue({ records: [record, slow, denied], warnings: [], partial: false, wipeEpoch: 0 });
    renderRuns();
    const grid = await screen.findByRole('grid', { name: 'Run records' });
    const rows = within(grid).getAllByRole('row').slice(1);
    const cells = Array.from(rows[0].querySelectorAll('td'));
    const time = cells[0];
    expect(time).toHaveAttribute('title', record.returnedAt);
    expect(time.textContent).toBe(formatLocalDateTime(record.returnedAt));
    expect(time.textContent).not.toBe(record.returnedAt);
    expect(rows[0]).toHaveTextContent('12ms');
    expect(rows[1]).toHaveTextContent('1.50s');
    const badge = (row: HTMLElement, text: string) => within(row).getByText(text);
    const done = badge(rows[0], 'completed');
    const failed = badge(rows[1], 'tool_error');
    const refused = badge(rows[2], 'denied');
    expect(done.className).not.toBe(failed.className);
    expect(failed.className).not.toBe(refused.className);
    expect(done.className).not.toBe(refused.className);
  });

  it('titles the target cell with a requested name that differs', async () => {
    renderRuns();
    const cell = await screen.findByText('github › create_issue');
    expect(cell).toHaveAttribute('title', 'github__create_issue');
  });

  it('lists every recorded field in the detail', async () => {
    const full: RunRecord = {
      ...record,
      parentAttemptId: 'p1',
      rootAttemptId: 'root1',
      previousAttemptId: 'prev1',
      downstreamDurationMs: 0,
      replicaId: 0,
      traceId: 'trace-abc',
      clientLabel: 'cli',
      accessLabel: 'ops',
      recorderInstanceId: 'rec-9',
      sequence: 42,
    };
    vi.mocked(fetchRuns).mockResolvedValue({ records: [full], warnings: [], partial: false, wipeEpoch: 0 });
    renderRuns({ onViewTrace: vi.fn(), onViewLogs: vi.fn() });
    fireEvent.click(await screen.findByText('github › create_issue'));
    const detail = screen.getByRole('complementary', { name: 'Run detail' });
    for (const label of ['Started', 'Returned', 'Duration', 'Downstream', 'Replica', 'Attempt', 'Trace', 'Parent', 'Root', 'Previous', 'Client', 'Access', 'Sequence', 'Recorder instance', 'Schema version', 'Stage', 'Reason']) {
      expect(within(detail).getByText(label)).toBeInTheDocument();
    }
    for (const value of ['a1', 'trace-abc', 'p1', 'root1', 'prev1', 'cli', 'ops', '42', 'rec-9', '0ms']) {
      expect(within(detail).getAllByText(value).length).toBeGreaterThan(0);
    }
    expect(within(detail).getByTitle(full.startedAt)).toHaveTextContent(formatLocalDateTime(full.startedAt));
    expect(within(detail).getByText('Replica').closest('tr')).toHaveTextContent('0');
    expect(within(detail).getByText('Parent record is unavailable, not still running.')).toBeInTheDocument();
  });

  it('shows none and not recorded for absent fields', async () => {
    renderRuns();
    fireEvent.click(await screen.findByText('github › create_issue'));
    const detail = screen.getByRole('complementary', { name: 'Run detail' });
    expect(within(detail).getByText('Parent').closest('tr')).toHaveTextContent('none');
    expect(within(detail).getByText('Previous').closest('tr')).toHaveTextContent('none');
    expect(within(detail).getByText('Root').closest('tr')).toHaveTextContent('a1');
    expect(within(detail).getByText('Client').closest('tr')).toHaveTextContent('not recorded');
    expect(within(detail).queryByText('Replica')).not.toBeInTheDocument();
    expect(within(detail).queryByText('Downstream')).not.toBeInTheDocument();
    expect(within(detail).queryByText('Parent record is unavailable, not still running.')).not.toBeInTheDocument();
  });

  it('marks omitted target names', async () => {
    const omitted: RunRecord = { ...record, requestedName: undefined, requestedNameOmitted: 'too_long' };
    vi.mocked(fetchRuns).mockResolvedValue({ records: [omitted], warnings: [], partial: false, wipeEpoch: 0 });
    renderRuns();
    fireEvent.click(await screen.findByText('github › create_issue'));
    expect(screen.getByText('omitted (too_long)')).toBeInTheDocument();
  });

  it('copies IDs through the toast path', async () => {
    vi.mocked(fetchRuns).mockResolvedValue({ records: [{ ...record, traceId: 'trace-abc' }], warnings: [], partial: false, wipeEpoch: 0 });
    renderRuns();
    fireEvent.click(await screen.findByText('github › create_issue'));
    fireEvent.click(screen.getByRole('button', { name: 'Copy attempt ID' }));
    expect(copyWithToast).toHaveBeenCalledWith('a1', 'Attempt ID');
    fireEvent.click(screen.getByRole('button', { name: 'Copy trace ID' }));
    expect(copyWithToast).toHaveBeenCalledWith('trace-abc', 'Trace ID');
    expect(screen.getByRole('button', { name: 'Copy root ID' })).toBeInTheDocument();
  });

  it('selects a loaded parent and lists nested attempts with badges', async () => {
    const parent: RunRecord = { ...record, attemptId: 'p1', sequence: 2, requestedName: 'call_tool', resolvedServer: undefined, resolvedTool: undefined };
    const child: RunRecord = { ...record, parentAttemptId: 'p1', disposition: 'tool_error' };
    vi.mocked(fetchRuns).mockResolvedValue({ records: [child, parent], warnings: [], partial: false, wipeEpoch: 0 });
    renderRuns();
    fireEvent.click(await screen.findByText('github › create_issue'));
    let detail = screen.getByRole('complementary', { name: 'Run detail' });
    fireEvent.click(within(detail).getByRole('button', { name: 'p1' }));
    expect(useRunsStore.getState().selectedId).toBe('p1');
    detail = screen.getByRole('complementary', { name: 'Run detail' });
    const nested = within(detail).getByText('Nested attempts').closest('section')!;
    expect(within(nested).getByRole('button', { name: 'github__create_issue' })).toBeInTheDocument();
    expect(within(nested).getByText('tool_error')).toBeInTheDocument();
  });

  it('selects a loaded previous run and keeps focus in the detail', async () => {
    const prev: RunRecord = { ...record, attemptId: 'prev1', sequence: 2 };
    const retry: RunRecord = { ...record, attemptId: 'a2', sequence: 3, previousAttemptId: 'prev1' };
    vi.mocked(fetchRuns).mockResolvedValue({ records: [retry, prev], warnings: [], partial: false, wipeEpoch: 0 });
    renderRuns();
    fireEvent.click((await screen.findAllByText('github › create_issue'))[0]);
    expect(useRunsStore.getState().selectedId).toBe('a2');
    const detail = screen.getByRole('complementary', { name: 'Run detail' });
    const prevButton = within(detail).getByRole('button', { name: 'prev1' });
    prevButton.focus();
    fireEvent.click(prevButton);
    expect(useRunsStore.getState().selectedId).toBe('prev1');
    expect(within(screen.getByRole('complementary', { name: 'Run detail' })).queryByRole('button', { name: 'prev1' })).not.toBeInTheDocument();
    expect(document.activeElement).toBe(screen.getByRole('complementary', { name: 'Run detail' }));
  });

  it('returns focus to the grid when Escape closes the detail from inside it', async () => {
    renderRuns();
    fireEvent.click(await screen.findByText('github › create_issue'));
    const copy = screen.getByRole('button', { name: 'Copy attempt ID' });
    copy.focus();
    fireEvent.keyDown(copy, { key: 'Escape' });
    expect(screen.queryByLabelText('Run detail')).not.toBeInTheDocument();
    expect(document.activeElement).toBe(screen.getByRole('grid', { name: 'Run records' }));
  });

  it('closes with the button and returns focus to the grid', async () => {
    renderRuns();
    fireEvent.click(await screen.findByText('github › create_issue'));
    fireEvent.click(screen.getByRole('button', { name: 'Close run detail' }));
    expect(screen.queryByLabelText('Run detail')).not.toBeInTheDocument();
    expect(useRunsStore.getState().selectedId).toBeNull();
    expect(document.activeElement).toBe(screen.getByRole('grid', { name: 'Run records' }));
  });

  it('closes only an open popover on Escape, then the detail', async () => {
    renderRuns();
    fireEvent.click(await screen.findByText('github › create_issue'));
    fireEvent.click(screen.getByRole('button', { name: 'About run records' }));
    fireEvent.keyDown(document.activeElement ?? document.body, { key: 'Escape' });
    expect(screen.queryByRole('dialog', { name: 'About run records' })).not.toBeInTheDocument();
    expect(screen.getByLabelText('Run detail')).toBeInTheDocument();
    fireEvent.keyDown(document.body, { key: 'Escape' });
    expect(screen.queryByLabelText('Run detail')).not.toBeInTheDocument();
  });

  it('links a sampled run to its trace and logs', async () => {
    const onViewTrace = vi.fn();
    const onViewLogs = vi.fn();
    vi.mocked(fetchRuns).mockResolvedValue({ records: [{ ...record, traceId: 'trace-abc' }], warnings: [], partial: false, wipeEpoch: 0 });
    renderRuns({ onViewTrace, onViewLogs });
    fireEvent.click(await screen.findByText('github › create_issue'));
    fireEvent.click(screen.getByRole('button', { name: 'View trace' }));
    expect(onViewTrace).toHaveBeenCalledWith('trace-abc');
    fireEvent.click(screen.getByRole('button', { name: 'View logs' }));
    expect(onViewLogs).toHaveBeenCalledWith('trace-abc');
    expect(screen.queryByText('No trace was sampled for this run.')).not.toBeInTheDocument();
  });

  it('says no trace was sampled for a run without one', async () => {
    renderRuns({ onViewTrace: vi.fn(), onViewLogs: vi.fn() });
    fireEvent.click(await screen.findByText('github › create_issue'));
    expect(screen.getByText('No trace was sampled for this run.')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'View trace' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'View logs' })).not.toBeInTheDocument();
  });

  it('explains run records and offers Enable when recording is off', async () => {
    vi.mocked(fetchRuns).mockResolvedValue({ records: [], warnings: [], partial: false, wipeEpoch: 0 });
    vi.mocked(fetchRunsStatus).mockResolvedValue(status({ enabled: false, effective: false, ...empty }));
    renderRuns();
    const body = (await screen.findByText('Run recording is off')).closest('[role="status"]') as HTMLElement;
    expect(body).toHaveTextContent(/Metrics are aggregate counts and traces are sampled timing/);
    expect(within(body).getByText('runs.enabled: true').tagName).toBe('CODE');
    fireEvent.click(within(body).getByRole('button', { name: 'Enable recording' }));
    const dialog = screen.getByRole('alertdialog', { name: /enable run recording/i });
    expect(dialog).toHaveTextContent('Metadata only.');
    expect(dialog).toHaveTextContent('Seven days and 100 MiB.');
  });

  it('notes earlier records when recording is off with history retained', async () => {
    vi.mocked(fetchRuns).mockResolvedValue({ records: [], warnings: [], partial: false, wipeEpoch: 0 });
    vi.mocked(fetchRunsStatus).mockResolvedValue(status({ enabled: false, effective: false }));
    renderRuns();
    const body = (await screen.findByText('No run records to show')).closest('[role="status"]') as HTMLElement;
    expect(body).toHaveTextContent('Recording is off. Earlier records are still on disk.');
    expect(within(body).getByRole('button', { name: 'Enable recording' })).toBeInTheDocument();
  });

  it('offers Clear filters when a filter matches nothing', async () => {
    vi.mocked(fetchRuns).mockResolvedValue({ records: [], warnings: [], partial: false, wipeEpoch: 0 });
    useRunsStore.setState({ filters: { ...emptyFilters(), disposition: 'denied' } });
    renderRuns();
    const body = (await screen.findByText('No run records match your filters')).closest('[role="status"]') as HTMLElement;
    fireEvent.click(within(body).getByRole('button', { name: 'Clear filters' }));
    expect(useRunsStore.getState().filters).toEqual(emptyFilters());
  });

  it('says no records yet while recording with nothing retained', async () => {
    vi.mocked(fetchRuns).mockResolvedValue({ records: [], warnings: [], partial: false, wipeEpoch: 0 });
    vi.mocked(fetchRunsStatus).mockResolvedValue(status(empty));
    renderRuns();
    const body = (await screen.findByText('No run records yet')).closest('[role="status"]') as HTMLElement;
    vi.mocked(fetchRuns).mockClear();
    fireEvent.click(within(body).getByRole('button', { name: 'Refresh' }));
    await waitFor(() => expect(fetchRuns).toHaveBeenCalledTimes(1));
  });
});

describe('dispositionTone', () => {
  it('tones success, failure, refusal, and anything else', () => {
    expect(dispositionTone('completed')).toContain('status-running');
    for (const d of ['tool_error', 'routing_failed', 'transport_error', 'timeout']) {
      expect(dispositionTone(d)).toContain('status-error');
    }
    for (const d of ['denied', 'retry_rejected']) {
      expect(dispositionTone(d)).toContain('status-pending');
    }
    for (const d of ['cancelled', 'input_required', 'something_new', 'constructor']) {
      expect(dispositionTone(d)).toContain('text-text-muted');
    }
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
