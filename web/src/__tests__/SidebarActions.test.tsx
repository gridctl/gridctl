import { describe, expect, it, vi, beforeEach } from 'vitest';
import '@testing-library/jest-dom';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import type { Node } from '@xyflow/react';

vi.mock('../hooks/useWindowManager', () => ({
  useWindowManager: () => ({ openDetachedWindow: vi.fn() }),
}));

vi.mock('../hooks/usePolicy', () => ({
  usePolicy: () => ({ report: null, error: null }),
}));

const restartMCPServer = vi.fn();
const patchServerEnabled = vi.fn();

vi.mock('../lib/api', async (importActual) => {
  const actual = await importActual<typeof import('../lib/api')>();
  return {
    ...actual,
    // Left pending: the evidence section is not under test here.
    fetchSecurityReport: vi.fn(() => new Promise(() => {})),
    restartMCPServer: (...args: unknown[]) => restartMCPServer(...args),
    patchServerEnabled: (...args: unknown[]) => patchServerEnabled(...args),
  };
});

import { Sidebar } from '../components/layout/Sidebar';
import { useStackStore } from '../stores/useStackStore';
import { resetRestartsInFlight } from '../lib/restartsInFlight';

function serverNode(name: string, overrides: Record<string, unknown> = {}): Node {
  return {
    id: `mcp-${name}`,
    position: { x: 0, y: 0 },
    data: {
      type: 'mcp-server',
      name,
      transport: 'stdio',
      initialized: true,
      toolCount: 0,
      tools: [],
      status: 'running',
      enabled: true,
      ...overrides,
    },
  };
}

const resourceNode: Node = {
  id: 'resource-db',
  position: { x: 0, y: 0 },
  data: { type: 'resource', name: 'db', image: 'postgres', status: 'running' },
};

function show(selectedNodeId: string, nodes: Node[]) {
  useStackStore.setState({ selectedNodeId, nodes, mcpServers: [], sessions: 0 });
  return render(
    <MemoryRouter>
      <Sidebar />
    </MemoryRouter>,
  );
}

// Availability is collapsed by default and mounts its content on first open.
function openAvailability() {
  fireEvent.click(screen.getByRole('button', { name: /Availability/ }));
}

function select(id: string) {
  act(() => useStackStore.setState({ selectedNodeId: id }));
}

describe('inspector actions', () => {
  beforeEach(() => {
    restartMCPServer.mockReset();
    patchServerEnabled.mockReset();
    resetRestartsInFlight();
  });

  it('offers no Restart for a resource, and View Logs carries no top margin', () => {
    show('resource-db', [resourceNode]);
    expect(screen.queryByRole('button', { name: 'Restart' })).not.toBeInTheDocument();
    const logs = screen.getByRole('button', { name: /View Logs/ });
    expect(logs.className.split(/\s+/)).not.toContain('mt-3');
  });

  it('offers no Restart for a disabled server and reads disabled', () => {
    show('mcp-parked', [
      serverNode('parked', { enabled: false, initialized: false, status: 'disabled' }),
    ]);
    expect(screen.queryByRole('button', { name: 'Restart' })).not.toBeInTheDocument();
    openAvailability();
    expect(screen.getByRole('button', { name: 'Enable' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /View Logs/ })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Variables/ })).toBeInTheDocument();
    const stateRow = screen.getByText('State').parentElement as HTMLElement;
    expect(within(stateRow).getByText('disabled')).toBeInTheDocument();
  });

  it('offers Restart for an enabled server', () => {
    show('mcp-a', [serverNode('a')]);
    expect(screen.getByRole('button', { name: 'Restart' })).toBeEnabled();
  });

  it('does not carry a failed restart to the next server', async () => {
    restartMCPServer.mockRejectedValue(new Error('restart failed for a'));
    show('mcp-a', [serverNode('a'), serverNode('b')]);
    fireEvent.click(screen.getByRole('button', { name: 'Restart' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('restart failed for a');

    select('mcp-b');
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Restart' })).toBeEnabled();
  });

  it('keeps an in-flight restart with its server across selections', async () => {
    restartMCPServer.mockReturnValue(new Promise(() => {}));
    show('mcp-a', [serverNode('a'), serverNode('b')]);
    fireEvent.click(screen.getByRole('button', { name: 'Restart' }));
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Restarting...' })).toBeDisabled(),
    );

    select('mcp-b');
    expect(screen.getByRole('button', { name: 'Restart' })).toBeEnabled();

    select('mcp-a');
    const button = screen.getByRole('button', { name: 'Restarting...' });
    expect(button).toBeDisabled();
    fireEvent.click(button);
    expect(restartMCPServer).toHaveBeenCalledTimes(1);
  });

  it('does not carry an Enable/Disable hint to the next server', async () => {
    patchServerEnabled.mockRejectedValue(new Error('toggle failed for a'));
    show('mcp-a', [serverNode('a'), serverNode('b')]);
    openAvailability();
    fireEvent.click(screen.getByRole('button', { name: 'Disable' }));
    await waitFor(() => expect(screen.getByText('toggle failed for a')).toBeInTheDocument());

    select('mcp-b');
    expect(screen.queryByText('toggle failed for a')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Disable' })).toBeEnabled();
  });
});
