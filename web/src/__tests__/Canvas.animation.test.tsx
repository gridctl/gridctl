import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render } from '@testing-library/react';
import type { ReactNode } from 'react';
import { Canvas } from '../components/graph/Canvas';
import { useStackStore } from '../stores/useStackStore';
import { useAccessLensStore } from '../stores/useAccessLensStore';
import { computeHighlightState } from '../hooks/usePathHighlight';
import type { ClientStatus, GatewayStatus, MCPServerStatus, ResourceStatus } from '../types';

const captured = vi.hoisted(() => ({
  edges: [] as Array<{ id: string; animated?: boolean }>,
}));

vi.mock('@xyflow/react', () => ({
  ReactFlow: ({
    children,
    edges,
  }: {
    children?: ReactNode;
    edges?: Array<{ id: string; animated?: boolean }>;
  }) => {
    captured.edges = edges ?? [];
    return <div data-testid="react-flow">{children}</div>;
  },
  Background: () => null,
  BackgroundVariant: { Dots: 'dots', Lines: 'lines', Cross: 'cross' },
  Panel: ({ children }: { children?: ReactNode }) => <div>{children}</div>,
  Handle: () => null,
  Position: { Left: 'left', Right: 'right', Top: 'top', Bottom: 'bottom' },
  MarkerType: { Arrow: 'arrow', ArrowClosed: 'arrowclosed' },
  useReactFlow: () => ({ fitView: vi.fn(), zoomIn: vi.fn(), zoomOut: vi.fn() }),
  useViewport: () => ({ zoom: 1 }),
  applyNodeChanges: (_changes: unknown, nodes: unknown) => nodes,
  applyEdgeChanges: (_changes: unknown, edges: unknown) => edges,
}));

globalThis.requestAnimationFrame = ((cb: FrameRequestCallback) => {
  cb(0);
  return 0;
}) as typeof requestAnimationFrame;
globalThis.cancelAnimationFrame = () => {};

function makeServer(name: string): MCPServerStatus {
  return {
    name,
    transport: 'http',
    initialized: true,
    toolCount: 0,
    tools: [],
  };
}

const client: ClientStatus = {
  name: 'Claude',
  slug: 'claude',
  detected: true,
  linked: true,
  transport: 'http',
};

const resource: ResourceStatus = { name: 'db', image: 'postgres', status: 'running' };

describe('Canvas edge animation gating', () => {
  beforeEach(() => {
    captured.edges = [];
    useAccessLensStore.setState({ enabled: false, clientSlug: null, draft: [] });
    useStackStore.setState({
      expandedServers: new Set(),
      selectedNodeId: null,
      draggedPositions: new Map(),
      clients: [],
      activeServers: new Set(),
    });
  });

  it('animates at most 12 token-active server edges when nothing is selected', () => {
    const names = Array.from({ length: 15 }, (_, i) => `srv-${String(i).padStart(2, '0')}`);
    const payload: GatewayStatus = {
      gateway: { name: 'gw', version: '0.0.0' },
      'mcp-servers': names.map(makeServer),
      resources: [resource],
    };
    useStackStore.getState().setGatewayStatus(payload);
    useStackStore.setState({ activeServers: new Set(names) });
    render(<Canvas />);

    const animated = captured.edges.filter((edge) => edge.animated);
    expect(animated.length).toBe(12);
    expect(animated.map((edge) => edge.id)).toEqual(
      names.slice(0, 12).map((name) => `edge-gateway-mcp-${name}`),
    );
    expect(captured.edges.some((edge) => edge.id === 'edge-gateway-resource-db' && edge.animated)).toBe(false);
  });

  it('animates exactly the highlighted edges when a client is selected', () => {
    const names = Array.from({ length: 15 }, (_, i) => `srv-${i}`);
    const payload: GatewayStatus = {
      gateway: { name: 'gw', version: '0.0.0' },
      'mcp-servers': names.map(makeServer),
      resources: [resource],
    };
    useStackStore.getState().setGatewayStatus(payload);
    useStackStore.getState().setClients([client]);
    useStackStore.setState({
      selectedNodeId: 'client-claude',
      activeServers: new Set(names),
    });
    render(<Canvas />);

    const { nodes, edges } = useStackStore.getState();
    const highlight = computeHighlightState(nodes, edges, 'client-claude');
    const animatedIds = captured.edges.filter((edge) => edge.animated).map((edge) => edge.id).sort();
    expect(animatedIds).toEqual([...highlight.highlightedEdgeIds].sort());
    expect(animatedIds).toContain('edge-client-gateway-claude');
    expect(animatedIds).not.toContain('edge-gateway-resource-db');
  });
});
