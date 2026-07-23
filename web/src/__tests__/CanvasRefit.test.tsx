import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, act } from '@testing-library/react';
import type { ReactNode } from 'react';

// The fitView spy must be hoisted so the module mock below can close over it.
const { fitView } = vi.hoisted(() => ({ fitView: vi.fn() }));

// Mock React Flow for every consumer in Canvas's module graph: CanvasBase
// (ReactFlow, Background, BackgroundVariant), Canvas (useReactFlow,
// useViewport, Panel), the node components (Handle, Position), the store
// (applyNodeChanges, applyEdgeChanges), and edge building (MarkerType).
vi.mock('@xyflow/react', () => ({
  ReactFlow: ({ children }: { children?: ReactNode }) => (
    <div data-testid="react-flow">{children}</div>
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

import { Canvas } from '../components/graph/Canvas';
import { useStackStore } from '../stores/useStackStore';
import { useAccessLensStore } from '../stores/useAccessLensStore';
import type { GatewayStatus, MCPServerStatus } from '../types';

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

function fittedIds(callIndex = -1): string[] {
  const calls = fitView.mock.calls;
  const call = calls.at(callIndex);
  const opts = call?.[0] as { nodes?: { id: string }[] } | undefined;
  return (opts?.nodes ?? []).map((n) => n.id);
}

describe('Canvas auto-refit on tool fan-out', () => {
  beforeEach(() => {
    fitView.mockClear();
    useStackStore.setState({
      expandedServers: new Set(),
      selectedNodeId: null,
      draggedPositions: new Map(),
    });
    useStackStore.getState().setGatewayStatus(status([makeServer('github', 3)]));
  });

  it('refits with the fan-out node ids when a server expands with nothing selected', () => {
    render(<Canvas />);
    fitView.mockClear();

    act(() => {
      useStackStore.getState().toggleServerExpanded('mcp-github');
    });

    expect(fitView).toHaveBeenCalled();
    const toolIds = useStackStore
      .getState()
      .nodes.filter((n) => (n.data as { type?: string }).type === 'tool')
      .map((n) => n.id);
    expect(toolIds.length).toBe(3);
    const ids = fittedIds();
    for (const toolId of toolIds) {
      expect(ids).toContain(toolId);
    }
  });

  it('does not refit on a polling refresh with an unchanged node set', () => {
    render(<Canvas />);
    act(() => {
      useStackStore.getState().toggleServerExpanded('mcp-github');
    });
    fitView.mockClear();

    act(() => {
      useStackStore.getState().setGatewayStatus(status([makeServer('github', 3)]));
    });

    expect(fitView).not.toHaveBeenCalled();
  });

  it('refits after resetLayout even though the node id set is unchanged', () => {
    render(<Canvas />);
    fitView.mockClear();

    // The reset button and the compact-cards toggle both recompute every
    // position through resetLayout without adding or removing nodes.
    act(() => {
      useStackStore.getState().resetLayout();
    });

    expect(fitView).toHaveBeenCalled();
  });

  it('refits on expansion during Access Lens editing, but not on scope toggles', () => {
    useStackStore.setState({ selectedNodeId: 'client-claude' });
    useAccessLensStore.setState({ enabled: true, clientSlug: 'claude' });
    try {
      render(<Canvas />);
      fitView.mockClear();

      // A draft grant/revoke changes highlighting only; the canvas holds still.
      act(() => {
        useAccessLensStore.getState().toggleServer('github');
      });
      expect(fitView).not.toHaveBeenCalled();

      // Expanding a server changes the node set; the fan-out must come into frame.
      act(() => {
        useStackStore.getState().toggleServerExpanded('mcp-github');
      });
      expect(fitView).toHaveBeenCalled();
    } finally {
      useAccessLensStore.setState({ enabled: false, clientSlug: null });
    }
  });

  it('refits without the tool ids after collapsing', () => {
    render(<Canvas />);
    act(() => {
      useStackStore.getState().toggleServerExpanded('mcp-github');
    });
    fitView.mockClear();

    act(() => {
      useStackStore.getState().toggleServerExpanded('mcp-github');
    });

    expect(fitView).toHaveBeenCalled();
    const ids = fittedIds();
    expect(ids.length).toBeGreaterThan(0);
    expect(ids.some((id) => id.includes('tool'))).toBe(false);
  });
});
