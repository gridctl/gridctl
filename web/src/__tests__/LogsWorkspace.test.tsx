import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import { MemoryRouter, Route, Routes } from 'react-router';
import { LogsWorkspace } from '../components/workspaces/LogsWorkspace';
import { CommandRegistryProvider } from '../hooks/useCommandRegistry';
import { useStackStore } from '../stores/useStackStore';
import { useUIStore, COMPACT_MODE_DEFAULTS } from '../stores/useUIStore';
import type { GatewayLogsResponse, LogEntry } from '../lib/api';
import type { MCPServerStatus } from '../types';

const { openDetachedWindowMock } = vi.hoisted(() => ({ openDetachedWindowMock: vi.fn() }));

vi.mock('../hooks/useWindowManager', () => ({
  useWindowManager: () => ({
    openDetachedWindow: openDetachedWindowMock,
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
  attrs: { server: 'github', tool: 'create_issue' },
};

const zapierEntry: LogEntry = {
  level: 'DEBUG',
  ts: '2026-07-23T10:00:02Z',
  msg: 'polling upstream',
  component: 'client',
  attrs: { server: 'zapier' },
};

function envelope(logs: LogEntry[]): GatewayLogsResponse {
  return { logs, total: logs.length, bufferCapacity: 1000 };
}

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
    <CommandRegistryProvider>
      <MemoryRouter initialEntries={[initialEntry]}>
        <Routes>
          <Route path="/logs" element={<LogsWorkspace />} />
          <Route path="/traces" element={<div data-testid="traces-probe" />} />
        </Routes>
      </MemoryRouter>
    </CommandRegistryProvider>,
  );
}

beforeEach(() => {
  openDetachedWindowMock.mockClear();
  vi.mocked(fetchGatewayLogs).mockResolvedValue(envelope([gatewayEntry, githubEntry, zapierEntry]));
  useStackStore.setState({ mcpServers: [server('github'), server('zapier')] });
  useUIStore.setState({
    compactMode: { ...COMPACT_MODE_DEFAULTS },
    logsDetached: false,
    // Persisted logs prefs seed a virgin /logs on mount — reset between tests
    // so a preference written by one test never leaks into the next URL.
    logsPrefs: { levelParam: '', source: '', wrap: false, relativeTime: false, windowSize: 500 },
  });
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
    expect(screen.getByRole('button', { name: /^Gateway/ })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^github/ })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^zapier/ })).toBeInTheDocument();
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

  it('filters to a single server from the ?source= param', async () => {
    renderAt('/logs?source=github');
    await waitFor(() => {
      expect(screen.getByText('tool call failed')).toBeInTheDocument();
    });
    expect(screen.queryByText('gateway listening')).not.toBeInTheDocument();
    expect(screen.queryByText('polling upstream')).not.toBeInTheDocument();
  });

  it('honors the legacy ?agent= alias permanently', async () => {
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

  it('toggles the one-click Errors filter and restores the previous levels', async () => {
    renderAt('/logs');
    await waitFor(() => {
      expect(screen.getByText('gateway listening')).toBeInTheDocument();
    });
    const errors = screen.getByRole('button', { name: 'Errors' });
    fireEvent.click(errors);
    expect(errors).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByText('tool call failed')).toBeInTheDocument();
    expect(screen.queryByText('gateway listening')).not.toBeInTheDocument();
    // Toggling off restores the previous (all-levels) selection.
    fireEvent.click(errors);
    expect(errors).toHaveAttribute('aria-pressed', 'false');
    expect(screen.getByText('gateway listening')).toBeInTheDocument();
  });

  it('matches search queries against structured attr values', async () => {
    renderAt('/logs?q=create_issue');
    await waitFor(() => {
      expect(screen.getByText('tool call failed')).toBeInTheDocument();
    });
    expect(screen.queryByText('gateway listening')).not.toBeInTheDocument();
    expect(screen.queryByText('polling upstream')).not.toBeInTheDocument();
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

  it('treats a source-only selection as an active filter with a working clear', async () => {
    renderAt('/logs?source=ghost');
    await waitFor(() => {
      expect(screen.getByText('No log lines for ghost')).toBeInTheDocument();
    });
    expect(screen.queryByText('No logs yet')).not.toBeInTheDocument();
    // Clear must also drop the source so the empty state is recoverable.
    fireEvent.click(screen.getByRole('button', { name: 'Clear filters' }));
    await waitFor(() => {
      expect(screen.getByText('gateway listening')).toBeInTheDocument();
    });
  });

  it('recomputes rail counts under the active level filter', async () => {
    renderAt('/logs?level=error');
    await waitFor(() => {
      expect(screen.getByText('tool call failed')).toBeInTheDocument();
    });
    expect(screen.getByRole('button', { name: /All sources/ })).toHaveTextContent('1');
    expect(screen.getByRole('button', { name: /^github/ })).toHaveTextContent('1');
    expect(screen.getByRole('button', { name: /^zapier/ })).toHaveTextContent('0');
    expect(screen.getByRole('button', { name: /^Gateway/ })).toHaveTextContent('0');
  });

  it('hands the full filter state to the detached window', async () => {
    renderAt('/logs?source=github&level=error&q=fail&trace=abc123def456&n=1000');
    await waitFor(() => {
      expect(screen.getByText(/entries/)).toBeInTheDocument();
    });
    fireEvent.click(screen.getByRole('button', { name: /separate window/i }));
    expect(openDetachedWindowMock).toHaveBeenCalledTimes(1);
    const params = new URLSearchParams(openDetachedWindowMock.mock.calls[0][1] as string);
    expect(params.get('source')).toBe('github');
    expect(params.get('q')).toBe('fail');
    expect(params.get('level')).toBe('error');
    expect(params.get('trace')).toBe('abc123def456');
    expect(params.get('n')).toBe('1000');
  });

  it('pivots a log line with a trace id to the Traces workspace', async () => {
    renderAt('/logs');
    await waitFor(() => {
      expect(screen.getByText('tool call failed')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByRole('button', { name: 'View trace abc123def456' }));
    expect(screen.getByTestId('traces-probe')).toBeInTheDocument();
  });

  it('lets an open popover consume Escape without collapsing the expanded row', async () => {
    renderAt('/logs');
    await waitFor(() => {
      expect(screen.getByText('tool call failed')).toBeInTheDocument();
    });
    // Expand a row, then open the export menu on top of it.
    fireEvent.click(screen.getByText('tool call failed'));
    expect(screen.getAllByText('tool call failed')).toHaveLength(2);
    fireEvent.click(screen.getByRole('button', { name: /Export filtered view/i }));
    expect(screen.getByText('Export as JSONL')).toBeInTheDocument();
    // First Escape closes only the popover; the row stays expanded.
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByText('Export as JSONL')).not.toBeInTheDocument();
    expect(screen.getAllByText('tool call failed')).toHaveLength(2);
    // Second Escape reaches the list and collapses the row.
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.getAllByText('tool call failed')).toHaveLength(1);
  });

  it('filters to a source by clicking its line badge', async () => {
    renderAt('/logs');
    await waitFor(() => {
      expect(screen.getByText('tool call failed')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByRole('button', { name: 'Filter to github' }));
    await waitFor(() => {
      expect(screen.queryByText('gateway listening')).not.toBeInTheDocument();
    });
    // Removable chip appears; clicking it restores the aggregate view.
    fireEvent.click(screen.getByRole('button', { name: 'Clear source filter' }));
    await waitFor(() => {
      expect(screen.getByText('gateway listening')).toBeInTheDocument();
    });
  });
});
