import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import '@testing-library/jest-dom';
import { act, render, fireEvent, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router';
import { AppShell } from '../components/shell/AppShell';
import { AuthPendingBadge } from '../components/sidebar/AuthPendingBadge';
import { openServerInspector, useStackStore } from '../stores/useStackStore';
import { useUIStore } from '../stores/useUIStore';
import { useShellNavStore } from '../stores/useShellNavStore';
import { leavesWorkspace, useNavigationGuardStore } from '../stores/useNavigationGuardStore';
import { useInspectorToolsGuardStore } from '../stores/useInspectorToolsGuardStore';
import { showToast } from '../components/ui/Toast';
import type { GatewayStatus, MCPServerStatus } from '../types';

vi.mock('../components/layout/Header', () => ({ Header: () => null }));
vi.mock('../components/layout/StatusBar', () => ({ StatusBar: () => null }));
vi.mock('../components/palette/CommandPalette', () => ({ CommandPalette: () => null }));
vi.mock('../components/ui/Toast', () => ({ ToastContainer: () => null, showToast: vi.fn() }));
vi.mock('../hooks/usePolling', () => ({ usePolling: () => ({ refresh: async () => {} }) }));
vi.mock('../hooks/useGlobalCommands', () => ({ useGlobalCommands: () => {} }));

function server(name: string, authStatus?: MCPServerStatus['authStatus']): MCPServerStatus {
  return { name, transport: 'http', initialized: true, toolCount: 0, tools: [], external: true, authStatus };
}

function LocationProbe() {
  const location = useLocation();
  return <div data-testid="location">{location.pathname}</div>;
}

function renderShell(path: string) {
  render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="*" element={<AppShell />}>
          <Route path="*" element={<LocationProbe />} />
        </Route>
      </Routes>
    </MemoryRouter>,
  );
}

function dirtyTools() {
  useNavigationGuardStore.getState().register({
    id: 'tools',
    label: 'Tools for notion',
    blocks: (to) => leavesWorkspace(to, 'tools'),
  });
}

function reset() {
  useStackStore.setState({
    selectedNodeId: null,
    missingSelection: null,
    nodes: [],
    gatewayInfo: null,
    mcpServers: [server('notion', 'needs_auth')],
  });
  useUIStore.setState({ sidebarOpen: false });
  useShellNavStore.getState().clear();
  useNavigationGuardStore.setState({ guards: [], hosts: 0, pending: null });
  useInspectorToolsGuardStore.setState({ dirtyServer: null, pendingLeave: null });
  vi.mocked(showToast).mockClear();
}

