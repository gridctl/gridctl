import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, act, fireEvent } from '@testing-library/react';
import type { ReactNode } from 'react';

type Vp = { x: number; y: number; zoom: number };
type MoveHandler = (event: MouseEvent | TouchEvent | null, viewport: Vp) => void;

// The viewport spies must be hoisted so the module mock below can close over
// them. paneClick and move capture the ReactFlow props so tests can click
// blank canvas and drive pan/zoom callbacks; viewport and flowSize are what
// the imperative reads return.
const { fitView, zoomIn, zoomOut, setViewport, getNode, paneClick, move, viewport, flowSize } =
  vi.hoisted(() => ({
    fitView: vi.fn(),
    zoomIn: vi.fn(),
    zoomOut: vi.fn(),
    setViewport: vi.fn(),
    getNode: vi.fn(),
    paneClick: { current: undefined as (() => void) | undefined },
    move: {
      start: undefined as MoveHandler | undefined,
      move: undefined as MoveHandler | undefined,
    },
    viewport: { current: { x: 0, y: 0, zoom: 1 } as Vp },
    flowSize: { current: { width: 1200, height: 800 } },
  }));

// Mock React Flow for every consumer in Canvas's module graph: CanvasBase
// (ReactFlow, Background, BackgroundVariant), Canvas (useReactFlow,
// useStoreApi, useViewport, Panel, applyNodeChanges, applyEdgeChanges), and
// the node components (Handle, Position).
vi.mock('@xyflow/react', () => ({
  ReactFlow: ({
    children,
    onPaneClick,
    onMoveStart,
    onMove,
  }: {
    children?: ReactNode;
    onPaneClick?: () => void;
    onMoveStart?: MoveHandler;
    onMove?: MoveHandler;
  }) => {
    paneClick.current = onPaneClick;
    move.start = onMoveStart;
    move.move = onMove;
    return <div data-testid="react-flow">{children}</div>;
  },
  Background: () => null,
  BackgroundVariant: { Dots: 'dots', Lines: 'lines', Cross: 'cross' },
  Panel: ({ children }: { children?: ReactNode }) => <div>{children}</div>,
  Handle: () => null,
  Position: { Left: 'left', Right: 'right', Top: 'top', Bottom: 'bottom' },
  useReactFlow: () => ({
    fitView,
    zoomIn,
    zoomOut,
    setViewport,
    getNode,
    getViewport: () => viewport.current,
  }),
  useStoreApi: () => ({ getState: () => flowSize.current }),
  useViewport: () => ({ zoom: 1 }),
  applyNodeChanges: (_changes: unknown, nodes: unknown) => nodes,
  applyEdgeChanges: (_changes: unknown, edges: unknown) => edges,
}));

import { Canvas } from '../components/graph/Canvas';
import { useCanvasActionsStore } from '../stores/useCanvasActionsStore';
import { useStackStore } from '../stores/useStackStore';
import { useAccessLensStore } from '../stores/useAccessLensStore';
import { useUIStore } from '../stores/useUIStore';
import type { ClientStatus, GatewayStatus, MCPServerStatus } from '../types';

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

const client: ClientStatus = {
  name: 'Claude',
  slug: 'claude',
  detected: true,
  linked: true,
  transport: 'http',
};

type Rect = { x: number; y: number; width: number; height: number };

// Per-test node placement for getNode. A rect without width/height is an
// unmeasured node at that position. Unplaced nodes report their layout
// position with a nominal measurement.
let placer: (id: string) => Partial<Rect> | undefined = () => undefined;

function lookupNode(id: string) {
  const node = useStackStore.getState().nodes.find((n) => n.id === id);
  if (!node) return undefined;
  const rect = placer(id);
  if (!rect) return { ...node, measured: node.measured ?? { width: 200, height: 60 } };
  const measured =
    rect.width !== undefined && rect.height !== undefined
      ? { width: rect.width, height: rect.height }
      : undefined;
  return {
    ...node,
    position: { x: rect.x ?? node.position.x, y: rect.y ?? node.position.y },
    measured,
  };
}

const toolId = (i: number) => `tool-mcp-github-github-tool-${i}`;

// A pane drag: a mousedown records the start viewport, then a mousemove
// shifts it well past click jitter.
function userMove() {
  act(() => {
    move.start?.(new MouseEvent('mousedown'), { x: 0, y: 0, zoom: 1 });
    move.move?.(new MouseEvent('mousemove'), { x: 40, y: 0, zoom: 1 });
  });
}

