import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import { MemoryRouter, Route, Routes } from 'react-router';
import { LogsWorkspace } from '../components/workspaces/LogsWorkspace';
import { useLogsView } from '../components/log/useLogsView';
import { parseLogEntry } from '../components/log/logTypes';
import { CommandRegistryProvider } from '../hooks/useCommandRegistry';
import { useStackStore } from '../stores/useStackStore';
import { useUIStore, COMPACT_MODE_DEFAULTS } from '../stores/useUIStore';
import type { GatewayLogsResponse, LogEntry } from '../lib/api';

vi.mock('../hooks/useWindowManager', () => ({
  useWindowManager: () => ({
    openDetachedWindow: vi.fn(),
    closeDetachedWindow: vi.fn(),
    broadcastStateUpdate: vi.fn(),
    broadcastSelectionChange: vi.fn(),
  }),
}));

vi.mock('../components/ui/Toast', async (importActual) => {
  const actual = await importActual<typeof import('../components/ui/Toast')>();
  return { ...actual, copyWithToast: vi.fn().mockResolvedValue(undefined), showToast: vi.fn() };
});

vi.mock('../lib/api', async (importActual) => {
  const actual = await importActual<typeof import('../lib/api')>();
  return { ...actual, fetchGatewayLogs: vi.fn() };
});

import { fetchGatewayLogs } from '../lib/api';
import { copyWithToast, showToast } from '../components/ui/Toast';

const entries: LogEntry[] = [
  { level: 'INFO', ts: '2026-07-23T10:00:00Z', msg: 'gateway listening', component: 'gateway' },
  { level: 'ERROR', ts: '2026-07-23T10:00:01Z', msg: 'tool call failed', component: 'client' },
  { level: 'DEBUG', ts: '2026-07-23T10:00:02Z', msg: 'polling upstream', component: 'client' },
];

function envelope(logs: LogEntry[]): GatewayLogsResponse {
  return { logs, total: logs.length, bufferCapacity: 1000 };
}

function CopyProbe() {
  const view = useLogsView();
  return (
    <button type="button" disabled={!view.hasLoaded} onClick={() => view.copyFiltered()}>
      probe copy
    </button>
  );
}

function renderAt(path: string, element = <LogsWorkspace />) {
  return render(
    <CommandRegistryProvider>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/logs" element={element} />
        </Routes>
      </MemoryRouter>
    </CommandRegistryProvider>,
  );
}

beforeEach(() => {
  vi.mocked(copyWithToast).mockClear();
  vi.mocked(showToast).mockClear();
  vi.mocked(fetchGatewayLogs).mockResolvedValue(envelope(entries));
  useStackStore.setState({ mcpServers: [] });
  useUIStore.setState({
    compactMode: { ...COMPACT_MODE_DEFAULTS },
    logsDetached: false,
    logsPrefs: { levelParam: '', source: '', wrap: false, relativeTime: false, windowSize: 500 },
  });
});

describe('Logs copy filtered view', () => {
  it('names the filter input', async () => {
    renderAt('/logs');
    await screen.findByText('gateway listening');
    expect(screen.getByRole('textbox', { name: 'Filter logs' })).toBeInTheDocument();
  });

  it('copies the raw lines with an entry-count label', async () => {
    renderAt('/logs');
    await screen.findByText('gateway listening');
    fireEvent.click(screen.getByRole('button', { name: /Copy filtered view/i }));
    expect(copyWithToast).toHaveBeenCalledTimes(1);
    const [text, label] = vi.mocked(copyWithToast).mock.calls[0];
    expect(text).toBe(entries.map((e) => parseLogEntry(e).raw).join('\n'));
    expect(label).toBe('3 entries');
  });

  it('uses the singular label for one entry', async () => {
    renderAt('/logs?q=listening');
    const button = screen.getByRole('button', { name: /Copy filtered view/i });
    await vi.waitFor(() => expect(button).toBeEnabled());
    fireEvent.click(button);
    expect(vi.mocked(copyWithToast).mock.calls[0][1]).toBe('1 entry');
  });

  it('warns and copies nothing when no entries match', async () => {
    renderAt('/logs?q=zzz-no-match', <CopyProbe />);
    const probe = await screen.findByRole('button', { name: 'probe copy' });
    await vi.waitFor(() => expect(probe).toBeEnabled());
    fireEvent.click(probe);
    expect(showToast).toHaveBeenCalledWith('warning', 'Nothing to copy');
    expect(copyWithToast).not.toHaveBeenCalled();
  });
});
