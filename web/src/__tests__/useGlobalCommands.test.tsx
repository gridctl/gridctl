import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import '@testing-library/jest-dom';
import { act, fireEvent, render, renderHook, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, useLocation } from 'react-router';
import { ReactFlowProvider } from '@xyflow/react';
import type { ReactNode } from 'react';
import { CommandRegistryProvider, useCommandRegistry } from '../hooks/useCommandRegistry';
import { useGlobalCommands } from '../hooks/useGlobalCommands';
import { useStackStore } from '../stores/useStackStore';
import { useUIStore } from '../stores/useUIStore';
import { useAuthStore } from '../stores/useAuthStore';
import { useInspectorToolsGuardStore } from '../stores/useInspectorToolsGuardStore';
import { leavesWorkspace, useNavigationGuardStore } from '../stores/useNavigationGuardStore';
import { NavigationGuardDialog } from '../components/shell/NavigationGuardDialog';
import type { ClientStatus, GatewayStatus, MCPServerStatus } from '../types';

function wrapper({ children }: { children: ReactNode }) {
  return (
    <MemoryRouter>
      <ReactFlowProvider>
        <CommandRegistryProvider>{children}</CommandRegistryProvider>
      </ReactFlowProvider>
    </MemoryRouter>
  );
}

function useProbe() {
  useGlobalCommands();
  return useCommandRegistry();
}

