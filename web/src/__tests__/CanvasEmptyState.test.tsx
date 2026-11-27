import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, act, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import type { ReactNode } from 'react';

// Mock React Flow for every consumer in Canvas's module graph, plus
// NodeToolbar so the empty-state card renders with its anchor node id.
vi.mock('@xyflow/react', () => ({
  ReactFlow: ({ children }: { children?: ReactNode }) => (
    <div data-testid="react-flow">{children}</div>
  ),
  Background: () => null,
  BackgroundVariant: { Dots: 'dots', Lines: 'lines', Cross: 'cross' },
  Panel: ({ children }: { children?: ReactNode }) => <div>{children}</div>,
  Handle: () => null,
  NodeToolbar: ({
    children,
    nodeId,
    isVisible,
    position,
    offset,
  }: {
    children?: ReactNode;
    nodeId?: string;
    isVisible?: boolean;
    position?: string;
    offset?: number;
  }) => (
    <div
      data-testid="node-toolbar"
      data-node-id={nodeId}
      data-visible={String(isVisible)}
      data-position={position}
      data-offset={String(offset)}
    >
      {children}
    </div>
  ),
  Position: { Left: 'left', Right: 'right', Top: 'top', Bottom: 'bottom' },
  MarkerType: { Arrow: 'arrow', ArrowClosed: 'arrowclosed' },
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
import { useWizardStore } from '../stores/useWizardStore';
import type { GatewayStatus, MCPServerStatus } from '../types';

globalThis.requestAnimationFrame = ((cb: FrameRequestCallback) => {
  cb(0);
  return 0;
}) as typeof requestAnimationFrame;
globalThis.cancelAnimationFrame = () => {};

const gateway = { name: 'gw', version: '0.0.0' };

const server: MCPServerStatus = {
  name: 'github',
  transport: 'http',
  initialized: true,
  toolCount: 1,
  tools: ['github-tool-0'],
};

function emptyStatus(extra: Partial<GatewayStatus> = {}): GatewayStatus {
  return { gateway, 'mcp-servers': [], ...extra };
}

function applyStatus(status: GatewayStatus) {
  act(() => {
    useStackStore.getState().setGatewayStatus(status);
  });
}

const createNew = () => screen.queryByRole('button', { name: 'Create new' });

describe('Canvas empty-stack call to action', () => {
  beforeEach(() => {
    useStackStore.setState({
      gatewayInfo: null,
      mcpServers: [],
      resources: [],
      clients: [],
      nodes: [],
      edges: [],
      stackName: '',
      isLoading: true,
      error: null,
      connectionStatus: 'disconnected',
      selectedNodeId: null,
      expandedServers: new Set(),
      draggedPositions: new Map(),
    });
    useWizardStore.getState().reset();
    useWizardStore.getState().close();
  });

  it('shows Create new beside the gateway node for a stackless gateway', () => {
    render(<Canvas />);
    applyStatus(emptyStatus());

    expect(screen.getByRole('button', { name: 'Create new' })).toBeInTheDocument();
    const toolbar = screen.getByTestId('node-toolbar');
    expect(toolbar).toHaveAttribute('data-node-id', 'gateway');
    expect(toolbar).toHaveAttribute('data-visible', 'true');
    expect(toolbar).toHaveAttribute('data-position', 'right');
    expect(toolbar).toHaveAttribute('data-offset', '24');
    expect(screen.getByRole('heading', { level: 2, name: 'No stack loaded' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'MCP Server' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Resource' })).toBeNull();
  });

  it('opts the card out of canvas pan, wheel, and drag gestures', () => {
    render(<Canvas />);
    applyStatus(emptyStatus());

    const card = screen.getByTestId('stack-empty-state');
    expect(card).toHaveClass('nopan', 'nowheel', 'nodrag');
  });

  it('offers MCP Server and Resource quick-add links with a loaded stack', () => {
    render(<Canvas />);
    applyStatus(emptyStatus({ stack_name: 'demo' }));

    expect(screen.getByRole('heading', { level: 2, name: 'This stack is empty' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'MCP Server' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Resource' })).toBeInTheDocument();
  });

  it('is absent with one MCP server', () => {
    render(<Canvas />);
    applyStatus(emptyStatus({ 'mcp-servers': [server] }));

    expect(createNew()).toBeNull();
  });

  it('is absent with one resource', () => {
    render(<Canvas />);
    applyStatus(emptyStatus({ resources: [{ name: 'db', image: 'postgres', status: 'running' }] }));

    expect(createNew()).toBeNull();
  });

  it('is absent while loading', () => {
    render(<Canvas />);
    applyStatus(emptyStatus());
    act(() => {
      useStackStore.setState({ isLoading: true });
    });

    expect(createNew()).toBeNull();
  });

  it('is absent in an error state', () => {
    render(<Canvas />);
    applyStatus(emptyStatus());
    act(() => {
      useStackStore.getState().setError('Gateway connection unavailable');
    });

    expect(createNew()).toBeNull();
  });

  it('is absent before the first status', () => {
    render(<Canvas />);

    expect(createNew()).toBeNull();
  });

  it('opens the wizard at the type picker from Create new', () => {
    render(<Canvas />);
    applyStatus(emptyStatus());

    fireEvent.click(screen.getByRole('button', { name: 'Create new' }));

    const wizard = useWizardStore.getState();
    expect(wizard.isOpen).toBe(true);
    expect(wizard.selectedType).toBeNull();
    expect(wizard.currentStep).toBe('type');
  });

  it('opens the MCP server template step from the quick-add link', () => {
    render(<Canvas />);
    applyStatus(emptyStatus({ stack_name: 'demo' }));

    fireEvent.click(screen.getByRole('button', { name: 'MCP Server' }));

    const wizard = useWizardStore.getState();
    expect(wizard.isOpen).toBe(true);
    expect(wizard.selectedType).toBe('mcp-server');
    expect(wizard.currentStep).toBe('template');
  });

  it('opens the resource template step from the quick-add link', () => {
    render(<Canvas />);
    applyStatus(emptyStatus({ stack_name: 'demo' }));

    fireEvent.click(screen.getByRole('button', { name: 'Resource' }));

    const wizard = useWizardStore.getState();
    expect(wizard.selectedType).toBe('resource');
    expect(wizard.currentStep).toBe('template');
  });

  it('no longer says New Stack', () => {
    render(<Canvas />);
    applyStatus(emptyStatus());

    expect(screen.queryByText('New Stack')).toBeNull();
  });
});
