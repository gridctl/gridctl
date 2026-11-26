import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import '@testing-library/jest-dom';

vi.mock('@xyflow/react', () => ({
  Handle: ({ id }: { id: string }) => <div data-testid={`handle-${id}`} />,
  Position: { Left: 'left', Right: 'right' },
  MarkerType: { ArrowClosed: 'arrowclosed' },
}));

import CustomNode from '../components/graph/CustomNode';
import type { MCPServerNodeData, ResourceNodeData } from '../types';
import { STATUS_SHAPES } from '../components/ui/StatusDot';

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

  it('names compact status from the dot and hides the clipped badge from assistive tech', () => {
    useUIStore.setState({ compactCards: true });
    render(<CustomNode data={makeServerData({ status: 'running' })} />);
    expect(screen.getByRole('img', { name: 'running' })).toBeInTheDocument();
    expect(screen.getAllByText('running')).toHaveLength(1);
    expect(screen.getByText('running').closest('[aria-hidden="true"]')).not.toBeNull();
  });

  it('keeps the expanded status badge in the accessibility tree', () => {
    useUIStore.setState({ compactCards: false });
    render(<CustomNode data={makeServerData({ status: 'running' })} />);
    expect(screen.getByText('running').closest('[aria-hidden]')).toBeNull();
  });

  afterEach(() => {
    useUIStore.setState({ compactCards: true });
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

// The drift annotation is a deep link into the Pins workspace; it needs a
// router only when a drift is rendered, so bare renders above stay
// router-free.
import { fireEvent } from '@testing-library/react';
import { MemoryRouter, Routes, Route, useLocation } from 'react-router';
import { useUIStore } from '../stores/useUIStore';

function LocationProbe() {
  const location = useLocation();
  return <div data-testid="location">{location.pathname + location.search}</div>;
}

describe('CustomNode pin drift link', () => {
  it('navigates to the server drift view in the Pins workspace', () => {
    // Full-card mode: the annotation renders only on expanded cards.
    useUIStore.setState({ compactCards: false });
    try {
      render(
        <MemoryRouter initialEntries={['/']}>
          <CustomNode data={makeServerData({ name: 'zapier', pinStatus: 'drift' })} />
          <Routes>
            <Route path="*" element={<LocationProbe />} />
          </Routes>
        </MemoryRouter>,
      );

      fireEvent.click(screen.getByRole('button', { name: /schema drift detected/i }));

      expect(screen.getByTestId('location')).toHaveTextContent('/pins?server=zapier&view=drift');
    } finally {
      useUIStore.setState({ compactCards: true });
    }
  });
});

describe('CustomNode compact glyphs', () => {
  afterEach(() => {
    useUIStore.setState({ compactCards: true });
  });

  function renderCompact(data: MCPServerNodeData | ResourceNodeData) {
    useUIStore.setState({ compactCards: true });
    return render(
      <MemoryRouter initialEntries={['/']}>
        <CustomNode data={data} />
        <Routes>
          <Route path="*" element={<LocationProbe />} />
        </Routes>
      </MemoryRouter>,
    );
  }

  const stoppedResource: ResourceNodeData = { type: 'resource', name: 'db', image: 'postgres:16', status: 'stopped' };

  it.each<[string, MCPServerNodeData | ResourceNodeData, string, boolean]>([
    ['needs-auth', makeServerData({ status: 'needs-auth', authStatus: 'needs_auth' }), 'Needs authorization', true],
    ['error', makeServerData({ status: 'error', healthy: false, healthError: 'boom' }), 'Error', true],
    ['restarting', makeServerData({ status: 'restarting', healthy: false }), 'Restarting', true],
    ['initializing', makeServerData({ status: 'initializing', initialized: false }), 'Initializing', true],
    ['idle', makeServerData({ status: 'idle' }), 'Idle', true],
    ['stopped resource', stoppedResource, 'Stopped', true],
    ['disabled', makeServerData({ status: 'disabled', enabled: false }), 'Disabled', true],
    ['blocked', makeServerData({ pinStatus: 'blocked' }), 'Blocked', false],
  ])('names the %s glyph beside the node name', (_case, data, name, isState) => {
    const { container } = renderCompact(data);
    const glyph = screen.getByRole('img', { name });
    expect(glyph.closest('[aria-hidden="true"]')).toBeNull();
    const dot = container.querySelector('[data-shape]');
    if (isState) {
      expect(dot?.closest('[aria-hidden="true"]')).not.toBeNull();
    } else {
      expect(screen.getByRole('img', { name: 'running' }).closest('[aria-hidden="true"]')).toBeNull();
    }
  });

  it('carries the health summary in the error glyph title', () => {
    renderCompact(makeServerData({ status: 'error', healthy: false, healthError: 'boom' }));
    const glyph = screen.getByRole('img', { name: 'Error' });
    expect(glyph.getAttribute('title')).toContain('boom');
  });

  it('carries the restart summary in the restarting glyph title', () => {
    renderCompact(makeServerData({
      status: 'restarting',
      healthy: false,
      replicas: [{ replicaId: 0, state: 'restarting', healthy: false, inFlight: 0, restartAttempts: 2 }],
    }));
    expect(screen.getByRole('img', { name: 'Restarting' }).getAttribute('title')).toContain('attempt 2');
  });

  it('renders no glyph for a running server without pin state', () => {
    const { container } = renderCompact(makeServerData());
    expect(container.querySelector('[data-glyph]')).toBeNull();
  });

  it('renders only the disabled glyph and a disabled dot for a disabled server needing auth', () => {
    const { container } = renderCompact(makeServerData({ status: 'disabled', enabled: false, authStatus: 'needs_auth' }));
    const glyphs = container.querySelectorAll('[data-glyph]');
    expect([...glyphs].map((g) => g.getAttribute('data-glyph'))).toEqual(['disabled']);
    const dot = container.querySelector('[data-shape]');
    expect(dot).toHaveAttribute('title', 'disabled');
    expect(dot).toHaveAttribute('data-shape', STATUS_SHAPES.disabled);
    expect(dot).not.toHaveAttribute('data-shape', STATUS_SHAPES.initializing);
  });

  it('links the drift glyph to the server drift view in Pins', () => {
    renderCompact(makeServerData({ name: 'zapier', pinStatus: 'drift' }));
    fireEvent.click(screen.getByRole('button', { name: 'Schema drift detected, review in Pins' }));
    expect(screen.getByTestId('location')).toHaveTextContent('/pins?server=zapier&view=drift');
  });

  it('renders both glyphs for a blocked server in error', () => {
    const { container } = renderCompact(makeServerData({ status: 'error', healthy: false, pinStatus: 'blocked' }));
    expect([...container.querySelectorAll('[data-glyph]')].map((g) => g.getAttribute('data-glyph'))).toEqual(['error', 'blocked']);
  });

  it('labels the compact tool counter', () => {
    renderCompact(makeServerData({ toolCount: 3 }));
    expect(screen.getByLabelText('3 tools')).toHaveAttribute('title', '3 tools');
  });

  it('titles the node name', () => {
    renderCompact(makeServerData({ name: 'a-very-long-server-name' }));
    expect(screen.getByText('a-very-long-server-name')).toHaveAttribute('title', 'a-very-long-server-name');
  });

  const autoscale = (lastDecision: 'up' | 'down' | 'noop') => ({
    min: 1, max: 5, current: 2, target: 3, targetInFlight: 10, medianInFlight: 14, lastDecision,
  });

  it('pulses a scale-up overlay and names the direction without animating the card', () => {
    const { container } = renderCompact(makeServerData({ autoscale: autoscale('up') }));
    const ring = container.querySelector('[data-scale-ring="up"]');
    expect(ring).toHaveClass('animate-pulse-glow');
    expect(ring).toHaveAttribute('aria-hidden', 'true');
    const card = ring?.parentElement as HTMLElement;
    expect(card).not.toHaveClass('animate-pulse-glow');
    expect(screen.getByLabelText(/scaling up/)).toBeInTheDocument();
    expect(screen.getByText('×2/3')).toBeInTheDocument();
  });

  it('marks a scale-down with a still overlay and direction', () => {
    const { container } = renderCompact(makeServerData({ autoscale: autoscale('down') }));
    const ring = container.querySelector('[data-scale-ring="down"]');
    expect(ring).not.toBeNull();
    expect(ring).not.toHaveClass('animate-pulse-glow');
    expect(screen.getByLabelText(/scaling down/)).toBeInTheDocument();
  });

  it('renders no scale overlay for a noop decision', () => {
    const { container } = renderCompact(makeServerData({ autoscale: autoscale('noop') }));
    expect(container.querySelector('[data-scale-ring]')).toBeNull();
  });
});
