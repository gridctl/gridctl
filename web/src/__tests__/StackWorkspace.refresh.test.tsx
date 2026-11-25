import { beforeEach, describe, expect, it, vi } from 'vitest';
import '@testing-library/jest-dom';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { StackWorkspace } from '../components/workspaces/StackWorkspace';
import { useStackStore } from '../stores/useStackStore';
import { useUIStore } from '../stores/useUIStore';

const { refreshMock } = vi.hoisted(() => ({ refreshMock: vi.fn(async () => {}) }));

vi.mock('../components/graph/Canvas', () => ({ Canvas: () => <div data-testid="canvas" /> }));
vi.mock('../components/layout/Sidebar', () => ({ Sidebar: () => <div data-testid="sidebar" /> }));
vi.mock('../components/stack/AccessLens', () => ({ AccessLens: () => <div data-testid="access-lens" /> }));
vi.mock('../components/spec/SpecPane', () => ({ SpecPane: () => <div data-testid="spec-pane" /> }));
vi.mock('../hooks/usePolling', () => ({
  usePolling: () => ({ refresh: refreshMock }),
  usePollingRefresh: () => refreshMock,
}));

const RECONNECT_HINT = 'The gateway may have been shut down or restarted';

function renderStack() {
  return render(
    <MemoryRouter initialEntries={['/stack?spec=1']}>
      <StackWorkspace />
    </MemoryRouter>,
  );
}

function inertAncestor(el: HTMLElement): HTMLElement | null {
  return el.closest('[inert]');
}

beforeEach(() => {
  cleanup();
  refreshMock.mockClear();
  useUIStore.setState({ sidebarOpen: false });
});

describe('StackWorkspace header', () => {
  it('renders a visible compact title above the canvas grid with server and tool counts', () => {
    useStackStore.setState({
      error: null,
      connectionStatus: 'connected',
      lastUpdated: new Date(),
      isLoading: false,
      mcpServers: [{ name: 'a' }, { name: 'b' }] as never,
      tools: [{ name: 't1' }] as never,
    });
    renderStack();
    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1);
    const h1 = screen.getByRole('heading', { level: 1, name: 'Stack' });
    expect(h1.className).not.toMatch(/sr-only|uppercase|tracking-/);
    const header = h1.closest('header')!;
    expect(header).toHaveClass('py-2');
    expect(header).toHaveTextContent('2 MCP servers · 1 tool');
    const grid = screen.getByTestId('canvas').closest('.grid') as HTMLElement;
    expect(grid).toHaveClass('flex-1', 'min-h-0');
    expect(header.nextElementSibling).toBe(grid);
    expect(header.contains(grid)).toBe(false);
  });

  it('uses singular forms and keeps the title alone before the first load', () => {
    useStackStore.setState({
      error: null,
      lastUpdated: new Date(),
      isLoading: false,
      mcpServers: [{ name: 'a' }] as never,
      tools: [] as never,
    });
    const { unmount } = renderStack();
    expect(screen.getByRole('heading', { level: 1, name: 'Stack' }).closest('header')).toHaveTextContent('1 MCP server · 0 tools');
    unmount();

    useStackStore.setState({ lastUpdated: null, isLoading: true, mcpServers: [], tools: [] });
    renderStack();
    expect(screen.getByRole('heading', { level: 1, name: 'Stack' })).toBeInTheDocument();
    expect(screen.queryByTestId('workspace-header-summary')).toBeNull();
  });
});

describe('StackWorkspace refresh states', () => {
  it('keeps the last-known graph under a banner when a refresh fails', () => {
    const lastUpdated = new Date(2026, 10, 23, 10, 15, 0);
    useStackStore.setState({
      error: 'Gateway connection unavailable',
      connectionStatus: 'disconnected',
      lastUpdated,
      isLoading: false,
    });
    renderStack();

    const alert = screen.getByRole('alert');
    expect(alert).toHaveTextContent('Gateway connection unavailable');
    expect(alert).toHaveTextContent(`Last updated ${lastUpdated.toLocaleTimeString()}`);
    expect(screen.queryByText('Stack unavailable')).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect(refreshMock).toHaveBeenCalledOnce();

    const canvas = screen.getByTestId('canvas');
    expect(inertAncestor(canvas)).toBeNull();
    expect(inertAncestor(screen.getByTestId('spec-pane'))).toBeNull();

    const dimmed = canvas.closest('.grayscale') as HTMLElement;
    expect(dimmed).not.toBeNull();
    expect(dimmed).toContainElement(canvas);
    expect(dimmed).not.toContainElement(screen.getByTestId('spec-pane'));
    expect(dimmed).not.toContainElement(screen.getByTestId('access-lens'));
  });

  it('does not desaturate the graph when it is current', () => {
    useStackStore.setState({ error: null, connectionStatus: 'connected', lastUpdated: new Date(), isLoading: false });
    renderStack();
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    expect(screen.getByTestId('canvas').closest('.grayscale')).toBeNull();
  });

  it('shows the failed state with the reconnect hint and an inert canvas on a first-load failure', () => {
    useStackStore.setState({
      error: 'Gateway connection unavailable',
      connectionStatus: 'disconnected',
      lastUpdated: null,
      isLoading: false,
    });
    renderStack();

    const alert = screen.getByRole('alert');
    expect(alert).toHaveTextContent('Stack unavailable');
    expect(alert).toHaveTextContent(RECONNECT_HINT);
    expect(inertAncestor(screen.getByTestId('canvas'))).not.toBeNull();
    expect(inertAncestor(screen.getByTestId('access-lens'))).not.toBeNull();
    expect(inertAncestor(screen.getByTestId('spec-pane'))).not.toBeNull();

    const retry = screen.getByRole('button', { name: 'Retry' });
    expect(inertAncestor(retry)).toBeNull();
    fireEvent.click(retry);
    expect(refreshMock).toHaveBeenCalledOnce();
  });

  it('picks the reconnect hint from the connection status, not the error text', () => {
    useStackStore.setState({
      error: 'Gateway connection unavailable',
      connectionStatus: 'error',
      lastUpdated: null,
      isLoading: false,
    });
    renderStack();
    const alert = screen.getByRole('alert');
    expect(alert).toHaveTextContent('Stack unavailable');
    expect(alert).not.toHaveTextContent(RECONNECT_HINT);
  });

  it('shows the loading overlay only before the first load', () => {
    useStackStore.setState({ error: null, connectionStatus: 'disconnected', lastUpdated: null, isLoading: true });
    const { unmount } = renderStack();
    expect(screen.getByText('Loading stack')).toBeInTheDocument();
    expect(inertAncestor(screen.getByTestId('canvas'))).not.toBeNull();
    unmount();

    useStackStore.setState({ error: null, connectionStatus: 'connected', lastUpdated: new Date(), isLoading: true });
    renderStack();
    expect(screen.queryByText('Loading stack')).not.toBeInTheDocument();
    expect(inertAncestor(screen.getByTestId('canvas'))).toBeNull();
  });

  it('shows the loading overlay for a pending cycle after a failed first load', () => {
    useStackStore.setState({
      error: 'Gateway connection unavailable',
      connectionStatus: 'error',
      lastUpdated: null,
      isLoading: true,
    });
    renderStack();
    expect(screen.getByText('Loading stack')).toBeInTheDocument();
    expect(screen.queryByText('Stack unavailable')).not.toBeInTheDocument();
    expect(inertAncestor(screen.getByTestId('canvas'))).not.toBeNull();
    expect(inertAncestor(screen.getByTestId('spec-pane'))).not.toBeNull();
  });
});
