import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import { MemoryRouter, Route, Routes } from 'react-router';
import { LogsWorkspace } from '../components/workspaces/LogsWorkspace';
import { useStackStore } from '../stores/useStackStore';
import { useUIStore, COMPACT_MODE_DEFAULTS } from '../stores/useUIStore';
import type { LogEntry } from '../lib/api';
import type { MCPServerStatus } from '../types';

vi.mock('../hooks/useWindowManager', () => ({
  useWindowManager: () => ({
    openDetachedWindow: vi.fn(),
    closeDetachedWindow: vi.fn(),
    broadcastStateUpdate: vi.fn(),
    broadcastSelectionChange: vi.fn(),
  }),
}));

const gatewayEntry: LogEntry = {
  level: 'INFO',
  ts: '2026-07-23T10:00:00Z',
  msg: 'gateway listening',
  component: 'gateway',
};

const githubEntry: LogEntry = {
  level: 'ERROR',
  ts: '2026-07-23T10:00:01Z',
  msg: 'tool call failed',
  component: 'client',
  trace_id: 'abc123def456',
  attrs: { server: 'github' },
};

const zapierEntry: LogEntry = {
  level: 'DEBUG',
  ts: '2026-07-23T10:00:02Z',
  msg: 'polling upstream',
  component: 'client',
  attrs: { server: 'zapier' },
};

vi.mock('../lib/api', async (importActual) => {
  const actual = await importActual<typeof import('../lib/api')>();
  return {
    ...actual,
    fetchGatewayLogs: vi.fn(),
  };
});

import { fetchGatewayLogs } from '../lib/api';

function server(name: string): MCPServerStatus {
  return { name, transport: 'stdio', initialized: true, tools: [], healthy: true } as unknown as MCPServerStatus;
}

function renderAt(initialEntry: string) {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <Routes>
        <Route path="/logs" element={<LogsWorkspace />} />
        <Route path="/traces" element={<div data-testid="traces-probe" />} />
      </Routes>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  vi.mocked(fetchGatewayLogs).mockResolvedValue([gatewayEntry, githubEntry, zapierEntry]);
  useStackStore.setState({ mcpServers: [server('github'), server('zapier')] });
  useUIStore.setState({ compactMode: { ...COMPACT_MODE_DEFAULTS }, logsDetached: false });
});

describe('LogsWorkspace', () => {
  it('shows the aggregate stream from every source with no selection', async () => {
    renderAt('/logs');
    await waitFor(() => {
      expect(screen.getByText('gateway listening')).toBeInTheDocument();
    });
    expect(screen.getByText('tool call failed')).toBeInTheDocument();
    expect(screen.getByText('polling upstream')).toBeInTheDocument();
    expect(vi.mocked(fetchGatewayLogs)).toHaveBeenCalled();
  });

  it('renders a source rail with all sources, gateway, and each server', async () => {
    renderAt('/logs');
    await waitFor(() => {
      expect(screen.getByText('gateway listening')).toBeInTheDocument();
    });
    expect(screen.getByRole('button', { name: /All sources/ })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Gateway/ })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /github/ })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /zapier/ })).toBeInTheDocument();
  });

  it('badges each line with its source in the all-sources view', async () => {
    renderAt('/logs');
    await waitFor(() => {
      expect(screen.getByText('tool call failed')).toBeInTheDocument();
    });
    // Badge next to the entry, in addition to the rail pill.
    const githubMentions = screen.getAllByText('github');
    expect(githubMentions.length).toBeGreaterThanOrEqual(2);
  });

  it('filters to a single server from the ?agent= param', async () => {
    renderAt('/logs?agent=github');
    await waitFor(() => {
      expect(screen.getByText('tool call failed')).toBeInTheDocument();
    });
    expect(screen.queryByText('gateway listening')).not.toBeInTheDocument();
    expect(screen.queryByText('polling upstream')).not.toBeInTheDocument();
  });

  it('filters levels from the ?level= param', async () => {
    renderAt('/logs?level=error');
    await waitFor(() => {
      expect(screen.getByText('tool call failed')).toBeInTheDocument();
    });
    expect(screen.queryByText('gateway listening')).not.toBeInTheDocument();
  });

  it('filters to a single trace from the ?trace= param', async () => {
    renderAt('/logs?trace=abc123def456');
    await waitFor(() => {
      expect(screen.getByText('tool call failed')).toBeInTheDocument();
    });
    expect(screen.queryByText('gateway listening')).not.toBeInTheDocument();
    // Active-filter chip with the shortened id.
    expect(screen.getByText(/trace: abc123de/)).toBeInTheDocument();
  });

  it('keeps every level disabled through the level=none sentinel', async () => {
    renderAt('/logs?level=none');
    await waitFor(() => {
      expect(screen.getByText(/0 \/ 3 entries/)).toBeInTheDocument();
    });
    expect(screen.queryByText('gateway listening')).not.toBeInTheDocument();
    expect(screen.queryByText('tool call failed')).not.toBeInTheDocument();
    expect(screen.getByText('No entries match your filters')).toBeInTheDocument();
  });

  it('pivots a log line with a trace id to the Traces workspace', async () => {
    renderAt('/logs');
    await waitFor(() => {
      expect(screen.getByText('tool call failed')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByRole('button', { name: 'View trace abc123def456' }));
    expect(screen.getByTestId('traces-probe')).toBeInTheDocument();
  });
});
