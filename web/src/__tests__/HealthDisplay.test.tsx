import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import '@testing-library/jest-dom';

// Mock @xyflow/react before importing components that use it
vi.mock('@xyflow/react', () => ({
  Handle: ({ id }: { id: string }) => <div data-testid={`handle-${id}`} />,
  Position: { Left: 'left', Right: 'right' },
}));

import CustomNode from '../components/graph/CustomNode';
import type { MCPServerNodeData, ResourceNodeData } from '../types';

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

// Test Header unhealthy count
describe('Header unhealthy count', () => {
  beforeEach(() => {
    vi.resetModules();
  });

  it('shows unhealthy count when servers are unhealthy', async () => {
    // Mock the store module
    vi.doMock('../stores/useStackStore', () => ({
      useStackStore: (selector: (s: Record<string, unknown>) => unknown) => {
        const state = {
          gatewayInfo: { name: 'test', version: '0.1.0' },
          mcpServers: [
            { name: 's1', initialized: true, healthy: true, toolCount: 1, tools: [] },
            { name: 's2', initialized: true, healthy: false, toolCount: 2, tools: [], healthError: 'timeout' },
            { name: 's3', initialized: true, healthy: false, toolCount: 0, tools: [], healthError: 'refused' },
          ],
          connectionStatus: 'connected',
        };
        return selector(state);
      },
    }));

    // Mock logo import
    vi.doMock('../assets/brand/logo.svg', () => ({ default: 'logo.svg' }));

    const { Header } = await import('../components/layout/Header');
    render(<Header />);

    expect(screen.getByText('(2 unhealthy)')).toBeInTheDocument();
  });

  it('does not show unhealthy count when all servers healthy', async () => {
    vi.doMock('../stores/useStackStore', () => ({
      useStackStore: (selector: (s: Record<string, unknown>) => unknown) => {
        const state = {
          gatewayInfo: { name: 'test', version: '0.1.0' },
          mcpServers: [
            { name: 's1', initialized: true, healthy: true, toolCount: 1, tools: [] },
            { name: 's2', initialized: true, healthy: true, toolCount: 2, tools: [] },
          ],
          connectionStatus: 'connected',
        };
        return selector(state);
      },
    }));

    vi.doMock('../assets/brand/logo.svg', () => ({ default: 'logo.svg' }));

    const { Header } = await import('../components/layout/Header');
    render(<Header />);

    expect(screen.queryByText(/unhealthy/)).not.toBeInTheDocument();
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
