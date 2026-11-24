import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, fireEvent, act } from '@testing-library/react';
import '@testing-library/jest-dom';
import type { KeyboardEvent, ReactNode } from 'react';
import { Canvas } from '../components/graph/Canvas';
import { useStackStore } from '../stores/useStackStore';
import { useUIStore } from '../stores/useUIStore';
import { useAccessLensStore } from '../stores/useAccessLensStore';
import type { ClientStatus, GatewayStatus, MCPServerStatus } from '../types';

const { fitView, flowProps } = vi.hoisted(() => ({
  fitView: vi.fn(),
  flowProps: { current: {} as Record<string, unknown> },
}));

type Change = { type: string; id?: string };
function honorRemove<T extends { id: string }>(changes: Change[], items: T[]): T[] {
  const removed = new Set(changes.filter((c) => c.type === 'remove').map((c) => c.id));
  return removed.size === 0 ? items : items.filter((item) => !removed.has(item.id));
}

vi.mock('@xyflow/react', () => ({
  ReactFlow: (props: {
    children?: ReactNode;
    onKeyDown?: (event: KeyboardEvent<HTMLDivElement>) => void;
    nodes?: Array<{ id: string }>;
  }) => {
    flowProps.current = props;
    const { children, onKeyDown, nodes } = props;
    return (
    <div data-testid="react-flow" onKeyDown={onKeyDown}>
      {(nodes ?? []).map((node) => (
        <div key={node.id} className="react-flow__node" data-id={node.id} tabIndex={0}>
          {(node.id === 'mcp-github' || node.id === 'gateway') && (
            <button type="button">inner</button>
          )}
          {node.id === 'mcp-github' && <input aria-label="field" />}
        </div>
      ))}
      {children}
    </div>
    );
  },
  Background: () => null,
  BackgroundVariant: { Dots: 'dots', Lines: 'lines', Cross: 'cross' },
  Panel: ({ children }: { children?: ReactNode }) => <div>{children}</div>,
  Handle: () => null,
  Position: { Left: 'left', Right: 'right', Top: 'top', Bottom: 'bottom' },
  MarkerType: { Arrow: 'arrow', ArrowClosed: 'arrowclosed' },
  useReactFlow: () => ({ fitView, zoomIn: vi.fn(), zoomOut: vi.fn() }),
  useViewport: () => ({ zoom: 1 }),
  applyNodeChanges: (changes: Change[], nodes: Array<{ id: string }>) => honorRemove(changes, nodes),
  applyEdgeChanges: (changes: Change[], edges: Array<{ id: string }>) => honorRemove(changes, edges),
}));

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

function status(servers: MCPServerStatus[]): GatewayStatus {
  return {
    gateway: { name: 'gw', version: '0.0.0' },
    'mcp-servers': servers,
  };
}

const client: ClientStatus = {
  name: 'Claude',
  slug: 'claude',
  detected: true,
  linked: true,
  transport: 'http',
};

function node(container: HTMLElement, id: string): HTMLElement {
  const found = container.querySelector(`.react-flow__node[data-id="${id}"]`);
  if (!found) throw new Error(`missing node ${id}`);
  return found as HTMLElement;
}

