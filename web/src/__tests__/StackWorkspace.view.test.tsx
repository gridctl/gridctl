import { beforeEach, describe, expect, it, vi } from 'vitest';
import '@testing-library/jest-dom';
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { StackWorkspace } from '../components/workspaces/StackWorkspace';
import { useStackStore } from '../stores/useStackStore';
import { useUIStore } from '../stores/useUIStore';
import { useAccessLensStore } from '../stores/useAccessLensStore';
import type { MCPServerStatus } from '../types';

vi.mock('../components/graph/Canvas', () => ({ Canvas: () => <div data-testid="canvas" /> }));
vi.mock('../components/layout/Sidebar', () => ({ Sidebar: () => <div data-testid="sidebar" /> }));
vi.mock('../components/stack/AccessLens', () => ({ AccessLens: () => <div data-testid="access-lens" /> }));
vi.mock('../components/spec/SpecPane', () => ({ SpecPane: () => null }));
vi.mock('../hooks/usePolling', () => ({
  usePolling: () => ({ refresh: async () => {} }),
  usePollingRefresh: () => async () => {},
}));

function servers(count: number): MCPServerStatus[] {
  return Array.from({ length: count }, (_, i) => ({
    name: `srv-${String(i).padStart(2, '0')}`,
    transport: 'http',
    initialized: true,
    toolCount: 1,
    tools: ['t'],
  }));
}

function loadStatus(count: number) {
  act(() => {
    useStackStore.getState().setGatewayStatus({
      gateway: { name: 'gw', version: '0.0.0' },
      'mcp-servers': servers(count),
    });
  });
}

function renderWorkspace() {
  return render(
    <MemoryRouter>
      <StackWorkspace />
    </MemoryRouter>,
  );
}

const showsTable = () => screen.queryByRole('table', { name: 'Stack contents' }) !== null;

beforeEach(() => {
  cleanup();
  useStackStore.setState({
    gatewayInfo: null,
    mcpServers: [],
    nodes: [],
    edges: [],
    selectedNodeId: null,
    lastUpdated: null,
    isLoading: false,
    error: null,
  });
  useUIStore.setState({ sidebarOpen: false, stackPrefs: { view: null } });
  useAccessLensStore.setState({ enabled: false });
});

describe('StackWorkspace view', () => {
  it('shows the toolbar and the graph by default with few servers', () => {
    loadStatus(3);
    renderWorkspace();
    expect(screen.getByRole('textbox', { name: 'Filter stack by name' })).toBeInTheDocument();
    expect(screen.getByTestId('canvas')).toBeInTheDocument();
    expect(showsTable()).toBe(false);
  });

  it('opens on the table when the first status has more than 24 servers', () => {
    renderWorkspace();
    expect(screen.getByTestId('canvas')).toBeInTheDocument();
    loadStatus(25);
    expect(showsTable()).toBe(true);
    expect(screen.queryByTestId('canvas')).not.toBeInTheDocument();
    expect(screen.queryByTestId('access-lens')).not.toBeInTheDocument();
    expect(useUIStore.getState().stackPrefs.view).toBeNull();
  });

  it('stays on the graph at exactly 24 servers', () => {
    loadStatus(24);
    renderWorkspace();
    expect(showsTable()).toBe(false);
  });

  it('does not flip the automatic choice when the count later changes', () => {
    loadStatus(30);
    renderWorkspace();
    expect(showsTable()).toBe(true);
    loadStatus(2);
    expect(showsTable()).toBe(true);
    cleanup();

    loadStatus(2);
    renderWorkspace();
    expect(showsTable()).toBe(false);
    loadStatus(30);
    expect(showsTable()).toBe(false);
  });

  it('lets a stored preference win over the automatic choice in both directions', () => {
    useUIStore.setState({ stackPrefs: { view: 'graph' } });
    loadStatus(30);
    renderWorkspace();
    expect(showsTable()).toBe(false);
    cleanup();

    useUIStore.setState({ stackPrefs: { view: 'table' } });
    loadStatus(2);
    renderWorkspace();
    expect(showsTable()).toBe(true);
  });

  it('shows the graph while Access Lens is enabled and returns to the table after', () => {
    useUIStore.setState({ stackPrefs: { view: 'table' } });
    useAccessLensStore.setState({ enabled: true });
    loadStatus(2);
    renderWorkspace();
    expect(screen.getByTestId('canvas')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Table view' })).toBeDisabled();
    act(() => useAccessLensStore.setState({ enabled: false }));
    expect(showsTable()).toBe(true);
  });

  it('remounts the canvas after switching back and keeps the selection', () => {
    loadStatus(3);
    renderWorkspace();
    fireEvent.click(screen.getByRole('button', { name: 'Table view' }));
    expect(screen.queryByTestId('canvas')).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'srv-01' }));
    expect(useStackStore.getState().selectedNodeId).toBe('mcp-srv-01');
    expect(screen.getByTestId('sidebar')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Graph view' }));
    expect(screen.getByTestId('canvas')).toBeInTheDocument();
    expect(useStackStore.getState().selectedNodeId).toBe('mcp-srv-01');
    expect(useUIStore.getState().sidebarOpen).toBe(true);
  });

  it('narrows the table from the toolbar filter and clears it from the empty state', () => {
    useUIStore.setState({ stackPrefs: { view: 'table' } });
    loadStatus(3);
    renderWorkspace();
    const input = screen.getByRole('textbox', { name: 'Filter stack by name' });
    fireEvent.change(input, { target: { value: 'srv-02' } });
    expect(screen.getByRole('status')).toHaveTextContent('1 of 3');
    fireEvent.change(input, { target: { value: 'none' } });
    fireEvent.click(screen.getByRole('button', { name: 'Clear filters' }));
    expect(input).toHaveValue('');
    expect(screen.getAllByRole('row')).toHaveLength(4);
  });

  it('moves focus from the filter to the table on ArrowDown', () => {
    useUIStore.setState({ stackPrefs: { view: 'table' } });
    loadStatus(3);
    renderWorkspace();
    fireEvent.keyDown(screen.getByRole('textbox', { name: 'Filter stack by name' }), { key: 'ArrowDown' });
    expect(document.activeElement).toHaveTextContent('srv-00');
  });
});