describe('useGlobalCommands inspect entry', () => {
  beforeEach(() => {
    const server: MCPServerStatus = {
      name: 'github',
      transport: 'http',
      initialized: true,
      toolCount: 1,
      tools: ['search'],
    };
    const status: GatewayStatus = {
      gateway: { name: 'gw', version: '0.0.0' },
      'mcp-servers': [server],
      resources: [{ name: 'postgres', image: 'postgres:16', status: 'running' }],
    };
    useStackStore.getState().setGatewayStatus(status);
    useStackStore.setState({ selectedNodeId: null });
    useUIStore.setState({ sidebarOpen: false, inspectorFocusRequest: null });
    const clients: ClientStatus[] = [
      { name: 'Claude', slug: 'claude', detected: true, linked: true, transport: 'http' },
      { name: 'Cursor', slug: 'cursor', detected: true, linked: false, transport: 'http' },
    ];
    useStackStore.getState().setClients(clients);
    useAuthStore.setState({ credential: null });
    useInspectorToolsGuardStore.setState({ dirtyServer: null, pendingLeave: null });
  });

  it('holds Inspect behind a dirty inspector tool draft on another server', async () => {
    useStackStore.setState({ selectedNodeId: 'resource-postgres' });
    useUIStore.setState({ sidebarOpen: true });
    useInspectorToolsGuardStore.setState({ dirtyServer: 'files', pendingLeave: null });
    const { result } = renderHook(() => useProbe(), { wrapper });
    await waitFor(() => {
      expect(result.current.commands.some((command) => command.id === 'node:github')).toBe(true);
    });
    result.current.commands.find((item) => item.id === 'node:github')?.onSelect();
    expect(useStackStore.getState().selectedNodeId).toBe('resource-postgres');
    expect(useInspectorToolsGuardStore.getState().pendingLeave).not.toBeNull();

    // Inspecting the draft's own server passes straight through.
    useInspectorToolsGuardStore.setState({ dirtyServer: 'github', pendingLeave: null });
    result.current.commands.find((item) => item.id === 'node:github')?.onSelect();
    expect(useInspectorToolsGuardStore.getState().pendingLeave).toBeNull();
    expect(useStackStore.getState().selectedNodeId).toBe('mcp-github');
  });

  it('registers Forget gateway credential only while a credential is held', async () => {
    const { result, rerender } = renderHook(() => useProbe(), { wrapper });
    await waitFor(() => {
      expect(result.current.commands.some((command) => command.id === 'action:wizard')).toBe(true);
    });
    expect(result.current.commands.some((command) => command.id === 'auth:forget-credential')).toBe(false);
    act(() => {
      useAuthStore.setState({ credential: { mode: 'bearer', header: 'Authorization', token: 'held' } });
    });
    rerender();
    await waitFor(() => {
      expect(result.current.commands.some((command) => command.id === 'auth:forget-credential')).toBe(true);
    });
    const command = result.current.commands.find((item) => item.id === 'auth:forget-credential');
    expect(command?.label).toBe('Forget gateway credential…');
    expect(command?.section).toBe('global');
    expect(command?.keywords).toEqual(['auth', 'credential', 'token', 'sign out', 'logout', 'forget']);
    useUIStore.setState({ forgetCredentialOpen: false });
    act(() => {
      command?.onSelect();
    });
    expect(useUIStore.getState().forgetCredentialOpen).toBe(true);
    expect(useAuthStore.getState().credential).not.toBeNull();
    useUIStore.setState({ forgetCredentialOpen: false });
  });

  it('selects the mcp node id and opens the sidebar', async () => {
    const { result } = renderHook(() => useProbe(), { wrapper });
    await waitFor(() => {
      expect(result.current.commands.some((command) => command.id === 'node:github')).toBe(true);
    });
    const command = result.current.commands.find((item) => item.id === 'node:github');
    expect(command?.label).toBe('Inspect github');
    expect(command?.keywords).toContain('inspect');
    command?.onSelect();
    expect(useStackStore.getState().selectedNodeId).toBe('mcp-github');
    expect(useUIStore.getState().sidebarOpen).toBe(true);
    expect(useUIStore.getState().inspectorFocusRequest).toBe('mcp-github');
  });

  it('inspects a linked client and a resource and requests inspector focus', async () => {
    const { result } = renderHook(() => useProbe(), { wrapper });
    await waitFor(() => {
      expect(result.current.commands.some((command) => command.id === 'node:client:claude')).toBe(true);
    });
    const client = result.current.commands.find((item) => item.id === 'node:client:claude');
    expect(client?.label).toBe('Inspect Claude');
    client?.onSelect();
    expect(useStackStore.getState().selectedNodeId).toBe('client-claude');
    expect(useUIStore.getState().sidebarOpen).toBe(true);
    expect(useUIStore.getState().inspectorFocusRequest).toBe('client-claude');

    useStackStore.setState({ selectedNodeId: null });
    useUIStore.setState({ sidebarOpen: false, inspectorFocusRequest: null });
    const resource = result.current.commands.find((item) => item.id === 'node:resource:postgres');
    expect(resource?.label).toBe('Inspect postgres');
    resource?.onSelect();
    expect(useStackStore.getState().selectedNodeId).toBe('resource-postgres');
    expect(useUIStore.getState().sidebarOpen).toBe(true);
    expect(useUIStore.getState().inspectorFocusRequest).toBe('resource-postgres');
  });

  it('registers no Inspect command for a detected but unlinked client', async () => {
    const { result } = renderHook(() => useProbe(), { wrapper });
    await waitFor(() => {
      expect(result.current.commands.some((command) => command.id === 'node:client:claude')).toBe(true);
    });
    expect(result.current.commands.some((command) => command.id === 'node:client:cursor')).toBe(false);
    expect(result.current.commands.some((command) => command.label === 'Inspect Cursor')).toBe(false);
  });
});

function LocationProbe() {
  const location = useLocation();
  return <div data-testid="location">{location.pathname + location.search}</div>;
}

function CommandsHarness({ onReady }: { onReady: (registry: ReturnType<typeof useCommandRegistry>) => void }) {
  const registry = useProbe();
  onReady(registry);
  return null;
}

