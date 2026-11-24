import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, waitFor, fireEvent, within } from '@testing-library/react';
import '@testing-library/jest-dom';
import { MemoryRouter, Route, Routes } from 'react-router';
import { DetachedLogsPage } from '../pages/DetachedLogsPage';
import { useUIStore } from '../stores/useUIStore';
import { useAuthStore } from '../stores/useAuthStore';
import type { LogEntry } from '../lib/api';
import type { SharedStatus } from '../hooks/useSharedStatus';
import type { GatewayStatus } from '../types';

vi.mock('../hooks/useBroadcastChannel', () => ({
  useDetachedWindowSync: vi.fn(),
  useBroadcastChannel: () => ({ postMessage: vi.fn() }),
}));

const shared: { current: SharedStatus } = {
  current: { status: null, source: null, settled: false, error: null, refresh: vi.fn() },
};

vi.mock('../hooks/useSharedStatus', () => ({
  useSharedStatus: () => shared.current,
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
  vi.mocked(fetchGatewayLogs).mockResolvedValue({
    logs: [gatewayEntry, tracedEntry],
    total: 2,
    bufferCapacity: 1000,
  });
  vi.mocked(fetchStatus).mockResolvedValue({
    'mcp-servers': [],
    resources: [],
  } as unknown as Awaited<ReturnType<typeof fetchStatus>>);
  shared.current = {
    status: {
      'mcp-servers': [{ name: 'github' }],
      resources: [{ name: 'postgres' }],
    } as unknown as GatewayStatus,
    source: 'self',
    settled: true,
    error: null,
    refresh: vi.fn().mockResolvedValue(undefined),
  };
  useUIStore.setState({
    logsPrefs: { levelParam: '', source: '', wrap: false, relativeTime: false, windowSize: 500 },
  });
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('DetachedLogsPage', () => {
  it('renders the notifications region', async () => {
    renderAt('/logs-window');
    expect(screen.getByRole('region', { name: 'Notifications' })).toBeInTheDocument();
    await waitFor(() => {
      expect(screen.getByText('gateway listening')).toBeInTheDocument();
    });
  });

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

  it('renders inside the frame with a main landmark, its title, and one notifications region', async () => {
    document.title = '';
    renderAt('/logs-window');
    expect(screen.getByRole('main')).toBeInTheDocument();
    expect(screen.getAllByRole('region', { name: 'Notifications' })).toHaveLength(1);
    expect(screen.getByRole('contentinfo')).toHaveTextContent('Detached Window');
    expect(document.title).toBe('Gridctl - Logs');
    await waitFor(() => expect(screen.getByText('gateway listening')).toBeInTheDocument());
  });

  it('opens the source menu, labels MCP servers, and closes on Escape with focus returned', async () => {
    renderAt('/logs-window');
    const trigger = screen.getByRole('button', { name: 'All sources' });
    expect(trigger).toHaveAttribute('aria-haspopup', 'menu');
    expect(trigger).toHaveAttribute('aria-expanded', 'false');
    fireEvent.click(trigger);
    expect(trigger).toHaveAttribute('aria-expanded', 'true');
    const menu = screen.getByRole('menu', { name: 'Log source' });
    expect(within(menu).getByRole('menuitemradio', { name: /github/ })).toHaveTextContent('MCP server');
    expect(within(menu).getByRole('menuitemradio', { name: /All sources/ })).toHaveAttribute('aria-checked', 'true');
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
    expect(trigger).toHaveFocus();
    await waitFor(() => expect(screen.getByText('gateway listening')).toBeInTheDocument());
  });

  it('closes the source menu on an outside click', async () => {
    renderAt('/logs-window');
    fireEvent.click(screen.getByRole('button', { name: 'All sources' }));
    fireEvent.mouseDown(screen.getByRole('main'));
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
    await waitFor(() => expect(screen.getByText('gateway listening')).toBeInTheDocument());
  });

  it('shows the disconnected banner over the loaded stream when status fails', async () => {
    shared.current = { ...shared.current, error: 'Status failed: down.' };
    renderAt('/logs-window');
    await waitFor(() => expect(screen.getByText('gateway listening')).toBeInTheDocument());
    const alerts = screen.getAllByRole('alert');
    expect(alerts).toHaveLength(1);
    expect(alerts[0]).toHaveTextContent('Status failed: down.');
    fireEvent.click(within(alerts[0]).getByRole('button', { name: 'Retry' }));
    expect(shared.current.refresh).toHaveBeenCalledTimes(1);
  });

  it('holds the banner back while the stream shows its own failure', async () => {
    shared.current = { ...shared.current, status: null, error: 'Status failed: down.' };
    vi.mocked(fetchGatewayLogs).mockRejectedValue(new Error('Gateway connection unavailable'));
    renderAt('/logs-window');
    await waitFor(() => expect(screen.getByText('Logs unavailable')).toBeInTheDocument());
    expect(screen.getAllByRole('alert')).toHaveLength(1);
    expect(screen.queryByText('Status failed: down.')).not.toBeInTheDocument();
  });

  it('does not show the banner while the credential prompt is up', async () => {
    useAuthStore.setState({ authRequired: true });
    shared.current = { ...shared.current, status: null, error: 'Status failed: down.' };
    try {
      renderAt('/logs-window');
      expect(screen.queryByText('Status failed: down.')).not.toBeInTheDocument();
    } finally {
      useAuthStore.setState({ authRequired: false });
    }
  });
});
