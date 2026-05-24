import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import '@testing-library/jest-dom';
import { FleetActions } from '../components/workspaces/FleetActions';
import * as api from '../lib/api';
import type { MCPServerStatus } from '../types';

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

function renderPanel() {
  return render(
    <FleetActions isOpen onClose={vi.fn()} servers={servers} activeServerName="github" />,
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
