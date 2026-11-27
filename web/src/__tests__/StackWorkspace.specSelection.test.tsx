import { beforeEach, describe, expect, it, vi } from 'vitest';
import '@testing-library/jest-dom';
import { act, cleanup, render, screen } from '@testing-library/react';
import type { ReactNode } from 'react';
import { BrowserRouter, MemoryRouter, useLocation } from 'react-router';
import type { Node } from '@xyflow/react';
import { StackWorkspace } from '../components/workspaces/StackWorkspace';
import { useUIStore } from '../stores/useUIStore';
import { useStackStore } from '../stores/useStackStore';
import { useAccessLensStore } from '../stores/useAccessLensStore';

// The real useNodeParamSync stays in place: selecting a node writes ?node= in
// the same tick that opening the inspector closes the spec.
vi.mock('../components/graph/Canvas', () => ({ Canvas: () => <div data-testid="canvas" /> }));
vi.mock('../components/layout/Sidebar', () => ({ Sidebar: () => <div data-testid="sidebar" /> }));
vi.mock('../components/stack/AccessLens', () => ({ AccessLens: () => null }));
vi.mock('../components/spec/SpecTab', () => ({ SpecTab: () => <div data-testid="spec-tab" /> }));
vi.mock('../hooks/usePolling', () => ({
  usePolling: () => ({ refresh: async () => {} }),
  usePollingRefresh: () => async () => {},
}));

function Location() {
  const location = useLocation();
  return <div data-testid="location">{location.search}</div>;
}

const routers: Array<[string, (search: string, children: ReactNode) => ReactNode]> = [
  ['MemoryRouter', (search, children) => <MemoryRouter initialEntries={[`/stack${search}`]}>{children}</MemoryRouter>],
  [
    'BrowserRouter',
    (search, children) => {
      window.history.replaceState(null, '', `/stack${search}`);
      return <BrowserRouter>{children}</BrowserRouter>;
    },
  ],
];

function inspect(id: string) {
  act(() => {
    useStackStore.getState().selectNode(id);
    useUIStore.getState().setSidebarOpen(true);
  });
}

function search(): string {
  return screen.getByTestId('location').textContent ?? '';
}

beforeEach(() => {
  cleanup();
  useUIStore.setState({ sidebarOpen: false });
  useAccessLensStore.setState({ slideOverOpen: false });
  useStackStore.setState({
    selectedNodeId: null,
    nodes: [{ id: 'mcp-github' }, { id: 'mcp-slack' }] as Node[],
  });
});

describe.each(routers)('StackWorkspace spec and node selection (%s)', (_, wrap) => {
  function renderAt(query: string) {
    return render(
      wrap(
        query,
        <>
          <StackWorkspace />
          <Location />
        </>,
      ),
    );
  }

  it('closes the spec and opens the inspector when a new node is selected', () => {
    renderAt('?spec=1');
    inspect('mcp-github');
    expect(new URLSearchParams(search()).get('node')).toBe('mcp-github');
    expect(search()).not.toContain('spec');
    expect(useUIStore.getState().sidebarOpen).toBe(true);
    expect(screen.queryByRole('complementary', { name: 'Stack spec' })).toBeNull();
    expect(screen.getByRole('complementary', { name: 'Stack inspector' })).toBeInTheDocument();
  });

  it('closes the spec when the already-selected node is inspected again', () => {
    useStackStore.setState({ selectedNodeId: 'mcp-github' });
    renderAt('?spec=1&node=mcp-github');
    expect(screen.getByRole('complementary', { name: 'Stack spec' })).toBeInTheDocument();
    inspect('mcp-github');
    expect(search()).toBe('?node=mcp-github');
    expect(screen.queryByRole('complementary', { name: 'Stack spec' })).toBeNull();
    expect(screen.getByRole('complementary', { name: 'Stack inspector' })).toBeInTheDocument();
  });

  it('lets a later node click open the inspector after the spec reopens', () => {
    renderAt('?spec=1');
    inspect('mcp-github');
    expect(screen.getByRole('complementary', { name: 'Stack inspector' })).toBeInTheDocument();
    cleanup();
    renderAt('?spec=1&node=mcp-github');
    expect(useUIStore.getState().sidebarOpen).toBe(false);
    inspect('mcp-slack');
    expect(search()).toBe('?node=mcp-slack');
    expect(screen.getByRole('complementary', { name: 'Stack inspector' })).toBeInTheDocument();
  });
});
