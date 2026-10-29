import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import type { KeyboardEvent, ReactNode } from 'react';
import { Canvas } from '../components/graph/Canvas';
import { useStackStore } from '../stores/useStackStore';
import { useUIStore } from '../stores/useUIStore';
import { useAccessLensStore } from '../stores/useAccessLensStore';
import type { ClientStatus, GatewayStatus, MCPServerStatus } from '../types';

const { fitView } = vi.hoisted(() => ({ fitView: vi.fn() }));

vi.mock('@xyflow/react', () => ({
  ReactFlow: ({
    children,
    onKeyDown,
    nodes,
  }: {
    children?: ReactNode;
    onKeyDown?: (event: KeyboardEvent<HTMLDivElement>) => void;
    nodes?: Array<{ id: string }>;
  }) => (
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
  ),
  Background: () => null,
  BackgroundVariant: { Dots: 'dots', Lines: 'lines', Cross: 'cross' },
  Panel: ({ children }: { children?: ReactNode }) => <div>{children}</div>,
  Handle: () => null,
  Position: { Left: 'left', Right: 'right', Top: 'top', Bottom: 'bottom' },
  MarkerType: { Arrow: 'arrow', ArrowClosed: 'arrowclosed' },
  useReactFlow: () => ({ fitView, zoomIn: vi.fn(), zoomOut: vi.fn() }),
  useViewport: () => ({ zoom: 1 }),
  applyNodeChanges: (_changes: unknown, nodes: unknown) => nodes,
  applyEdgeChanges: (_changes: unknown, edges: unknown) => edges,
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
    useUIStore.setState({ sidebarOpen: false, edgeStyle: 'default', compactCards: true });
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
