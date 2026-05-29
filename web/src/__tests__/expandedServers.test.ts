import { describe, it, expect, beforeEach } from 'vitest';

import { useStackStore } from '../stores/useStackStore';
import { TOOL_FANOUT_CAP } from '../lib/graph/toolFanout';
import type { GatewayStatus, MCPServerStatus } from '../types';

function makeServer(name: string, toolCount: number): MCPServerStatus {
  return {
    name,
    transport: 'http',
    initialized: true,
    toolCount,
    tools: Array.from({ length: toolCount }, (_, i) => `${name}-tool-${i}`),
  };
}

function status(servers: MCPServerStatus[]): GatewayStatus {
  return {
    gateway: { name: 'gw', version: '0.0.0' },
    'mcp-servers': servers,
  };
}

function toolNodeCountFor(serverNodeId: string): number {
  return useStackStore
    .getState()
    .nodes.filter((n) => {
      const d = n.data as { type?: string; serverNodeId?: string };
      return (d.type === 'tool' || d.type === 'tool-overflow') && d.serverNodeId === serverNodeId;
    }).length;
}

describe('useStackStore tool expansion', () => {
  beforeEach(() => {
    // Reset expansion and seed two servers: github (3 tools), jira (15 tools).
    useStackStore.setState({ expandedServers: new Set() });
    useStackStore.getState().setGatewayStatus(
      status([makeServer('github', 3), makeServer('jira', TOOL_FANOUT_CAP + 5)])
    );
  });

  it('starts with no servers expanded and no tool nodes', () => {
    expect(useStackStore.getState().expandedServers.size).toBe(0);
    expect(toolNodeCountFor('mcp-github')).toBe(0);
  });

  it('toggles a server expanded and back collapsed', () => {
    const { toggleServerExpanded } = useStackStore.getState();

    toggleServerExpanded('mcp-github');
    expect(useStackStore.getState().expandedServers.has('mcp-github')).toBe(true);
    expect(toolNodeCountFor('mcp-github')).toBe(3);

    toggleServerExpanded('mcp-github');
    expect(useStackStore.getState().expandedServers.has('mcp-github')).toBe(false);
    expect(toolNodeCountFor('mcp-github')).toBe(0);
  });

  it('expandServer / collapseServer are idempotent', () => {
    const { expandServer, collapseServer } = useStackStore.getState();

    expandServer('mcp-github');
    expandServer('mcp-github');
    expect(useStackStore.getState().expandedServers.size).toBe(1);
    expect(toolNodeCountFor('mcp-github')).toBe(3);

    collapseServer('mcp-github');
    collapseServer('mcp-github');
    expect(useStackStore.getState().expandedServers.size).toBe(0);
  });

  it('expands multiple servers independently', () => {
    const { expandServer } = useStackStore.getState();
    expandServer('mcp-github');
    expandServer('mcp-jira');

    expect(toolNodeCountFor('mcp-github')).toBe(3);
    // jira has 15 tools -> capped 10 tool nodes + 1 overflow node = 11.
    expect(toolNodeCountFor('mcp-jira')).toBe(TOOL_FANOUT_CAP + 1);
  });

  it('caps fan-out and adds a single overflow node for a large server', () => {
    useStackStore.getState().expandServer('mcp-jira');
    const overflow = useStackStore
      .getState()
      .nodes.filter((n) => n.type === 'toolOverflow');
    expect(overflow).toHaveLength(1);
    const data = overflow[0].data as { overflowCount: number };
    expect(data.overflowCount).toBe(5);
  });

  it('keeps expansion across a polling refresh (setGatewayStatus)', () => {
    useStackStore.getState().expandServer('mcp-github');
    expect(toolNodeCountFor('mcp-github')).toBe(3);

    // Simulate the next poll delivering the same status.
    useStackStore.getState().setGatewayStatus(
      status([makeServer('github', 3), makeServer('jira', TOOL_FANOUT_CAP + 5)])
    );

    expect(useStackStore.getState().expandedServers.has('mcp-github')).toBe(true);
    expect(toolNodeCountFor('mcp-github')).toBe(3);
  });
});