describe('useGlobalCommands navigation guard', () => {
  let registry: ReturnType<typeof useCommandRegistry> | null = null;

  function renderGuarded(path = '/tools') {
    render(
      <MemoryRouter initialEntries={[path]}>
        <ReactFlowProvider>
          <CommandRegistryProvider>
            <CommandsHarness onReady={(r) => { registry = r; }} />
            <LocationProbe />
            <NavigationGuardDialog />
          </CommandRegistryProvider>
        </ReactFlowProvider>
      </MemoryRouter>,
    );
  }

  function select(id: string) {
    const command = registry?.commands.find((item) => item.id === id);
    expect(command).toBeDefined();
    act(() => {
      command?.onSelect();
    });
  }

  function registerDirty() {
    useNavigationGuardStore.getState().register({ id: 'tools', label: 'Tools for github' });
  }

  beforeEach(() => {
    registry = null;
    useAuthStore.setState({ credential: null });
    useUIStore.setState({ resetDialogOpen: false });
    useNavigationGuardStore.setState({ guards: [], hosts: 0, pending: null });
  });

  afterEach(() => {
    useNavigationGuardStore.setState({ guards: [], hosts: 0, pending: null });
    useUIStore.setState({ resetDialogOpen: false });
  });

  it('holds Open Logs behind the discard confirm and lands on /logs when confirmed', async () => {
    renderGuarded();
    await waitFor(() => expect(registry?.commands.some((c) => c.id === 'navigate:logs')).toBe(true));
    registerDirty();
    select('navigate:logs');
    expect(screen.getByTestId('location')).toHaveTextContent('/tools');
    expect(screen.getByText('Discard unsaved changes')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Discard & leave' }));
    expect(screen.getByTestId('location')).toHaveTextContent('/logs');
    expect(screen.queryByText('Discard unsaved changes')).not.toBeInTheDocument();
  });

  it('stays put when the confirm is canceled', async () => {
    renderGuarded();
    await waitFor(() => expect(registry?.commands.some((c) => c.id === 'navigate:logs')).toBe(true));
    registerDirty();
    select('navigate:logs');
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(screen.getByTestId('location')).toHaveTextContent('/tools');
    expect(screen.queryByText('Discard unsaved changes')).not.toBeInTheDocument();
    expect(useNavigationGuardStore.getState().guards).toHaveLength(1);
  });

  it('navigates with no dialog when nothing is dirty', async () => {
    renderGuarded();
    await waitFor(() => expect(registry?.commands.some((c) => c.id === 'navigate:logs')).toBe(true));
    select('navigate:logs');
    expect(screen.getByTestId('location')).toHaveTextContent('/logs');
    expect(screen.queryByText('Discard unsaved changes')).not.toBeInTheDocument();
  });

  it('opens the reset dialog only once the guarded navigation proceeds', async () => {
    renderGuarded();
    await waitFor(() => expect(registry?.commands.some((c) => c.id === 'system:reset-gridctl')).toBe(true));
    registerDirty();
    select('system:reset-gridctl');
    expect(useUIStore.getState().resetDialogOpen).toBe(false);
    expect(screen.getByTestId('location')).toHaveTextContent('/tools');
    fireEvent.click(screen.getByRole('button', { name: 'Discard & leave' }));
    expect(useUIStore.getState().resetDialogOpen).toBe(true);
    expect(screen.getByTestId('location')).toHaveTextContent('/connections');
  });

  it('lets Open Spec Editor through a guard that only blocks leaving /stack', async () => {
    renderGuarded('/stack');
    await waitFor(() => expect(registry?.commands.some((c) => c.id === 'navigate:spec')).toBe(true));
    useNavigationGuardStore.getState().register({
      id: 'lens',
      label: 'Access Lens',
      blocks: (to) => leavesWorkspace(to, 'stack'),
    });
    select('navigate:spec');
    expect(screen.getByTestId('location')).toHaveTextContent('/stack?spec=1');
    expect(screen.queryByText('Discard unsaved changes')).not.toBeInTheDocument();
    select('navigate:workspace-library');
    expect(screen.getByTestId('location')).toHaveTextContent('/stack?spec=1');
    expect(screen.getByText('Discard unsaved changes')).toBeInTheDocument();
  });
});
