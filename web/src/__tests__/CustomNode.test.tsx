import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import '@testing-library/jest-dom';

vi.mock('@xyflow/react', () => ({
  Handle: ({ id }: { id: string }) => <div data-testid={`handle-${id}`} />,
  Position: { Left: 'left', Right: 'right' },
  MarkerType: { ArrowClosed: 'arrowclosed' },
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

describe('CustomNode', () => {
  it('renders node with correct label', () => {
    render(<CustomNode data={makeServerData({ name: 'my-server' })} />);
    expect(screen.getByText('my-server')).toBeInTheDocument();
  });

  it('shows running status indicator', () => {
    render(<CustomNode data={makeServerData({ status: 'running' })} />);
    expect(screen.getByText('running')).toBeInTheDocument();
  });

  it('shows stopped status indicator', () => {
    render(<CustomNode data={makeServerData({ status: 'stopped' })} />);
    expect(screen.getByText('stopped')).toBeInTheDocument();
  });

  it('shows error status indicator', () => {
    render(<CustomNode data={makeServerData({ status: 'error' })} />);
    expect(screen.getByText('error')).toBeInTheDocument();
  });

  it('renders mcp-server type with Container badge', () => {
    render(<CustomNode data={makeServerData()} />);
    expect(screen.getByText('Container')).toBeInTheDocument();
  });

  it('renders external server with External badge', () => {
    render(<CustomNode data={makeServerData({ external: true })} />);
    expect(screen.getByText('External')).toBeInTheDocument();
    expect(screen.queryByText('Container')).not.toBeInTheDocument();
  });

  it('renders local process with Local badge', () => {
    render(<CustomNode data={makeServerData({ localProcess: true })} />);
    expect(screen.getByText('Local')).toBeInTheDocument();
  });

  it('renders SSH server with SSH badge', () => {
    render(<CustomNode data={makeServerData({ ssh: true })} />);
    expect(screen.getByText('SSH')).toBeInTheDocument();
  });

  it('renders OpenAPI server with OpenAPI badge', () => {
    render(<CustomNode data={makeServerData({ openapi: true })} />);
    expect(screen.getByText('OpenAPI')).toBeInTheDocument();
  });

  it('renders ×N replica badge when replicaCount > 1', () => {
    render(<CustomNode data={makeServerData({ replicaCount: 3 })} />);
    expect(screen.getByText('×3')).toBeInTheDocument();
  });

  it('omits replica badge for single-replica servers', () => {
    render(<CustomNode data={makeServerData({ replicaCount: 1 })} />);
    expect(screen.queryByText(/^×/)).not.toBeInTheDocument();
  });

  it('omits replica badge when replicaCount is undefined', () => {
    render(<CustomNode data={makeServerData()} />);
    expect(screen.queryByText(/^×/)).not.toBeInTheDocument();
  });

  it('renders resource node with image', () => {
    const data: ResourceNodeData = {
      type: 'resource',
      name: 'postgres',
      image: 'postgres:16',
      status: 'running',
    };
    render(<CustomNode data={data} />);
    expect(screen.getByText('postgres')).toBeInTheDocument();
    expect(screen.getByText('postgres:16')).toBeInTheDocument();
  });

  it('displays tool count for servers', () => {
    render(<CustomNode data={makeServerData({ toolCount: 5 })} />);
    expect(screen.getByText('5 tools')).toBeInTheDocument();
  });

  it('displays transport type', () => {
    render(<CustomNode data={makeServerData({ transport: 'stdio' })} />);
    expect(screen.getByText('stdio')).toBeInTheDocument();
  });

  it('renders connection handles', () => {
    render(<CustomNode data={makeServerData()} />);
    expect(screen.getByTestId('handle-input')).toBeInTheDocument();
    expect(screen.getByTestId('handle-output')).toBeInTheDocument();
  });

  it('displays output format badge when not json', () => {
    render(<CustomNode data={makeServerData({ outputFormat: 'toon' })} />);
    expect(screen.getByText('toon')).toBeInTheDocument();
  });

  it('hides output format badge for json default', () => {
    render(<CustomNode data={makeServerData({ outputFormat: 'json' })} />);
    // Transport 'http' should be visible but 'json' format badge should not
    expect(screen.getByText('http')).toBeInTheDocument();
    expect(screen.queryByText('json')).not.toBeInTheDocument();
  });

  it('hides output format badge when not set', () => {
    render(<CustomNode data={makeServerData()} />);
    expect(screen.queryByText('toon')).not.toBeInTheDocument();
    expect(screen.queryByText('csv')).not.toBeInTheDocument();
  });

  it('renders ×current/target badge when autoscale is present', () => {
    render(
      <CustomNode
        data={makeServerData({
          autoscale: {
            min: 1,
            max: 5,
            current: 2,
            target: 3,
            targetInFlight: 10,
            medianInFlight: 14,
            lastDecision: 'up',
          },
        })}
      />,
    );
    expect(screen.getByText('×2/3')).toBeInTheDocument();
  });

  it('autoscale badge takes precedence over replicaCount badge', () => {
    render(
      <CustomNode
        data={makeServerData({
          replicaCount: 3,
          autoscale: {
            min: 1,
            max: 5,
            current: 2,
            target: 3,
            targetInFlight: 10,
            medianInFlight: 14,
            lastDecision: 'noop',
          },
        })}
      />,
    );
    expect(screen.getByText('×2/3')).toBeInTheDocument();
    expect(screen.queryByText('×3')).not.toBeInTheDocument();
  });

  it('falls back to ×N when only replicaCount is set', () => {
    render(<CustomNode data={makeServerData({ replicaCount: 4 })} />);
    expect(screen.getByText('×4')).toBeInTheDocument();
  });

  it('renders no scale badge when neither autoscale nor replicaCount > 1', () => {
    render(<CustomNode data={makeServerData()} />);
    expect(screen.queryByText(/^×/)).not.toBeInTheDocument();
  });
});
