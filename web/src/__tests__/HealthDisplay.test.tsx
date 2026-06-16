import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import '@testing-library/jest-dom';

// Mock @xyflow/react before importing components that use it
vi.mock('@xyflow/react', () => ({
  Handle: ({ id }: { id: string }) => <div data-testid={`handle-${id}`} />,
  Position: { Left: 'left', Right: 'right' },
  MarkerType: { ArrowClosed: 'arrowclosed' },
}));

import CustomNode from '../components/graph/CustomNode';
import { createMCPServerNodes } from '../lib/graph/nodes';
import type { MCPServerNodeData, MCPServerStatus, ResourceNodeData } from '../types';

function makeServerData(overrides: Partial<MCPServerNodeData> = {}): MCPServerNodeData {
  return {
    type: 'mcp-server',
    name: 'test-server',
    transport: 'http',
    initialized: true,
    toolCount: 3,
    tools: ['tool1', 'tool2', 'tool3'],
    status: 'running',
    ...overrides,
  };
}

describe('CustomNode health indicator', () => {
  it('shows health error when healthy is false', () => {
    const data = makeServerData({
      healthy: false,
      healthError: 'connection refused',
      status: 'error',
    });

    render(<CustomNode data={data} />);

    expect(screen.getByText('connection refused')).toBeInTheDocument();
  });

  it('shows default message when healthy is false with no healthError', () => {
    const data = makeServerData({
      healthy: false,
      healthError: '',
      status: 'error',
    });

    render(<CustomNode data={data} />);

    expect(screen.getByText('Health check failed')).toBeInTheDocument();
  });

  it('shows "Healthy" when healthy is true', () => {
    const data = makeServerData({
      healthy: true,
    });

    render(<CustomNode data={data} />);

    expect(screen.getByText('Healthy')).toBeInTheDocument();
  });

  it('shows no health indicator when healthy is undefined', () => {
    const data = makeServerData({
      healthy: undefined,
    });

    render(<CustomNode data={data} />);

    expect(screen.queryByText('Healthy')).not.toBeInTheDocument();
    expect(screen.queryByText('Health check failed')).not.toBeInTheDocument();
  });

  it('shows no health indicator for resource nodes', () => {
    const data: ResourceNodeData = {
      type: 'resource',
      name: 'postgres',
      image: 'postgres:16',
      status: 'running',
    };

    render(<CustomNode data={data} />);

    expect(screen.queryByText('Healthy')).not.toBeInTheDocument();
  });
});

// Unhealthy server-count coverage moved to StatusBar.test.tsx: the header no
// longer mirrors server health (it lives only in the StatusBar now).

// Test OpenAPI server type display
describe('CustomNode OpenAPI type', () => {
  it('renders "OpenAPI" badge when openapi is true', () => {
    const data = makeServerData({ openapi: true, openapiSpec: 'https://api.example.com/openapi.json' });
    render(<CustomNode data={data} />);
    expect(screen.getByText('OpenAPI')).toBeInTheDocument();
  });

  it('does not render "Container" badge when openapi is true', () => {
    const data = makeServerData({ openapi: true });
    render(<CustomNode data={data} />);
    expect(screen.queryByText('Container')).not.toBeInTheDocument();
    expect(screen.getByText('OpenAPI')).toBeInTheDocument();
  });

  it('renders "Container" badge when openapi is false', () => {
    const data = makeServerData({ openapi: false });
    render(<CustomNode data={data} />);
    expect(screen.getByText('Container')).toBeInTheDocument();
    expect(screen.queryByText('OpenAPI')).not.toBeInTheDocument();
  });

  it('renders "Container" badge when openapi is undefined', () => {
    const data = makeServerData();
    render(<CustomNode data={data} />);
    expect(screen.getByText('Container')).toBeInTheDocument();
    expect(screen.queryByText('OpenAPI')).not.toBeInTheDocument();
  });
});

// Test createMCPServerNodes passes through OpenAPI fields
describe('createMCPServerNodes OpenAPI fields', () => {
  function makeServerStatus(overrides: Partial<MCPServerStatus> = {}): MCPServerStatus {
    return {
      name: 'test-server',
      transport: 'http',
      initialized: true,
      toolCount: 3,
      tools: ['tool1', 'tool2', 'tool3'],
      ...overrides,
    };
  }

  it('passes through openapi and openapiSpec fields', () => {
    const servers = [makeServerStatus({
      openapi: true,
      openapiSpec: 'https://api.example.com/openapi.json',
    })];
    const nodes = createMCPServerNodes(servers);
    expect(nodes).toHaveLength(1);
    expect(nodes[0].data.openapi).toBe(true);
    expect(nodes[0].data.openapiSpec).toBe('https://api.example.com/openapi.json');
  });

  it('passes through undefined when openapi fields not set', () => {
    const servers = [makeServerStatus()];
    const nodes = createMCPServerNodes(servers);
    expect(nodes).toHaveLength(1);
    expect(nodes[0].data.openapi).toBeUndefined();
    expect(nodes[0].data.openapiSpec).toBeUndefined();
  });
});

// Test formatRelativeTime
describe('formatRelativeTime', () => {
  it('returns "just now" for recent times', async () => {
    const { formatRelativeTime } = await import('../lib/time');
    const now = new Date();
    expect(formatRelativeTime(now)).toBe('just now');
  });

  it('returns seconds for times under a minute', async () => {
    const { formatRelativeTime } = await import('../lib/time');
    const date = new Date(Date.now() - 30_000);
    expect(formatRelativeTime(date)).toBe('30s ago');
  });

  it('returns minutes for times under an hour', async () => {
    const { formatRelativeTime } = await import('../lib/time');
    const date = new Date(Date.now() - 5 * 60_000);
    expect(formatRelativeTime(date)).toBe('5m ago');
  });

  it('returns hours for times over an hour', async () => {
    const { formatRelativeTime } = await import('../lib/time');
    const date = new Date(Date.now() - 2 * 3_600_000);
    expect(formatRelativeTime(date)).toBe('2h ago');
  });
});