function stubReducedMotion(matches: boolean) {
  window.matchMedia = ((query: string) => ({
    matches: matches && query === '(prefers-reduced-motion: reduce)',
    media: query,
    addEventListener: () => {},
    removeEventListener: () => {},
  })) as unknown as typeof window.matchMedia;
}

function fittedIds(callIndex = -1): string[] {
  const calls = fitView.mock.calls;
  const call = calls.at(callIndex);
  const opts = call?.[0] as { nodes?: { id: string }[] } | undefined;
  return (opts?.nodes ?? []).map((n) => n.id);
}

describe('Canvas auto-refit on tool fan-out', () => {
  beforeEach(() => {
    for (const spy of [fitView, zoomIn, zoomOut, setViewport]) spy.mockClear();
    getNode.mockReset();
    getNode.mockImplementation(lookupNode);
    placer = () => undefined;
    viewport.current = { x: 0, y: 0, zoom: 1 };
    flowSize.current = { width: 1200, height: 800 };
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
    useStackStore.getState().setGatewayStatus(status([makeServer('github', 3)]));
  });

  afterEach(() => {
    globalThis.ResizeObserver = SetupResizeObserver;
    vi.useRealTimers();
    delete (window as { matchMedia?: unknown }).matchMedia;
  });

  it('registers canvas actions on mount and clears them on unmount', () => {
    const view = render(<Canvas />);
    fitView.mockClear();
    act(() => useCanvasActionsStore.getState().fitView({ padding: 0.2, duration: 300 }));
    expect(fitView).toHaveBeenCalledTimes(1);
    act(() => useCanvasActionsStore.getState().zoomIn({ duration: 200 }));
    expect(zoomIn).toHaveBeenCalledTimes(1);
    act(() => useCanvasActionsStore.getState().zoomOut({ duration: 200 }));
    expect(zoomOut).toHaveBeenCalledTimes(1);

    view.unmount();
    const calls = fitView.mock.calls.length;
    useCanvasActionsStore.getState().fitView();
    useCanvasActionsStore.getState().zoomIn();
    expect(fitView).toHaveBeenCalledTimes(calls);
    expect(zoomIn).toHaveBeenCalledTimes(1);
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
    act(() => {
      useStackStore.getState().selectNode('mcp-github');
    });
    fitView.mockClear();
    setViewport.mockClear();
    const nodesBefore = useStackStore.getState().nodes;

    act(() => {
      useStackStore.getState().setGatewayStatus(status([makeServer('github', 3)]));
    });

    expect(fitView).not.toHaveBeenCalled();
    expect(setViewport).not.toHaveBeenCalled();
    expect(useStackStore.getState().nodes).toBe(nodesBefore);
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

  it('deselects and closes the inspector on a pane click, keeping fan-outs and the viewport', () => {
    render(<Canvas />);
    act(() => {
      useStackStore.getState().toggleServerExpanded('mcp-github');
      useStackStore.getState().selectNode('mcp-github');
      useUIStore.getState().setSidebarOpen(true);
    });
    expect(useStackStore.getState().expandedServers.size).toBe(1);
    fitView.mockClear();
    setViewport.mockClear();

    act(() => {
      paneClick.current?.();
    });

    expect(useStackStore.getState().expandedServers.size).toBe(1);
    expect(
      useStackStore.getState().nodes.some((n) => (n.data as { type?: string }).type === 'tool'),
    ).toBe(true);
    expect(useStackStore.getState().selectedNodeId).toBeNull();
    expect(useUIStore.getState().sidebarOpen).toBe(false);
    expect(fitView).not.toHaveBeenCalled();
    expect(setViewport).not.toHaveBeenCalled();
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

  describe('after the user moves the viewport', () => {
    it('blocks every automatic whole-graph fit', () => {
      const { fire } = installCapturingResizeObserver();
      useStackStore.getState().setClients([client]);
      render(<Canvas />);
      act(() => {
        fire(1200);
        vi.runAllTimers();
      });
      userMove();
      fitView.mockClear();

      act(() => useStackStore.getState().toggleServerExpanded('mcp-github'));
      act(() => useStackStore.getState().toggleServerExpanded('mcp-github'));
      act(() => {
        useStackStore
          .getState()
          .setGatewayStatus(status([makeServer('github', 3), makeServer('files', 2)]));
      });
      act(() => useStackStore.getState().selectNode('client-claude'));
      act(() => {
        fire(880);
        vi.runAllTimers();
      });

      expect(fitView).not.toHaveBeenCalled();
    });

    it('treats a zoom-only wheel change as a user move', () => {
      render(<Canvas />);
      act(() => {
        move.start?.(new WheelEvent('wheel'), { x: 0, y: 0, zoom: 1 });
        move.move?.(new WheelEvent('wheel'), { x: 0, y: 0, zoom: 1.4 });
      });
      fitView.mockClear();

      act(() => useStackStore.getState().toggleServerExpanded('mcp-github'));
      expect(fitView).not.toHaveBeenCalled();
    });

    it('checks the flag when the resize debounce fires, not only when it is scheduled', () => {
      const { fire } = installCapturingResizeObserver();
      render(<Canvas />);
      act(() => fire(1200));
      fitView.mockClear();

      act(() => fire(880));
      userMove();
      act(() => vi.runAllTimers());
      expect(fitView).not.toHaveBeenCalled();
    });

    it('counts the Zoom in and Zoom out actions as user moves', () => {
      const view = render(<Canvas />);
      act(() => useCanvasActionsStore.getState().zoomIn({ duration: 200 }));
      expect(zoomIn).toHaveBeenCalledTimes(1);
      fitView.mockClear();
      act(() => useStackStore.getState().toggleServerExpanded('mcp-github'));
      expect(fitView).not.toHaveBeenCalled();
      view.unmount();

      // A remount starts with automatic fits armed; the canvas button is a
      // user move too.
      const { getByRole } = render(<Canvas />);
      fireEvent.click(getByRole('button', { name: 'Zoom out' }));
      expect(zoomOut).toHaveBeenCalledTimes(1);
      fitView.mockClear();
      act(() => useStackStore.getState().toggleServerExpanded('mcp-github'));
      expect(fitView).not.toHaveBeenCalled();
    });

    it('re-arms automatic fits on an explicit fit', () => {
      render(<Canvas />);
      userMove();
      fitView.mockClear();

      act(() => useCanvasActionsStore.getState().fitView({ padding: 0.2, duration: 300 }));
      expect(fitView).toHaveBeenCalledTimes(1);
      expect(fitView.mock.calls[0][0]).toMatchObject({ padding: 0.2, duration: 300 });
      expect(fittedIds()).toEqual([]);

      act(() => useStackStore.getState().toggleServerExpanded('mcp-github'));
      expect(fitView).toHaveBeenCalledTimes(2);
      expect(fittedIds()).toContain(toolId(0));
    });

    it('re-arms automatic fits from the Fit view button', () => {
      const { getByRole } = render(<Canvas />);
      userMove();
      fitView.mockClear();

      fireEvent.click(getByRole('button', { name: 'Fit view' }));
      expect(fitView).toHaveBeenCalledTimes(1);
      act(() => useStackStore.getState().toggleServerExpanded('mcp-github'));
      expect(fitView).toHaveBeenCalledTimes(2);
    });

    it('re-arms automatic fits on resetLayout', () => {
      render(<Canvas />);
      userMove();
      fitView.mockClear();

      act(() => useStackStore.getState().resetLayout());
      expect(fitView).toHaveBeenCalledTimes(1);

      act(() => useStackStore.getState().toggleServerExpanded('mcp-github'));
      expect(fitView).toHaveBeenCalledTimes(2);
      expect(fittedIds()).toContain(toolId(0));
    });

    it('moves nothing when the expanded tools are already on screen', () => {
      placer = (id) => {
        if (id === 'mcp-github') return { x: 100, y: 100, width: 200, height: 60 };
        if (id.startsWith('tool-')) return { x: 400, y: 100 + 40 * Number(id.at(-1)), width: 200, height: 34 };
        return undefined;
      };
      render(<Canvas />);
      userMove();
      fitView.mockClear();

      act(() => useStackStore.getState().toggleServerExpanded('mcp-github'));
      expect(fitView).not.toHaveBeenCalled();
      expect(setViewport).not.toHaveBeenCalled();
    });

    it('pans at the current zoom to reveal off-screen tools, using layout sizes before measurement', () => {
      // Tools are unmeasured on the frame they mount; their right edge at
      // x = 1100 + TOOL_WIDTH crosses the 1200 px canvas.
      placer = (id) => {
        if (id === 'mcp-github') return { x: 700, y: 100, width: 200, height: 60 };
        if (id.startsWith('tool-')) return { x: 1100, y: 100 + 40 * Number(id.at(-1)) };
        return undefined;
      };
      render(<Canvas />);
      userMove();
      fitView.mockClear();

      act(() => useStackStore.getState().toggleServerExpanded('mcp-github'));
      expect(fitView).not.toHaveBeenCalled();
      expect(setViewport).toHaveBeenCalledTimes(1);
      const [next] = setViewport.mock.calls[0] as [Vp];
      expect(next.zoom).toBe(1);
      expect(next.x).toBe(-124);
      expect(next.y).toBe(0);

      // Collapsing moves nothing.
      setViewport.mockClear();
      act(() => useStackStore.getState().toggleServerExpanded('mcp-github'));
      expect(setViewport).not.toHaveBeenCalled();
      expect(fitView).not.toHaveBeenCalled();
    });

    it('frames only the server band, capped at the current zoom, when it cannot fit', () => {
      viewport.current = { x: 0, y: 0, zoom: 1.2 };
      placer = (id) => {
        if (id === 'mcp-github') return { x: 0, y: 100, width: 200, height: 60 };
        if (id.startsWith('tool-')) return { x: 1400, y: 100 + 40 * Number(id.at(-1)), width: 200, height: 34 };
        return undefined;
      };
      render(<Canvas />);
      userMove();
      fitView.mockClear();

      act(() => useStackStore.getState().toggleServerExpanded('mcp-github'));
      expect(setViewport).not.toHaveBeenCalled();
      expect(fitView).toHaveBeenCalledTimes(1);
      expect(fitView.mock.calls[0][0]).toMatchObject({ maxZoom: 1.2 });
      expect(fittedIds().sort()).toEqual(['mcp-github', toolId(0), toolId(1), toolId(2)].sort());
    });
  });

  describe('motions that are not user moves', () => {
    it('keeps automatic fits armed after a blank-pane click', () => {
      const { fire } = installCapturingResizeObserver();
      render(<Canvas />);
      act(() => fire(1200));
      fitView.mockClear();

      act(() => {
        move.start?.(new MouseEvent('mousedown'), { x: 0, y: 0, zoom: 1 });
        paneClick.current?.();
      });
      act(() => useStackStore.getState().toggleServerExpanded('mcp-github'));
      expect(fittedIds()).toContain(toolId(0));

      fitView.mockClear();
      act(() => {
        useStackStore
          .getState()
          .setGatewayStatus(status([makeServer('github', 3), makeServer('files', 2)]));
      });
      expect(fittedIds()).toContain('mcp-files');

      fitView.mockClear();
      act(() => {
        fire(880);
        vi.runAllTimers();
      });
      expect(fitView).toHaveBeenCalledTimes(1);
    });

    it('tolerates 1 px of click jitter', () => {
      render(<Canvas />);
      act(() => {
        move.start?.(new MouseEvent('mousedown'), { x: 0, y: 0, zoom: 1 });
        move.move?.(new MouseEvent('mousemove'), { x: 1, y: 0, zoom: 1 });
        paneClick.current?.();
      });
      fitView.mockClear();

      act(() => useStackStore.getState().toggleServerExpanded('mcp-github'));
      expect(fittedIds()).toContain(toolId(0));
    });

    it('ignores programmatic moves reported with a null event', () => {
      render(<Canvas />);
      act(() => {
        move.start?.(null, { x: 0, y: 0, zoom: 1 });
        move.move?.(null, { x: 200, y: 0, zoom: 0.5 });
      });
      fitView.mockClear();
      act(() => useStackStore.getState().toggleServerExpanded('mcp-github'));
      expect(fittedIds()).toContain(toolId(0));
    });

    it('ignores a null-event move after a blank click started a gesture', () => {
      render(<Canvas />);
      act(() => {
        move.start?.(new MouseEvent('mousedown'), { x: 0, y: 0, zoom: 1 });
        move.move?.(null, { x: 300, y: 50, zoom: 0.6 });
      });
      fitView.mockClear();
      act(() => useStackStore.getState().toggleServerExpanded('mcp-github'));
      expect(fittedIds()).toContain(toolId(0));
    });

    it('drops the recorded start when a programmatic move begins', () => {
      render(<Canvas />);
      act(() => {
        move.start?.(new MouseEvent('mousedown'), { x: 0, y: 0, zoom: 1 });
        move.start?.(null, { x: 0, y: 0, zoom: 1 });
        move.move?.(new MouseEvent('mousemove'), { x: 300, y: 0, zoom: 1 });
      });
      fitView.mockClear();
      act(() => useStackStore.getState().toggleServerExpanded('mcp-github'));
      expect(fittedIds()).toContain(toolId(0));
    });
  });

  describe('selection reveal', () => {
    it('pans an off-screen selected node into view at the current zoom', () => {
      placer = (id) => (id === 'mcp-github' ? { x: 1500, y: 300, width: 200, height: 60 } : undefined);
      render(<Canvas />);
      fitView.mockClear();

      act(() => useStackStore.getState().selectNode('mcp-github'));
      expect(fitView).not.toHaveBeenCalled();
      expect(setViewport).toHaveBeenCalledTimes(1);
      const [next] = setViewport.mock.calls[0] as [Vp];
      expect(next.zoom).toBe(1);
      expect(next.x).toBe(1200 - 24 - 1700);
      expect(next.y).toBe(0);
    });

    it('leaves a fully visible selected node alone', () => {
      placer = (id) => (id === 'mcp-github' ? { x: 300, y: 300, width: 200, height: 60 } : undefined);
      render(<Canvas />);
      fitView.mockClear();

      act(() => useStackStore.getState().selectNode('mcp-github'));
      expect(setViewport).not.toHaveBeenCalled();
      expect(fitView).not.toHaveBeenCalled();
    });

    it('yields to the subgraph fit when a client selection re-keys it', () => {
      // A resource sits outside the client's reach, so its subgraph differs
      // from the whole graph and the selection re-keys the fit.
      useStackStore.getState().setGatewayStatus({
        ...status([makeServer('github', 3)]),
        resources: [{ name: 'db', image: 'postgres', status: 'running' }],
      });
      useStackStore.getState().setClients([client]);
      placer = (id) => (id === 'client-claude' ? { x: -900, y: 300, width: 200, height: 60 } : undefined);
      render(<Canvas />);
      fitView.mockClear();

      act(() => useStackStore.getState().selectNode('client-claude'));
      expect(setViewport).not.toHaveBeenCalled();
      expect(fitView).toHaveBeenCalledTimes(1);
      expect(fittedIds()).toContain('client-claude');
      expect(fittedIds()).not.toContain('resource-db');
    });

    it('reveals a client selection after a user move instead of fitting', () => {
      useStackStore.getState().setClients([client]);
      placer = (id) => (id === 'client-claude' ? { x: -900, y: 300, width: 200, height: 60 } : undefined);
      render(<Canvas />);
      userMove();
      fitView.mockClear();

      act(() => useStackStore.getState().selectNode('client-claude'));
      expect(fitView).not.toHaveBeenCalled();
      expect(setViewport).toHaveBeenCalledTimes(1);
      const [next] = setViewport.mock.calls[0] as [Vp];
      expect(next).toEqual({ x: 924, y: 0, zoom: 1 });
    });
  });

  describe('reduced motion', () => {
    it('passes duration 0 to every canvas viewport call', () => {
      stubReducedMotion(true);
      placer = (id) => (id === 'mcp-github' ? { x: 1500, y: 300, width: 200, height: 60 } : undefined);
      render(<Canvas />);
      fitView.mockClear();

      act(() => useStackStore.getState().toggleServerExpanded('mcp-github'));
      expect(fitView.mock.calls.at(-1)?.[0]).toMatchObject({ duration: 0 });
      act(() => useCanvasActionsStore.getState().fitView({ padding: 0.2, duration: 300 }));
      expect(fitView.mock.calls.at(-1)?.[0]).toMatchObject({ duration: 0 });
      act(() => useCanvasActionsStore.getState().zoomIn({ duration: 200 }));
      expect(zoomIn).toHaveBeenLastCalledWith({ duration: 0 });
      act(() => useCanvasActionsStore.getState().zoomOut({ duration: 200 }));
      expect(zoomOut).toHaveBeenLastCalledWith({ duration: 0 });
      act(() => useStackStore.getState().selectNode('mcp-github'));
      expect(setViewport.mock.calls.at(-1)?.[1]).toEqual({ duration: 0 });
    });

    it('keeps the current durations without the preference', () => {
      stubReducedMotion(false);
      render(<Canvas />);
      fitView.mockClear();

      act(() => useStackStore.getState().toggleServerExpanded('mcp-github'));
      expect(fitView.mock.calls.at(-1)?.[0]).toMatchObject({ duration: 400 });
      act(() => useCanvasActionsStore.getState().fitView({ padding: 0.2, duration: 300 }));
      expect(fitView.mock.calls.at(-1)?.[0]).toMatchObject({ duration: 300 });
      act(() => useCanvasActionsStore.getState().zoomIn({ duration: 200 }));
      expect(zoomIn).toHaveBeenLastCalledWith({ duration: 200 });
    });
  });
});
