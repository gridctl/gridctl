import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import '@testing-library/jest-dom';
import { MemoryRouter } from 'react-router';
import { RunsView } from '../components/runs/RunsView';
import { useRunsStore } from '../stores/useRunsStore';
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

import { fetchRuns, fetchRunsStatus, updateStackRuns, wipeRuns } from '../lib/api';

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
      error: null,
      selectedId: null,
      filters: { server: '', tool: '', disposition: '', requested: '', client: '', access: '', attempt: '', parent: '', root: '', previous: '', trace: '', since: '', until: '' },
      nextCursor: null,
      statusError: null,
      mutationError: null,
      loadSeq: 0,
    });
    vi.mocked(fetchRuns).mockResolvedValue({ records: [record], warnings: [], partial: false, wipeEpoch: 0 });
    vi.mocked(fetchRunsStatus).mockResolvedValue(status());
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
    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent(/unreadable/));
    expect(screen.queryByText(/No matching run records/)).not.toBeInTheDocument();
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
    fireEvent.click(screen.getByRole('button', { name: /^enable$/i }));
    await waitFor(() => expect(updateStackRuns).toHaveBeenCalledWith({ enabled: true }));
  });

  it('shows drop counts and wipe partial errors', async () => {
    vi.mocked(fetchRunsStatus).mockResolvedValue(status({ drops: { queue_full: 3 }, writer_health: 'ok' }));
    vi.mocked(wipeRuns).mockResolvedValue({ success: false, partial: true, recordingEnabled: true, scope: 'demo' });
    render(
      <MemoryRouter>
        <RunsView servers={['github']} />
      </MemoryRouter>,
    );
    await waitFor(() => expect(screen.getByText(/Loss queue_full: 3/)).toBeInTheDocument());
    fireEvent.click(screen.getByRole('button', { name: /wipe history/i }));
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent(/partial/i));
  });

  it('supports keyboard selection', async () => {
    render(
      <MemoryRouter>
        <RunsView servers={['github']} />
      </MemoryRouter>,
    );
    await waitFor(() => expect(screen.getByRole('grid')).toBeInTheDocument());
    const row = screen.getByRole('row', { name: /github/i });
    fireEvent.keyDown(row, { key: 'Enter' });
    expect(screen.getByLabelText('Run detail')).toBeInTheDocument();
  });
});