describe('openServerInspector', () => {
  beforeEach(reset);
  afterEach(reset);

  it('lands on Stack with the server selected and the inspector open', () => {
    renderShell('/logs');
    act(() => openServerInspector('notion'));
    expect(screen.getByTestId('location')).toHaveTextContent('/stack');
    expect(useStackStore.getState().selectedNodeId).toBe('mcp-notion');
    expect(useUIStore.getState().sidebarOpen).toBe(true);
  });

  // An unconditional guard turns any navigation attempt into a visible
  // confirm, so its absence proves goToStack did not navigate.
  it('does not navigate when already on Stack', () => {
    renderShell('/stack');
    useNavigationGuardStore.getState().register({ id: 'any', label: 'a draft' });
    act(() => openServerInspector('notion'));
    expect(screen.getByTestId('location')).toHaveTextContent('/stack');
    expect(screen.queryByText('Discard unsaved changes')).not.toBeInTheDocument();
    expect(useStackStore.getState().selectedNodeId).toBe('mcp-notion');
    expect(useUIStore.getState().sidebarOpen).toBe(true);
  });

  it('reads the current route, not the route at mount', () => {
    renderShell('/logs');
    act(() => openServerInspector('notion'));
    expect(screen.getByTestId('location')).toHaveTextContent('/stack');
    useNavigationGuardStore.getState().register({ id: 'any', label: 'a draft' });
    act(() => openServerInspector('notion'));
    expect(screen.queryByText('Discard unsaved changes')).not.toBeInTheDocument();
  });

  it('raises the discard confirm from Tools with unsaved changes', () => {
    renderShell('/tools');
    dirtyTools();
    act(() => openServerInspector('notion'));
    expect(screen.getByTestId('location')).toHaveTextContent('/tools');
    expect(screen.getByText('Discard unsaved changes')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Discard & leave' }));
    expect(screen.getByTestId('location')).toHaveTextContent('/stack');
    expect(useStackStore.getState().selectedNodeId).toBe('mcp-notion');
  });

  it('selects nothing and warns for a server that left the stack', () => {
    renderShell('/logs');
    act(() => openServerInspector('ghost'));
    expect(screen.getByTestId('location')).toHaveTextContent('/logs');
    expect(useStackStore.getState().selectedNodeId).toBeNull();
    expect(useUIStore.getState().sidebarOpen).toBe(false);
    expect(showToast).toHaveBeenCalledWith('warning', 'ghost is no longer in the stack');
  });

  it('selects and opens without navigating before the shell registers', () => {
    openServerInspector('notion');
    expect(useStackStore.getState().selectedNodeId).toBe('mcp-notion');
    expect(useUIStore.getState().sidebarOpen).toBe(true);
  });

  it('stops registering navigation once the shell unmounts', () => {
    const { unmount } = render(
      <MemoryRouter initialEntries={['/logs']}>
        <Routes>
          <Route path="*" element={<AppShell />} />
        </Routes>
      </MemoryRouter>,
    );
    const registered = useShellNavStore.getState().goToStack;
    unmount();
    expect(useShellNavStore.getState().goToStack).not.toBe(registered);
  });
});

describe('auth entry points from another workspace', () => {
  beforeEach(reset);
  afterEach(reset);

  function renderShellWithBadge(path: string) {
    render(
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="*" element={<AppShell />}>
            <Route
              path="*"
              element={
                <>
                  <AuthPendingBadge />
                  <LocationProbe />
                </>
              }
            />
          </Route>
        </Routes>
      </MemoryRouter>,
    );
  }

  it('the pending chip opens the first pending server on Stack', () => {
    useStackStore.setState({ mcpServers: [server('github', 'authorized'), server('notion', 'needs_auth')] });
    renderShellWithBadge('/logs');
    fireEvent.click(screen.getByRole('button', { name: /Authorization: 1 pending/ }));
    expect(screen.getByTestId('location')).toHaveTextContent('/stack');
    expect(useStackStore.getState().selectedNodeId).toBe('mcp-notion');
    expect(useUIStore.getState().sidebarOpen).toBe(true);
  });

  it('the pending chip raises the discard confirm from dirty Tools', () => {
    renderShellWithBadge('/tools');
    dirtyTools();
    fireEvent.click(screen.getByRole('button', { name: /Authorization: 1 pending/ }));
    expect(screen.getByTestId('location')).toHaveTextContent('/tools');
    expect(screen.getByText('Discard unsaved changes')).toBeInTheDocument();
  });

  function captureAuthorizeToast(): () => void {
    const status = (authStatus: 'authorized' | 'needs_auth'): GatewayStatus => ({
      gateway: { name: 'g', version: '1' },
      'mcp-servers': [server('notion', authStatus)],
    });
    act(() => {
      useStackStore.getState().setGatewayStatus(status('authorized'));
      useStackStore.getState().setGatewayStatus(status('needs_auth'));
    });
    const call = vi.mocked(showToast).mock.calls.find((c) => c[1] === 'notion requires authorization');
    const onClick = call?.[2]?.action?.onClick;
    if (!onClick) throw new Error('no Authorize action');
    return onClick;
  }

  it('the toast Authorize action opens the server on Stack', () => {
    renderShell('/logs');
    const authorize = captureAuthorizeToast();
    act(() => authorize());
    expect(screen.getByTestId('location')).toHaveTextContent('/stack');
    expect(useStackStore.getState().selectedNodeId).toBe('mcp-notion');
    expect(useUIStore.getState().sidebarOpen).toBe(true);
  });

  it('the toast Authorize action raises the discard confirm from dirty Tools', () => {
    renderShell('/tools');
    dirtyTools();
    const authorize = captureAuthorizeToast();
    act(() => authorize());
    expect(screen.getByTestId('location')).toHaveTextContent('/tools');
    expect(screen.getByText('Discard unsaved changes')).toBeInTheDocument();
  });

  it('a stale toast Authorize action selects nothing', () => {
    renderShell('/logs');
    const authorize = captureAuthorizeToast();
    act(() => useStackStore.getState().setGatewayStatus({ gateway: { name: 'g', version: '1' }, 'mcp-servers': [] }));
    act(() => authorize());
    expect(screen.getByTestId('location')).toHaveTextContent('/logs');
    expect(useStackStore.getState().selectedNodeId).toBeNull();
    expect(showToast).toHaveBeenCalledWith('warning', 'notion is no longer in the stack');
  });
});
