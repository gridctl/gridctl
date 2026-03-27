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
    clientCount: 0,
    totalToolCount: 5,
    sessions: 0,
    codeMode: null,
    totalSkills: 0,
    activeSkills: 0,
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


describe('createGatewayNode passes session data', () => {
  const gatewayInfo = { name: 'test', version: 'v1.0' };
  const mcpServers = [{ name: 's1', transport: 'http' as const, initialized: true, toolCount: 2, tools: ['a', 'b'] }];

  it('includes sessions in node data', () => {
    const node = createGatewayNode(gatewayInfo, mcpServers, [], 5);
    expect(node.data.sessions).toBe(5);
  });

  it('defaults sessions to 0 when omitted', () => {
    const node = createGatewayNode(gatewayInfo, mcpServers, []);
    expect(node.data.sessions).toBe(0);
  });
});
