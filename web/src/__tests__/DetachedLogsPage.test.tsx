import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import { MemoryRouter, Route, Routes } from 'react-router';
import { DetachedLogsPage } from '../pages/DetachedLogsPage';
import type { LogEntry } from '../lib/api';

vi.mock('../hooks/useBroadcastChannel', () => ({
  useDetachedWindowSync: vi.fn(),
}));

vi.mock('../lib/api', async (importActual) => {
  const actual = await importActual<typeof import('../lib/api')>();
  return {
    ...actual,
    fetchGatewayLogs: vi.fn(),
    fetchStatus: vi.fn(),
  };
});

import { fetchGatewayLogs, fetchStatus } from '../lib/api';

const gatewayEntry: LogEntry = {
  level: 'INFO',
  ts: '2026-07-23T10:00:00Z',
  msg: 'gateway listening',
  component: 'gateway',
};

const tracedEntry: LogEntry = {
  level: 'ERROR',
  ts: '2026-07-23T10:00:01Z',
  msg: 'tool call failed',
  component: 'client',
  trace_id: 'abc123def456',
  attrs: { server: 'github' },
};

function renderAt(initialEntry: string) {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <Routes>
        <Route path="/logs-window" element={<DetachedLogsPage />} />
      </Routes>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  vi.mocked(fetchGatewayLogs).mockResolvedValue([gatewayEntry, tracedEntry]);
  vi.mocked(fetchStatus).mockResolvedValue({
    'mcp-servers': [],
    resources: [],
  } as unknown as Awaited<ReturnType<typeof fetchStatus>>);
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('DetachedLogsPage', () => {
  it('filters to a single trace from the ?trace= param with a clearable chip', async () => {
    renderAt('/logs-window?trace=abc123def456');
    await waitFor(() => {
      expect(screen.getByText('tool call failed')).toBeInTheDocument();
    });
    expect(screen.queryByText('gateway listening')).not.toBeInTheDocument();
    expect(screen.getByText(/trace: abc123de/)).toBeInTheDocument();

    fireEvent.click(screen.getByText(/trace: abc123de/));
    await waitFor(() => {
      expect(screen.getByText('gateway listening')).toBeInTheDocument();
    });
  });

  it('pivots a traced log line to the Traces workspace in a full tab', async () => {
    const open = vi.spyOn(window, 'open').mockReturnValue(null);
    renderAt('/logs-window');
    await waitFor(() => {
      expect(screen.getByText('tool call failed')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByRole('button', { name: 'View trace abc123def456' }));
    expect(open).toHaveBeenCalledWith('/traces?trace=abc123def456', '_blank', 'noopener');
  });

  it('honors the level=none sentinel from the URL', async () => {
    renderAt('/logs-window?level=none');
    await waitFor(() => {
      expect(screen.getByText('No entries match your filters')).toBeInTheDocument();
    });
    expect(screen.queryByText('gateway listening')).not.toBeInTheDocument();
    expect(screen.queryByText('tool call failed')).not.toBeInTheDocument();
  });
});
