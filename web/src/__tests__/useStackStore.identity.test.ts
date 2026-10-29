import { describe, it, expect, beforeEach } from 'vitest';
import { useStackStore } from '../stores/useStackStore';
import { usePinsStore } from '../stores/usePinsStore';
import type { ClientStatus, GatewayStatus, MCPServerStatus, TokenUsage } from '../types';
import type { ServerPins } from '../lib/api';

function makeServer(name: string, overrides: Partial<MCPServerStatus> = {}): MCPServerStatus {
  return {
    name,
    transport: 'http',
    initialized: true,
    healthy: true,
    toolCount: 1,
    tools: [`${name}-tool-0`],
    ...overrides,
  };
}

function status(servers: MCPServerStatus[], tokenUsage?: TokenUsage): GatewayStatus {
  return {
    gateway: { name: 'gw', version: '0.0.0' },
    'mcp-servers': servers,
    token_usage: tokenUsage,
  };
}

function usage(totals: Record<string, number>): TokenUsage {
  const per_server: TokenUsage['per_server'] = {};
  for (const [name, total] of Object.entries(totals)) {
    per_server[name] = { input_tokens: total, output_tokens: 0, total_tokens: total };
  }
  return {
    session: { input_tokens: 0, output_tokens: 0, total_tokens: 0 },
    per_server,
    format_savings: { original_tokens: 0, formatted_tokens: 0, saved_tokens: 0, savings_percent: 0 },
  };
}

function pin(pinStatus: ServerPins['status'], toolCount: number): ServerPins {
  return {
    server_hash: 'abc',
    pinned_at: '2026-01-01T00:00:00Z',
    last_verified_at: '2026-01-01T00:00:00Z',
    tool_count: toolCount,
    status: pinStatus,
    tools: {},
  };
}

const linked: ClientStatus = {
  name: 'Claude',
  slug: 'claude',
  detected: true,
  linked: true,
  transport: 'http',
};

describe('useStackStore graph identity', () => {
  beforeEach(() => {
    useStackStore.setState({
      nodes: [],
      edges: [],
      draggedPositions: new Map(),
      expandedServers: new Set(),
      clients: [],
      gatewayInfo: null,
      mcpServers: [],
      selectedNodeId: null,
      activeServers: new Set(),
      tokenTotalsLastSeen: {},
    });
    usePinsStore.setState({ pins: null });
  });

  it('keeps node and edge references across an identical poll', () => {
    const payload = status([makeServer('github'), makeServer('jira')]);
    useStackStore.getState().setGatewayStatus(payload);
    const firstNodes = useStackStore.getState().nodes;
    const firstEdges = useStackStore.getState().edges;

    useStackStore.getState().setGatewayStatus(JSON.parse(JSON.stringify(payload)) as GatewayStatus);

    expect(useStackStore.getState().nodes).toBe(firstNodes);
    expect(useStackStore.getState().edges).toBe(firstEdges);
    for (const node of useStackStore.getState().nodes) {
      expect(node).toBe(firstNodes.find((item) => item.id === node.id));
    }
  });

  it('replaces only the server whose health changed', () => {
    useStackStore.getState().setGatewayStatus(status([makeServer('github'), makeServer('jira')]));
    const before = useStackStore.getState().nodes;

    useStackStore.getState().setGatewayStatus(status([
      makeServer('github', { healthy: false }),
      makeServer('jira'),
    ]));
    const after = useStackStore.getState().nodes;

    expect(after.find((node) => node.id === 'mcp-github')).not.toBe(before.find((node) => node.id === 'mcp-github'));
    expect(after.find((node) => node.id === 'mcp-jira')).toBe(before.find((node) => node.id === 'mcp-jira'));
    expect(after.find((node) => node.id === 'gateway')).toBe(before.find((node) => node.id === 'gateway'));
  });

  it('keeps a dragged position across the next poll', () => {
    useStackStore.getState().setGatewayStatus(status([makeServer('github')]));
    useStackStore.getState().onNodesChange([{
      id: 'mcp-github',
      type: 'position',
      position: { x: 400, y: 250 },
      dragging: false,
    }]);
    useStackStore.getState().setGatewayStatus(status([makeServer('github')]));

    const node = useStackStore.getState().nodes.find((item) => item.id === 'mcp-github');
    expect(node?.position).toEqual({ x: 400, y: 250 });
    expect(useStackStore.getState().draggedPositions.get('mcp-github')).toEqual({ x: 400, y: 250 });
  });

  it('does not replace references when refresh runs with no input change', () => {
    useStackStore.getState().setGatewayStatus(status([makeServer('github')]));
    const nodes = useStackStore.getState().nodes;
    const edges = useStackStore.getState().edges;
    useStackStore.getState().refreshNodesAndEdges();
    expect(useStackStore.getState().nodes).toBe(nodes);
    expect(useStackStore.getState().edges).toBe(edges);
  });

  it('paints a client added after status without another status call', () => {
    useStackStore.getState().setGatewayStatus(status([makeServer('github')]));
    expect(useStackStore.getState().nodes.some((node) => node.id.startsWith('client-'))).toBe(false);

    useStackStore.getState().setClients([linked]);
    expect(useStackStore.getState().nodes.some((node) => node.id === 'client-claude')).toBe(true);

    const nodes = useStackStore.getState().nodes;
    useStackStore.getState().setClients([{ ...linked }]);
    expect(useStackStore.getState().nodes).toBe(nodes);
  });

  it('replaces only the server whose pin status changed', () => {
    useStackStore.getState().setGatewayStatus(status([makeServer('github'), makeServer('jira')]));
    usePinsStore.setState({ pins: { github: pin('pinned', 3) } });
    useStackStore.getState().refreshNodesAndEdges();
    const before = useStackStore.getState().nodes;

    usePinsStore.setState({ pins: { github: pin('drift', 3) } });
    useStackStore.getState().refreshNodesAndEdges();
    const after = useStackStore.getState().nodes;

    const github = after.find((node) => node.id === 'mcp-github');
    expect(github).not.toBe(before.find((node) => node.id === 'mcp-github'));
    expect((github?.data as { pinStatus?: string }).pinStatus).toBe('drift');
    expect(after.find((node) => node.id === 'mcp-jira')).toBe(before.find((node) => node.id === 'mcp-jira'));
    expect(after.find((node) => node.id === 'gateway')).toBe(before.find((node) => node.id === 'gateway'));
  });

  it('marks a server active only after its token total increases', () => {
    useStackStore.getState().setGatewayStatus(status([makeServer('github')], usage({ github: 10 })));
    expect(useStackStore.getState().activeServers.has('github')).toBe(false);

    useStackStore.getState().setGatewayStatus(status([makeServer('github')], usage({ github: 12 })));
    expect([...useStackStore.getState().activeServers]).toEqual(['github']);

    useStackStore.getState().setGatewayStatus(status([makeServer('github')], usage({ github: 12 })));
    expect(useStackStore.getState().activeServers.size).toBe(0);
  });
});
