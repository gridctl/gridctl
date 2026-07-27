import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import '@testing-library/jest-dom';
import { FleetActions } from '../components/workspaces/FleetActions';
import * as api from '../lib/api';
import type { MCPServerStatus, ToolUsageResponse } from '../types';

vi.mock('../components/ui/Toast', () => ({ showToast: vi.fn() }));

function server(name: string, tools: string[], toolWhitelist?: string[]): MCPServerStatus {
  return { name, transport: 'stdio', initialized: true, toolCount: tools.length, tools, toolWhitelist, healthy: true } as unknown as MCPServerStatus;
}

const servers = [
  server('github', ['create_issue', 'delete_issue', 'delete_repo']), // expose-all
  server('atlassian', ['get_page']),
];

beforeEach(() => {
  vi.spyOn(api, 'fetchStatus').mockResolvedValue({
    gateway: { name: 'g', version: '1' },
    'mcp-servers': [],
  } as unknown as Awaited<ReturnType<typeof api.fetchStatus>>);
  vi.spyOn(api, 'fetchTools').mockResolvedValue({ tools: [] });
});

afterEach(() => {
  vi.restoreAllMocks();
});

const WINDOW_MS = 7 * 24 * 60 * 60 * 1000;
const NOW = Date.parse('2026-07-26T12:00:00Z');

// github: create_issue used recently, delete_issue/delete_repo unused;
// atlassian: get_page unused (its only exposed tool → blocked for
// disable-unused).
const usage: ToolUsageResponse = {
  observedSince: '2026-07-01T00:00:00Z',
  servers: {
    github: { create_issue: { calls: 5, lastCalledAt: '2026-07-26T10:00:00Z' } },
  },
};

function renderPanel(overrides: Partial<React.ComponentProps<typeof FleetActions>> = {}) {
  return render(
    <FleetActions
      isOpen
      onClose={vi.fn()}
      servers={servers}
      activeServerName="github"
      usage={usage}
      windowMs={WINDOW_MS}
      windowLabel="7 days"
      fetchedAt={NOW}
      {...overrides}
    />,
  );
}

describe('FleetActions', () => {
  it('echoes the resolved match count for a hide pattern', () => {
    renderPanel();
    fireEvent.click(screen.getByRole('button', { name: /hide matching pattern/i }));
    fireEvent.change(screen.getByLabelText(/glob pattern/i), { target: { value: 'delete_*' } });

    // delete_issue + delete_repo on github → 2 tools across 1 server.
    expect(screen.getByText(/Matches/)).toHaveTextContent('Matches 2 tools across 1 server');
    expect(screen.getByRole('button', { name: /review & apply \(1\)/i })).toBeEnabled();
  });

  it('requires a consequence-stating confirmation, then applies via the batch endpoint', async () => {
    const batchSpy = vi
      .spyOn(api, 'setServerToolsBatch')
      .mockResolvedValue({ servers: [{ server: 'github', tools: ['create_issue'] }], reloaded: true });

    renderPanel();
    fireEvent.click(screen.getByRole('button', { name: /hide matching pattern/i }));
    fireEvent.change(screen.getByLabelText(/glob pattern/i), { target: { value: 'delete_*' } });
    fireEvent.click(screen.getByRole('button', { name: /review & apply/i }));

    // The confirmation states the single-reload consequence.
    expect(screen.getByText(/single reload/i)).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: /apply & reload/i }));

    // Batch payload keeps the non-matching tool as an explicit whitelist.
    await waitFor(() =>
      expect(batchSpy).toHaveBeenCalledWith([{ name: 'github', tools: ['create_issue'] }]),
    );
    // Per-server result summary.
    expect(await screen.findByText(/Updated 1 server/)).toBeInTheDocument();
    expect(screen.getByText('✓ github')).toBeInTheDocument();
  });

  it('disables apply when an expose-all action would change nothing', () => {
    // No server restricts tools → expose-all is a no-op.
    renderPanel(); // default action is expose-all
    expect(screen.getByText(/already exposes all/i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /review & apply \(0\)/i })).toBeDisabled();
  });

  it('plans disable-unused with a blocked server and the observedSince caveat', () => {
    renderPanel();
    fireEvent.click(screen.getByRole('button', { name: /disable unused/i }));

    // github keeps create_issue (used) and drops the two unused; atlassian's
    // only exposed tool is unused → blocked, never emptied to expose-all.
    // The count covers only servers that actually change (blocked servers'
    // tools are excluded so the copy never overstates the apply).
    expect(screen.getByText(/Disables/)).toHaveTextContent(
      'Disables 2 tools with no recorded calls in the last 7 days across 1 server',
    );
    expect(screen.getByText(/1 server skipped/)).toHaveTextContent(/every exposed tool is unused/);
    expect(screen.getByText(/Tracking since/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /review & apply \(1\)/i })).toBeEnabled();
  });

  it('applies disable-unused through the batch endpoint with the kept whitelist', async () => {
    const batchSpy = vi
      .spyOn(api, 'setServerToolsBatch')
      .mockResolvedValue({ servers: [{ server: 'github', tools: ['create_issue'] }], reloaded: true });

    renderPanel();
    fireEvent.click(screen.getByRole('button', { name: /disable unused/i }));
    fireEvent.click(screen.getByRole('button', { name: /review & apply/i }));

    // The confirm phase lists the affected tools per server.
    expect(screen.getByText(/delete_issue, delete_repo/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: /apply & reload/i }));

    await waitFor(() =>
      expect(batchSpy).toHaveBeenCalledWith([{ name: 'github', tools: ['create_issue'] }]),
    );
  });

  it('disables the disable-unused action until a usage snapshot has loaded', () => {
    renderPanel({ usage: null, fetchedAt: null });
    expect(screen.getByRole('button', { name: /disable unused/i })).toBeDisabled();
    expect(screen.getByText(/needs the usage snapshot/i)).toBeInTheDocument();
  });

  it('scopes an action to a hand-picked subset of servers', () => {
    renderPanel();
    fireEvent.click(screen.getByRole('button', { name: /selected servers…/i }));
    fireEvent.click(screen.getByRole('button', { name: /disable unused/i }));

    // Nothing picked yet → empty plan.
    expect(screen.getByText(/No exposed tools are unused/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole('checkbox', { name: 'github' }));
    expect(screen.getByText(/Disables/)).toHaveTextContent('across 1 server');
  });

  it('surfaces a batch error in the summary', async () => {
    vi.spyOn(api, 'setServerToolsBatch').mockRejectedValue(
      new api.SetServerToolsError('stack_modified', 'File changed', 'Reload and retry.', 409),
    );
    renderPanel();
    fireEvent.click(screen.getByRole('button', { name: /hide matching pattern/i }));
    fireEvent.change(screen.getByLabelText(/glob pattern/i), { target: { value: 'delete_*' } });
    fireEvent.click(screen.getByRole('button', { name: /review & apply/i }));
    fireEvent.click(screen.getByRole('button', { name: /apply & reload/i }));

    expect(await screen.findByRole('alert')).toHaveTextContent(/File changed/);
  });
});
