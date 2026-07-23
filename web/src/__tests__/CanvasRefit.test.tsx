import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
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

// The refit defers one frame before fitting; run frames synchronously so
// every fit lands inside act(), including the unmount cleanups React runs
// after each test, which is why this cannot be a per-test stub.
globalThis.requestAnimationFrame = ((cb: FrameRequestCallback) => {
  cb(0);
  return 0;
}) as typeof requestAnimationFrame;
globalThis.cancelAnimationFrame = () => {};

// The setup-file ResizeObserver polyfill, restored after tests that install a
// callback-capturing stand-in.
const SetupResizeObserver = globalThis.ResizeObserver;

// Installs a ResizeObserver stand-in whose callback is fired by hand with a
// given content width, and fakes only the debounce timer: faking rAF too
// would defer the fit-key refits into runAllTimers and pollute the resize
// assertions. The afterEach below restores both.
function installCapturingResizeObserver() {
  let cb: ResizeObserverCallback | undefined;
  class CapturingResizeObserver {
    constructor(callback: ResizeObserverCallback) {
      cb = callback;
    }
    observe() {}
    unobserve() {}
    disconnect() {}
  }
  globalThis.ResizeObserver = CapturingResizeObserver as unknown as typeof ResizeObserver;
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
  return {
    fire: (width: number) =>
      cb?.([{ contentRect: { width } } as ResizeObserverEntry], {} as ResizeObserver),
  };
}

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

  afterEach(() => {
    globalThis.ResizeObserver = SetupResizeObserver;
    vi.useRealTimers();
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

  it('refits with the current fit set when the canvas width changes', () => {
    // The detail sidebar is a grid column: opening it narrows the canvas
    // without changing node ids, so the resize observer is the only trigger.
    const { fire } = installCapturingResizeObserver();

    render(<Canvas />);
    act(() => {
      useStackStore.getState().toggleServerExpanded('mcp-github');
    });
    fitView.mockClear();

    // The first callback after observe() reports the mount-time size; it must
    // not re-frame a canvas nobody resized.
    act(() => {
      fire(1200);
      vi.runAllTimers();
    });
    expect(fitView).not.toHaveBeenCalled();

    act(() => {
      fire(880);
      vi.runAllTimers();
    });
    expect(fitView).toHaveBeenCalledTimes(1);
    // The re-frame targets the current fit set, fan-out nodes included.
    expect(fittedIds().some((id) => id.includes('tool'))).toBe(true);
  });

  it('ignores height-only resizes and debounces width bursts into one refit', () => {
    const { fire } = installCapturingResizeObserver();

    render(<Canvas />);
    fitView.mockClear();

    // A repeat of the same width is a height-only change (window snapping,
    // devtools); the viewport must hold still.
    act(() => {
      fire(1200);
      fire(1200);
      vi.runAllTimers();
    });
    expect(fitView).not.toHaveBeenCalled();

    // Dragging the sidebar resize handle streams widths; only the settled
    // size re-frames.
    act(() => {
      for (const width of [1100, 1000, 940, 880]) fire(width);
      vi.runAllTimers();
    });
    expect(fitView).toHaveBeenCalledTimes(1);
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