describe('Canvas keyboard activation', () => {
  beforeEach(() => {
    fitView.mockClear();
    useAccessLensStore.setState({ enabled: false, clientSlug: null, draft: [] });
    useUIStore.setState({
      sidebarOpen: false,
      edgeStyle: 'default',
      compactCards: true,
      inspectorFocusRequest: null,
    });
    useStackStore.setState({
      expandedServers: new Set(),
      selectedNodeId: null,
      draggedPositions: new Map(),
      nodes: [],
      edges: [],
    });
    useStackStore.getState().setGatewayStatus(status([makeServer('github', 3)]));
    useStackStore.getState().toggleServerExpanded('mcp-github');
  });

  afterEach(() => {
    useAccessLensStore.setState({ enabled: false, clientSlug: null, draft: [] });
  });

  it('opens the inspector on Enter and Space for a focused server node', () => {
    const { container } = render(<Canvas />);
    fireEvent.keyDown(node(container, 'mcp-github'), { key: 'Enter' });
    expect(useStackStore.getState().selectedNodeId).toBe('mcp-github');
    expect(useUIStore.getState().sidebarOpen).toBe(true);

    useStackStore.setState({ selectedNodeId: null });
    useUIStore.setState({ sidebarOpen: false });
    fireEvent.keyDown(node(container, 'mcp-github'), { key: ' ' });
    expect(useStackStore.getState().selectedNodeId).toBe('mcp-github');
    expect(useUIStore.getState().sidebarOpen).toBe(true);
  });

  it('does not select a tool node, an inner button, or an input', () => {
    const { container } = render(<Canvas />);
    fireEvent.keyDown(node(container, 'tool-mcp-github-github-tool-0'), { key: 'Enter' });
    expect(useStackStore.getState().selectedNodeId).toBeNull();

    const serverButton = node(container, 'mcp-github').querySelector('button');
    fireEvent.keyDown(serverButton as HTMLElement, { key: 'Enter' });
    expect(useStackStore.getState().selectedNodeId).toBeNull();

    const gatewayButton = node(container, 'gateway').querySelector('button');
    fireEvent.keyDown(gatewayButton as HTMLElement, { key: 'Enter' });
    expect(useStackStore.getState().selectedNodeId).toBeNull();

    const field = node(container, 'mcp-github').querySelector('input');
    fireEvent.keyDown(field as HTMLElement, { key: 'Enter' });
    expect(useStackStore.getState().selectedNodeId).toBeNull();
  });

  it('toggles the Access Lens draft instead of selecting the server', () => {
    useStackStore.getState().setClients([client]);
    useStackStore.setState({ selectedNodeId: 'client-claude' });
    useAccessLensStore.setState({ enabled: true, clientSlug: 'claude', draft: [] });
    const { container } = render(<Canvas />);

    fireEvent.keyDown(node(container, 'mcp-github'), { key: 'Enter' });
    expect(useAccessLensStore.getState().draft).toContain('github');
    expect(useStackStore.getState().selectedNodeId).toBe('client-claude');
  });

  it('requests inspector focus on Enter but not on a mouse click', () => {
    const { container } = render(<Canvas />);
    fireEvent.keyDown(node(container, 'mcp-github'), { key: 'Enter' });
    expect(useUIStore.getState().inspectorFocusRequest).toBe('mcp-github');

    useStackStore.setState({ selectedNodeId: null });
    useUIStore.setState({ sidebarOpen: false, inspectorFocusRequest: null });
    const onNodeClick = flowProps.current.onNodeClick as (
      event: unknown,
      node: { id: string; data?: { type?: string } },
    ) => void;
    const github = useStackStore.getState().nodes.find((n) => n.id === 'mcp-github');
    act(() => onNodeClick({}, github as { id: string }));
    expect(useStackStore.getState().selectedNodeId).toBe('mcp-github');
    expect(useUIStore.getState().sidebarOpen).toBe(true);
    expect(useUIStore.getState().inspectorFocusRequest).toBeNull();
  });

  it('a mouse click clears a pending keyboard focus request', () => {
    useUIStore.setState({ inspectorFocusRequest: 'gateway' });
    render(<Canvas />);
    const onNodeClick = flowProps.current.onNodeClick as (
      event: unknown,
      node: { id: string; data?: { type?: string } },
    ) => void;
    const github = useStackStore.getState().nodes.find((n) => n.id === 'mcp-github');
    act(() => onNodeClick({}, github as { id: string }));
    expect(useUIStore.getState().inspectorFocusRequest).toBeNull();
  });

  it('does not request inspector focus for an Access Lens draft toggle', () => {
    useStackStore.getState().setClients([client]);
    useStackStore.setState({ selectedNodeId: 'client-claude' });
    useAccessLensStore.setState({ enabled: true, clientSlug: 'claude', draft: [] });
    const { container } = render(<Canvas />);
    fireEvent.keyDown(node(container, 'mcp-github'), { key: 'Enter' });
    expect(useAccessLensStore.getState().draft).toContain('github');
    expect(useUIStore.getState().inspectorFocusRequest).toBeNull();
  });

  it('renders a read-only canvas: no focusable edges, delete key, or connections', () => {
    render(<Canvas />);
    expect(flowProps.current.edgesFocusable).toBe(false);
    expect(flowProps.current.deleteKeyCode).toBeNull();
    expect(flowProps.current.nodesConnectable).toBe(false);
  });

  it('ignores remove changes for nodes and edges', () => {
    render(<Canvas />);
    const nodesBefore = useStackStore.getState().nodes;
    const edgesBefore = useStackStore.getState().edges;
    expect(nodesBefore.some((n) => n.id === 'mcp-github')).toBe(true);
    expect(edgesBefore.length).toBeGreaterThan(0);

    const onNodesChange = flowProps.current.onNodesChange as (changes: Change[]) => void;
    const onEdgesChange = flowProps.current.onEdgesChange as (changes: Change[]) => void;
    act(() => onNodesChange([{ type: 'remove', id: 'mcp-github' }]));
    act(() => onEdgesChange([{ type: 'remove', id: edgesBefore[0].id }]));

    expect(useStackStore.getState().nodes).toBe(nodesBefore);
    expect(useStackStore.getState().edges).toBe(edgesBefore);
  });

  it('still applies non-remove changes when a batch mixes in a removal', () => {
    render(<Canvas />);
    const onNodesChange = flowProps.current.onNodesChange as (changes: Change[]) => void;
    act(() =>
      onNodesChange([
        { type: 'remove', id: 'mcp-github' },
        { type: 'select', id: 'gateway' },
      ]),
    );
    expect(useStackStore.getState().nodes.some((n) => n.id === 'mcp-github')).toBe(true);
  });

  it('names the seven canvas controls', () => {
    const { getByRole } = render(<Canvas />);
    for (const label of [
      'Zoom in',
      'Zoom out',
      'Fit view',
      'Reset layout',
      'Switch to straight edges',
      'Switch to full cards',
      'Overlays',
    ]) {
      expect(getByRole('button', { name: label })).toBeInTheDocument();
    }
  });
});
