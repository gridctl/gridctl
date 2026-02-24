import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import '@testing-library/jest-dom';

// Mock @xyflow/react before importing components that use it
vi.mock('@xyflow/react', () => ({
  Handle: ({ id }: { id: string }) => <div data-testid={`handle-${id}`} />,
  Position: { Left: 'left', Right: 'right' },
  MarkerType: { ArrowClosed: 'arrowclosed' },
}));

import GatewayNode from '../components/graph/GatewayNode';
import { createGatewayNode } from '../lib/graph/nodes';
import type { GatewayNodeData } from '../types';

function makeGatewayData(overrides: Partial<GatewayNodeData> = {}): GatewayNodeData {
  return {
    type: 'gateway',
    name: 'test-stack',
    version: 'v0.1.0',
    serverCount: 2,
    resourceCount: 0,
    agentCount: 0,
    a2aAgentCount: 0,
    clientCount: 0,
    totalToolCount: 5,
    sessions: 0,
    a2aTasks: null,
    codeMode: null,
    ...overrides,
  };
}

describe('GatewayNode session count', () => {
  it('renders session count when > 0', () => {
    const data = makeGatewayData({ sessions: 12 });
    render(<GatewayNode data={data} />);
    expect(screen.getByText('Sessions')).toBeInTheDocument();
    expect(screen.getByText('12')).toBeInTheDocument();
  });

  it('hides session row when count is 0', () => {
    const data = makeGatewayData({ sessions: 0 });
    render(<GatewayNode data={data} />);
    expect(screen.queryByText('Sessions')).not.toBeInTheDocument();
  });
});

describe('GatewayNode A2A task count', () => {
  it('renders A2A task count when present and > 0', () => {
    const data = makeGatewayData({ a2aTasks: 3 });
    render(<GatewayNode data={data} />);
    expect(screen.getByText('A2A Tasks')).toBeInTheDocument();
    expect(screen.getByText('3')).toBeInTheDocument();
  });

  it('hides A2A task row when count is 0', () => {
    const data = makeGatewayData({ a2aTasks: 0 });
    render(<GatewayNode data={data} />);
    expect(screen.queryByText('A2A Tasks')).not.toBeInTheDocument();
  });

  it('hides A2A task row when null (no A2A gateway)', () => {
    const data = makeGatewayData({ a2aTasks: null });
    render(<GatewayNode data={data} />);
    expect(screen.queryByText('A2A Tasks')).not.toBeInTheDocument();
  });
});

describe('createGatewayNode passes session/task data', () => {
  const gatewayInfo = { name: 'test', version: 'v1.0' };
  const mcpServers = [{ name: 's1', transport: 'http' as const, initialized: true, toolCount: 2, tools: ['a', 'b'] }];

  it('includes sessions and a2aTasks in node data', () => {
    const node = createGatewayNode(gatewayInfo, mcpServers, [], [], 5, 3);
    expect(node.data.sessions).toBe(5);
    expect(node.data.a2aTasks).toBe(3);
  });

  it('defaults sessions to 0 and a2aTasks to null when omitted', () => {
    const node = createGatewayNode(gatewayInfo, mcpServers, [], []);
    expect(node.data.sessions).toBe(0);
    expect(node.data.a2aTasks).toBeNull();
  });
});
