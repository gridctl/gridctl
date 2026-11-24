import { beforeEach, describe, expect, it, vi } from 'vitest';
import '@testing-library/jest-dom';
import { fireEvent, render, screen, within } from '@testing-library/react';
import { MemoryRouter, useLocation } from 'react-router';
import type { Node } from '@xyflow/react';

const { openDetachedWindow } = vi.hoisted(() => ({ openDetachedWindow: vi.fn() }));

// useWindowManager opens a BroadcastChannel, which jsdom does not provide.
vi.mock('../hooks/useWindowManager', () => ({
  useWindowManager: () => ({ openDetachedWindow, focusDetachedWindow: vi.fn() }),
}));

vi.mock('../lib/api', async (importActual) => {
  const actual = await importActual<typeof import('../lib/api')>();
  return {
    ...actual,
    fetchSecurityReport: vi.fn().mockResolvedValue({ checks: [] }),
  };
});

import { Sidebar } from '../components/layout/Sidebar';
import { useStackStore } from '../stores/useStackStore';
import { useUIStore } from '../stores/useUIStore';
import { useInspectorToolsGuardStore } from '../stores/useInspectorToolsGuardStore';

const serverNode: Node = {
  id: 'mcp-vault',
  position: { x: 0, y: 0 },
  data: {
    type: 'mcp-server',
    name: 'vault',
    transport: 'stdio',
    initialized: true,
    toolCount: 2,
    tools: ['list_paths', 'read_secret'],
    toolWhitelist: [],
    status: 'running',
  },
};

function LocationProbe() {
  const loc = useLocation();
  return <div data-testid="loc">{loc.pathname + loc.search}</div>;
}

function renderDirty() {
  render(
    <MemoryRouter initialEntries={['/stack']}>
      <Sidebar />
      <LocationProbe />
    </MemoryRouter>,
  );
  fireEvent.click(screen.getByRole('button', { name: /^Tools/ }));
  fireEvent.click(screen.getByRole('option', { name: 'read_secret' }));
  expect(screen.getByRole('option', { name: 'read_secret' })).toHaveAttribute(
    'aria-checked',
    'false',
  );
}

describe('inspector leave guard for the tools editor', () => {
  beforeEach(() => {
    openDetachedWindow.mockReset();
    useInspectorToolsGuardStore.setState({ dirtyServer: null, pendingLeave: null });
    useUIStore.setState({ sidebarOpen: true, sidebarDetached: false });
    useStackStore.setState({
      selectedNodeId: 'mcp-vault',
      nodes: [serverNode],
      mcpServers: [],
      toolCatalog: [],
    });
  });

  it('asks before the close button discards a dirty draft', () => {
    renderDirty();

    fireEvent.click(screen.getByRole('button', { name: 'Close inspector' }));
    const dialog = screen.getByRole('alertdialog');
    expect(dialog).toHaveTextContent('Discard unsaved changes to vault?');

    fireEvent.click(within(dialog).getByRole('button', { name: 'Keep editing' }));
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(useStackStore.getState().selectedNodeId).toBe('mcp-vault');
    expect(useUIStore.getState().sidebarOpen).toBe(true);
    expect(screen.getByRole('option', { name: 'read_secret' })).toHaveAttribute(
      'aria-checked',
      'false',
    );

    fireEvent.click(screen.getByRole('button', { name: 'Close inspector' }));
    fireEvent.click(within(screen.getByRole('alertdialog')).getByRole('button', { name: 'Discard' }));
    expect(useStackStore.getState().selectedNodeId).toBeNull();
    expect(useUIStore.getState().sidebarOpen).toBe(false);
    expect(useInspectorToolsGuardStore.getState().dirtyServer).toBeNull();
  });

  it('asks before popping out, then opens the popout on Discard', () => {
    renderDirty();
    fireEvent.click(screen.getByRole('button', { name: 'Open in separate window' }));
    expect(screen.getByRole('alertdialog')).toBeInTheDocument();
    expect(openDetachedWindow).not.toHaveBeenCalled();

    fireEvent.click(within(screen.getByRole('alertdialog')).getByRole('button', { name: 'Discard' }));
    expect(openDetachedWindow).toHaveBeenCalledWith('sidebar', 'node=vault');
  });

  it.each([
    ['View Logs', '/logs?source=vault'],
    ['Variables', '/vault?filter=server:vault'],
    ['Open in Tools workspace', '/tools?server=vault'],
  ])('asks before %s navigates away', (label, target) => {
    renderDirty();
    fireEvent.click(screen.getByRole('button', { name: new RegExp(label) }));
    expect(screen.getByRole('alertdialog')).toBeInTheDocument();
    expect(screen.getByTestId('loc')).toHaveTextContent('/stack');

    fireEvent.click(within(screen.getByRole('alertdialog')).getByRole('button', { name: 'Keep editing' }));
    expect(screen.getByTestId('loc')).toHaveTextContent('/stack');

    fireEvent.click(screen.getByRole('button', { name: new RegExp(label) }));
    fireEvent.click(within(screen.getByRole('alertdialog')).getByRole('button', { name: 'Discard' }));
    expect(decodeURIComponent(screen.getByTestId('loc').textContent ?? '')).toBe(target);
  });

  it('closes without a prompt when the draft is clean', () => {
    render(
      <MemoryRouter>
        <Sidebar />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole('button', { name: /^Tools/ }));
    fireEvent.click(screen.getByRole('button', { name: 'Close inspector' }));
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(useStackStore.getState().selectedNodeId).toBeNull();
  });
});
