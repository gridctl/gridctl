import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, act } from '@testing-library/react';
import type { ReactNode } from 'react';
import type { Edge, Node } from '@xyflow/react';

// Captures the nodes and edges Canvas hands React Flow. The rest mirrors the
// CanvasRefit mock so Canvas's module graph loads without a real flow.
const { seen } = vi.hoisted(() => ({
  seen: { nodes: [] as Node[], edges: [] as Edge[] },
}));

vi.mock('@xyflow/react', () => ({
  ReactFlow: ({ children, nodes, edges }: { children?: ReactNode; nodes: Node[]; edges: Edge[] }) => {
    seen.nodes = nodes;
    seen.edges = edges;
    return <div data-testid="react-flow">{children}</div>;
  },
  Background: () => null,
  BackgroundVariant: { Dots: 'dots', Lines: 'lines', Cross: 'cross' },
  Panel: ({ children }: { children?: ReactNode }) => <div>{children}</div>,
  Handle: () => null,
  Position: { Left: 'left', Right: 'right', Top: 'top', Bottom: 'bottom' },
  useReactFlow: () => ({
    fitView: vi.fn(),
    zoomIn: vi.fn(),
    zoomOut: vi.fn(),
    setViewport: vi.fn(),
    getNode: vi.fn(),
    getViewport: () => ({ x: 0, y: 0, zoom: 1 }),
  }),
  useStoreApi: () => ({ getState: () => ({ width: 1200, height: 800 }) }),
  useViewport: () => ({ zoom: 1 }),
  applyNodeChanges: (_changes: unknown, nodes: unknown) => nodes,
  applyEdgeChanges: (_changes: unknown, edges: unknown) => edges,
}));

import { Canvas } from '../components/graph/Canvas';
import { useStackStore } from '../stores/useStackStore';
import { useAccessLensStore } from '../stores/useAccessLensStore';
import { useUIStore } from '../stores/useUIStore';
import type { StackFacet, StackFilter } from '../lib/stackRows';
import type { MCPServerStatus } from '../types';

globalThis.requestAnimationFrame = ((cb: FrameRequestCallback) => {
  cb(0);
  return 0;
}) as typeof requestAnimationFrame;
globalThis.cancelAnimationFrame = () => {};

function makeServer(name: string, toolCount: number): MCPServerStatus {
  return {
    name,
    transport: 'http',
    initialized: true,
    toolCount,
    tools: Array.from({ length: toolCount }, (_, i) => `${name}-tool-${i}`),
  };
}

function filterOf(query: string, facets: StackFacet[] = []): StackFilter {
  return { query, facets: new Set(facets) };
}

const faded = (list: Array<{ id: string; className?: string }>) =>
  list.filter((item) => item.className?.split(/\s+/).includes('filtered-out')).map((item) => item.id).sort();

beforeEach(() => {
  useAccessLensStore.setState({ enabled: false, clientSlug: null });
  useUIStore.setState({ sidebarOpen: false });
  useStackStore.setState({
    expandedServers: new Set(),
    selectedNodeId: null,
    draggedPositions: new Map(),
    clients: [],
    nodes: [],
    edges: [],
  });
  useStackStore.getState().setGatewayStatus({
    gateway: { name: 'gw', version: '0.0.0' },
    'mcp-servers': [makeServer('github', 2), makeServer('slack', 1)],
    resources: [{ name: 'db', image: 'postgres:16', status: 'running' }],
  });
  useStackStore.getState().setClients([
    { name: 'Claude', slug: 'claude', detected: true, linked: true, transport: 'http' },
  ]);
  act(() => useStackStore.getState().toggleServerExpanded('mcp-github'));
});

describe('Canvas filter', () => {
  it('passes the store nodes through untouched when the filter is inactive', () => {
    const { rerender } = render(<Canvas />);
    const edgesWithout = seen.edges;
    expect(seen.nodes).toBe(useStackStore.getState().nodes);
    rerender(<Canvas filter={filterOf('  ')} />);
    expect(seen.nodes).toBe(useStackStore.getState().nodes);
    expect(seen.edges).toEqual(edgesWithout);
    expect(faded(seen.nodes)).toEqual([]);
  });

  it('fades non-matching nodes, their tools, and their edges, never the gateway', () => {
    const storeNodes = useStackStore.getState().nodes;
    render(<Canvas filter={filterOf('slack')} />);

    const githubTools = storeNodes
      .filter((n) => (n.data as { serverNodeId?: string }).serverNodeId === 'mcp-github')
      .map((n) => n.id);
    expect(githubTools.length).toBeGreaterThan(0);
    expect(faded(seen.nodes)).toEqual(
      ['mcp-github', 'resource-db', 'client-claude', ...githubTools].sort(),
    );

    const fadedEdges = faded(seen.edges);
    const expectedEdges = useStackStore
      .getState()
      .edges.filter((e) => new Set(faded(seen.nodes)).has(e.source) || new Set(faded(seen.nodes)).has(e.target))
      .map((e) => e.id)
      .sort();
    expect(fadedEdges).toEqual(expectedEdges);
    expect(fadedEdges).toContain('edge-gateway-mcp-github');
    expect(fadedEdges).not.toContain('edge-gateway-mcp-slack');

    // Same ids and positions; the filter only adds a class.
    expect(seen.nodes.map((n) => [n.id, n.position])).toEqual(storeNodes.map((n) => [n.id, n.position]));
    expect(useStackStore.getState().selectedNodeId).toBeNull();
  });

  it('applies facets by row state', () => {
    render(<Canvas filter={filterOf('', ['linked'])} />);
    expect(faded(seen.nodes)).not.toContain('client-claude');
    expect(faded(seen.nodes)).toContain('mcp-slack');
    expect(faded(seen.nodes)).not.toContain('gateway');
  });

  it('composes with the selection classes', () => {
    act(() => useStackStore.getState().selectNode('client-claude'));
    render(<Canvas filter={filterOf('github')} />);
    const byId = new Map(seen.nodes.map((n) => [n.id, n.className ?? '']));
    expect(byId.get('client-claude')).toMatch(/\b(highlighted|dimmed)\b/);
    expect(byId.get('client-claude')).toContain('filtered-out');
    expect(byId.get('mcp-github')).toMatch(/\b(highlighted|dimmed)\b/);
    expect(byId.get('mcp-github')).not.toContain('filtered-out');
  });
});
