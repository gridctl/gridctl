import { describe, it, expect, beforeEach } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { ReactFlowProvider } from '@xyflow/react';
import type { ReactNode } from 'react';
import { CommandRegistryProvider, useCommandRegistry } from '../hooks/useCommandRegistry';
import { useGlobalCommands } from '../hooks/useGlobalCommands';
import { useStackStore } from '../stores/useStackStore';
import { useUIStore } from '../stores/useUIStore';
import { useAuthStore } from '../stores/useAuthStore';
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
    expect(command?.label).toBe('Forget gateway credential');
    expect(command?.keywords).toEqual(['auth', 'credential', 'token', 'sign out', 'logout', 'forget']);
    command?.onSelect();
    expect(useAuthStore.getState().credential).toBeNull();
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
