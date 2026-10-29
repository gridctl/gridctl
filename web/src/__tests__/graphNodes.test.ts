import { describe, it, expect } from 'vitest';
import {
  createClientNodes,
  createGatewayNode,
  createMCPServerNodes,
  createResourceNodes,
  createSkillGroupNodes,
} from '../lib/graph/nodes';
import { createToolFanout } from '../lib/graph/toolFanout';
import type { AgentSkill, ClientStatus, MCPServerStatus, ResourceStatus } from '../types';
import type { Node } from '@xyflow/react';

function server(overrides: Partial<MCPServerStatus> = {}): MCPServerStatus {
  return {
    name: 'github',
    transport: 'http',
    initialized: true,
    healthy: true,
    toolCount: 1,
    tools: ['search'],
    ...overrides,
  };
}

describe('graph node accessible names', () => {
  it('announces a running server, a needs-auth server, and a disabled server', () => {
    const running = createMCPServerNodes([server()])[0];
    expect(running.ariaLabel).toBe('github, MCP server, running');

    const pending = createMCPServerNodes([server({ authStatus: 'needs_auth' })])[0];
    expect(pending.ariaLabel).toBe('github, MCP server, needs authorization');

    const disabled = createMCPServerNodes([server({ enabled: false, healthy: false })])[0];
    expect(disabled.ariaLabel).toBe('github, MCP server, disabled');
  });

  it('announces resources, clients, and the gateway', () => {
    const resource: ResourceStatus = { name: 'db', image: 'postgres', status: 'running' };
    expect(createResourceNodes([resource])[0].ariaLabel).toBe('db, resource, running');

    const client: ClientStatus = {
      name: 'Claude',
      slug: 'claude',
      detected: true,
      linked: true,
      transport: 'http',
    };
    expect(createClientNodes([client])[0].ariaLabel).toBe('Claude, client, linked');

    const gateway = createGatewayNode({ name: 'gw', version: '0.0.0' }, [], []);
    expect(gateway.ariaLabel).toBe('gw, gateway, active');
  });

  it('announces skill groups from the producer', () => {
    const skills: AgentSkill[] = [
      { name: 'branch', description: 'branch', state: 'active', fileCount: 1, dir: 'git/branch' },
      { name: 'fork', description: 'fork', state: 'active', fileCount: 1, dir: 'git/fork' },
    ];
    const group = createSkillGroupNodes(skills)[0];
    expect(group.ariaLabel).toBe('git, skill group, 2 of 2 active');
  });

  it('marks tool and overflow nodes as not focusable and names them', () => {
    const serverNode: Node = {
      id: 'mcp-github',
      position: { x: 0, y: 0 },
      data: { type: 'mcp-server', name: 'github', tools: Array.from({ length: 12 }, (_, i) => `tool-${i}`) },
    };
    const { nodes } = createToolFanout(serverNode);
    const tool = nodes.find((item) => item.data && (item.data as { type?: string }).type === 'tool');
    const overflow = nodes.find((item) => (item.data as { type?: string }).type === 'tool-overflow');
    expect(tool?.focusable).toBe(false);
    expect(tool?.ariaLabel).toBe('github tool tool-0');
    expect(overflow?.focusable).toBe(false);
    expect(overflow?.ariaLabel).toBe('2 more github tools');
  });
});
