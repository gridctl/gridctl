import { describe, it, expect, beforeEach } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { ReactFlowProvider } from '@xyflow/react';
import type { ReactNode } from 'react';
import { CommandRegistryProvider, useCommandRegistry } from '../hooks/useCommandRegistry';
import { useGlobalCommands } from '../hooks/useGlobalCommands';
import { useStackStore } from '../stores/useStackStore';
import { useUIStore } from '../stores/useUIStore';
import type { GatewayStatus, MCPServerStatus } from '../types';

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
    };
    useStackStore.getState().setGatewayStatus(status);
    useStackStore.setState({ selectedNodeId: null });
    useUIStore.setState({ sidebarOpen: false });
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
  });
});
