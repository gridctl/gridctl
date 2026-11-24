import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import '@testing-library/jest-dom';
import { MemoryRouter, Route, Routes } from 'react-router';
import { DetachedTracesPage } from '../pages/DetachedTracesPage';
import { useAuthStore } from '../stores/useAuthStore';
import { useTracesStore } from '../stores/useTracesStore';
import type { SharedStatus } from '../hooks/useSharedStatus';
import type { GatewayStatus } from '../types';

vi.mock('../hooks/useBroadcastChannel', () => ({
  useDetachedWindowSync: vi.fn(),
  useBroadcastChannel: () => ({ postMessage: vi.fn() }),
}));

vi.mock('../lib/api', async (importActual) => {
  const actual = await importActual<typeof import('../lib/api')>();
  return {
    ...actual,
    fetchMCPServers: vi.fn(),
    fetchTraces: vi.fn(),
    fetchTraceDetail: vi.fn(),
    fetchTraceOTLP: vi.fn(),
  };
});

import { fetchMCPServers, fetchTraces } from '../lib/api';

const captured: { props: Record<string, unknown> | null } = { props: null };

vi.mock('../components/traces/TracesView', () => ({
  TracesView: (props: Record<string, unknown>) => {
    captured.props = props;
    return <div>traces view</div>;
  },
}));

const shared: { current: SharedStatus } = {
  current: { status: null, source: null, settled: false, error: null, refresh: vi.fn() },
};

vi.mock('../hooks/useSharedStatus', () => ({
  useSharedStatus: () => shared.current,
}));

beforeEach(() => {
  localStorage.clear();
  useAuthStore.setState({ authRequired: false });
  useTracesStore.setState({ error: null });
  captured.props = null;
  shared.current = {
    status: { 'mcp-servers': [] } as unknown as GatewayStatus,
    source: 'self',
    settled: true,
    error: null,
    refresh: vi.fn().mockResolvedValue(undefined),
  };
  vi.mocked(fetchMCPServers).mockResolvedValue([]);
  vi.mocked(fetchTraces).mockResolvedValue({
    traces: [],
    total: 0,
    tracingEnabled: true,
    bufferSize: 0,
    bufferCapacity: 1000,
  } as never);
});

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/traces-window']}>
      <Routes>
        <Route path="/traces-window" element={<DetachedTracesPage />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe('DetachedTracesPage', () => {
  it('renders the notifications region', () => {
    renderPage();
    expect(screen.getByRole('region', { name: 'Notifications' })).toBeInTheDocument();
  });

  it('renders one notifications region, a main landmark, and its title', () => {
    document.title = '';
    renderPage();
    expect(screen.getAllByRole('region', { name: 'Notifications' })).toHaveLength(1);
    expect(screen.getAllByRole('main')).toHaveLength(1);
    expect(document.title).toBe('Gridctl - Traces');
    expect(screen.getByRole('button', { name: 'Fullscreen' })).toBeInTheDocument();
  });

  it('pivots to the Logs and Metrics workspaces in a new tab', () => {
    const open = vi.spyOn(window, 'open').mockReturnValue(null);
    try {
      renderPage();
      const props = captured.props as { onViewLogs: (id: string) => void; onViewMetrics: (s: string) => void };
      props.onViewLogs('abc');
      expect(open).toHaveBeenCalledWith('/logs?trace=abc', '_blank', 'noopener');
      props.onViewMetrics('db');
      expect(open).toHaveBeenCalledWith('/metrics?scope=servers&selected=db', '_blank', 'noopener');
      props.onViewMetrics('a b');
      expect(open).toHaveBeenCalledWith('/metrics?scope=servers&selected=a%20b', '_blank', 'noopener');
    } finally {
      open.mockRestore();
    }
  });

  it('shows the disconnected banner when status fails', () => {
    shared.current = { ...shared.current, error: 'Status failed: down.' };
    renderPage();
    expect(screen.getByRole('alert')).toHaveTextContent('Status failed: down.');
    expect(screen.getByText('traces view')).toBeInTheDocument();
  });

  it('holds the banner back while the trace list shows its own error', () => {
    shared.current = { ...shared.current, error: 'Status failed: down.' };
    useTracesStore.setState({ error: 'Failed to fetch traces' });
    renderPage();
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });
});
