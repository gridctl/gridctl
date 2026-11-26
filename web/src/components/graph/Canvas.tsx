import { useCallback, useEffect, useMemo, useRef, type KeyboardEvent, type MouseEvent } from 'react';
import {
  Panel,
  BackgroundVariant,
  applyEdgeChanges,
  applyNodeChanges,
  useReactFlow,
  useStoreApi,
  type EdgeChange,
  type Node,
  type NodeChange,
  type OnMove,
  type OnMoveStart,
  type Viewport,
} from '@xyflow/react';
import { RotateCcw, Spline, Minus, Plus, Maximize, Rows3, LayoutGrid, Layers, Server, Database } from 'lucide-react';

import { nodeTypes } from './nodeTypes';
import { useStackStore } from '../../stores/useStackStore';
import {
  useCanvasActionsStore,
  type CanvasViewportOptions,
  type CanvasZoomOptions,
} from '../../stores/useCanvasActionsStore';
import { useUIStore } from '../../stores/useUIStore';
import { useWizardStore } from '../../stores/useWizardStore';
import { useAccessLensStore, buildDraftScope, flattenTools, isDirty } from '../../stores/useAccessLensStore';
import { useThemeColors } from '../../themes/useTheme';
import { usePathHighlight } from '../../hooks/usePathHighlight';
import { cn } from '../../lib/cn';
import { applyEdgeThemeColors } from '../../lib/graph/edges';
import { getNodeDimensions } from '../../lib/graph/utils';
import { isRectVisible, revealViewport, unionRect, type FlowRect } from '../../lib/graph/viewport';
import { LAYOUT, NODE_TYPES } from '../../lib/constants';
import { motionDuration } from '../../lib/motion';
import { buildStackRows, isFilterActive, matchesFilter, type StackFilter } from '../../lib/stackRows';
import { CanvasBase } from '../canvas/CanvasBase';
import { SpecModeOverlay } from '../spec/SpecModeOverlay';
import { OverlaysMenu } from './OverlaysMenu';
import { CanvasLegend } from './CanvasLegend';
import { SubGrid } from './SubGrid';
import { requestInspectorLeave } from '../../stores/useInspectorToolsGuardStore';

const MAX_ANIMATED_EDGES = 12;
const GATEWAY_MCP_EDGE_PREFIX = 'edge-gateway-mcp-';

function serverNameFromEdge(edgeId: string): string {
  return edgeId.startsWith(GATEWAY_MCP_EDGE_PREFIX)
    ? edgeId.slice(GATEWAY_MCP_EDGE_PREFIX.length)
    : '';
}

function gatedAnimatedIds(
  edges: Array<{ id: string }>,
  hasSelection: boolean,
  highlighted: Set<string>,
  activeServers: Set<string>,
): Set<string> {
  if (hasSelection) return highlighted;
  const ids = new Set<string>();
  for (const edge of edges) {
    if (ids.size >= MAX_ANIMATED_EDGES) break;
    const serverName = serverNameFromEdge(edge.id);
    if (serverName && activeServers.has(serverName)) ids.add(edge.id);
  }
  return ids;
}

// A user move translates by the pointer delta, and React Flow's default
// paneClickDistance lets a click jitter by 1 px; squared to skip the root.
const CLICK_TOLERANCE_SQ = 1;

// Flow-space bounds of a node. A node added this frame is not measured yet,
// so fall back to its layout size.
function nodeRect(node: Node): FlowRect {
  const fallback =
    node.type === NODE_TYPES.TOOL || node.type === NODE_TYPES.TOOL_OVERFLOW
      ? { width: LAYOUT.TOOL_WIDTH, height: LAYOUT.TOOL_HEIGHT }
      : getNodeDimensions(node, useUIStore.getState().compactCards);
  return {
    x: node.position.x,
    y: node.position.y,
    width: node.measured?.width ?? fallback.width,
    height: node.measured?.height ?? fallback.height,
  };
}

type ActivatableNode = {
  id: string;
  data?: { type?: string; name?: string };
};

interface CanvasProps {
  /** Stack toolbar filter. Non-matching nodes and their edges fade; none are removed. */
  filter?: StackFilter;
}

export function Canvas({ filter }: CanvasProps = {}) {
  const nodes = useStackStore((s) => s.nodes);
  const edges = useStackStore((s) => s.edges);
  const setNodes = useStackStore((s) => s.setNodes);
  const setEdges = useStackStore((s) => s.setEdges);
  const recordDraggedPositions = useStackStore((s) => s.recordDraggedPositions);
  const selectNode = useStackStore((s) => s.selectNode);
  const selectedNodeId = useStackStore((s) => s.selectedNodeId);
  const resetLayout = useStackStore((s) => s.resetLayout);
  const connectionStatus = useStackStore((s) => s.connectionStatus);
  const gatewayInfo = useStackStore((s) => s.gatewayInfo);
  const setSidebarOpen = useUIStore((s) => s.setSidebarOpen);
  const requestInspectorFocus = useUIStore((s) => s.requestInspectorFocus);
  const edgeStyle = useUIStore((s) => s.edgeStyle);
  const toggleEdgeStyle = useUIStore((s) => s.toggleEdgeStyle);
  const compactCards = useUIStore((s) => s.compactCards);
  const toggleCompactCards = useUIStore((s) => s.toggleCompactCards);
  // Read only for the overlay mount below; the toggles live in OverlaysMenu.
  const showSpecMode = useUIStore((s) => s.showSpecMode);

  // Access Lens: a draft per-client scope edited on the canvas. When active for
  // the selected client, the highlight previews the draft instead of the saved
  // scope, and clicking a server node grants/revokes it in the draft.
  const mcpServers = useStackStore((s) => s.mcpServers);
  const activeServers = useStackStore((s) => s.activeServers);
  const lensEnabled = useAccessLensStore((s) => s.enabled);
  const lensClientSlug = useAccessLensStore((s) => s.clientSlug);
  const lensDraft = useAccessLensStore((s) => s.draft);
  const lensToolMode = useAccessLensStore((s) => s.toolMode);
  const lensCustomSel = useAccessLensStore((s) => s.customSel);
  const lensSavedTools = useAccessLensStore((s) => s.savedTools);
  const lensBaselineTools = useAccessLensStore((s) => s.baselineTools);
  const lensToolsTouched = useAccessLensStore((s) => s.toolsTouched);
  const toggleDraftServer = useAccessLensStore((s) => s.toggleServer);

  // The lens edits exactly the selected client; a mismatch (or no client
  // selected) means the canvas behaves normally.
  const lensActive =
    lensEnabled && lensClientSlug != null && selectedNodeId === `client-${lensClientSlug}`;

  // Re-light the canvas against the live draft, including per-server tool
  // narrowing. Use the same tool list a commit would write: the flattened
  // All/Custom intent when the operator has touched a tool group, else the saved
  // list (an untouched commit omits the axis, so the backend keeps savedTools).
  const scopeOverride = useMemo(() => {
    if (!lensActive) return null;
    const serverTools = Object.fromEntries(mcpServers.map((s) => [s.name, s.tools ?? []]));
    const flat = flattenTools(lensDraft, serverTools, lensToolMode, lensCustomSel);
    const toolsDirty = lensToolsTouched && isDirty(flat, lensBaselineTools);
    const effective = toolsDirty ? flat : lensSavedTools;
    return buildDraftScope(lensDraft, mcpServers, effective);
  }, [
    lensActive,
    lensDraft,
    mcpServers,
    lensToolMode,
    lensCustomSel,
    lensToolsTouched,
    lensBaselineTools,
    lensSavedTools,
  ]);

  const { zoomIn, zoomOut, fitView, getViewport, setViewport, getNode } = useReactFlow();
  const flowStore = useStoreApi();

  // Whether the user has moved the viewport since the last explicit fit.
  // While set, automatic fits are skipped so the chosen viewport survives.
  // React Flow reports programmatic moves (fits, setViewport, zoomIn/zoomOut)
  // with a null event, and a pane mousedown or tap starts a gesture with an
  // event before anything moves, so only a non-null onMove whose viewport
  // left the recorded start by more than click jitter counts. Refs only:
  // onMove runs on every frame of a drag and of every animated fit.
  const userMovedRef = useRef(false);
  const moveStartRef = useRef<Viewport | null>(null);
  const onMoveStart = useCallback<OnMoveStart>((event, viewport) => {
    moveStartRef.current = event ? viewport : null;
  }, []);
  const onMove = useCallback<OnMove>((event, viewport) => {
    const start = moveStartRef.current;
    if (!event || !start || userMovedRef.current) return;
    const dx = viewport.x - start.x;
    const dy = viewport.y - start.y;
    if (viewport.zoom !== start.zoom || dx * dx + dy * dy > CLICK_TOLERANCE_SQ) {
      userMovedRef.current = true;
    }
  }, []);

  // React Flow controls, wrapped so every caller (canvas buttons, shell
  // shortcuts, palette) shares the user-moved bookkeeping and reduced motion.
  // An explicit whole-graph fit re-arms automatic fits; framing a node subset
  // (the toolbar's Enter) and a zoom step are user moves, so a later resize
  // does not re-frame the whole graph over them.
  const fit = useCallback((options?: CanvasViewportOptions) => {
    userMovedRef.current = options?.nodes !== undefined;
    void fitView({ ...options, duration: motionDuration(options?.duration ?? 0) });
  }, [fitView]);
  const zoomInUser = useCallback((options?: CanvasZoomOptions) => {
    userMovedRef.current = true;
    void zoomIn({ duration: motionDuration(options?.duration ?? 0) });
  }, [zoomIn]);
  const zoomOutUser = useCallback((options?: CanvasZoomOptions) => {
    userMovedRef.current = true;
    void zoomOut({ duration: motionDuration(options?.duration ?? 0) });
  }, [zoomOut]);
  const autoFit = useCallback((ids: { id: string }[]) => {
    void fitView({ nodes: ids, padding: 0.25, duration: motionDuration(400), maxZoom: 1.5 });
  }, [fitView]);

  // Registered for shell shortcuts that live outside this tree.
  const registerCanvasActions = useCanvasActionsStore((s) => s.register);
  const clearCanvasActions = useCanvasActionsStore((s) => s.clear);
  useEffect(() => {
    registerCanvasActions({ fitView: fit, zoomIn: zoomInUser, zoomOut: zoomOutUser });
    return () => clearCanvasActions();
  }, [fit, zoomInUser, zoomOutUser, registerCanvasActions, clearCanvasActions]);

  // The canvas size as laid out now. A frame callback runs before
  // ResizeObserver notifications, so React Flow's stored size is still the
  // pre-inspector width when a selection opens the sidebar in the same
  // commit; reading the wrapper forces layout and is current. The store
  // covers an unlaid-out wrapper (jsdom).
  const wrapperRef = useRef<HTMLDivElement>(null);
  const canvasSize = useCallback(() => {
    const el = wrapperRef.current;
    if (el && el.clientWidth > 0 && el.clientHeight > 0) {
      return { width: el.clientWidth, height: el.clientHeight };
    }
    const { width, height } = flowStore.getState();
    return { width, height };
  }, [flowStore]);

  // Pans by the minimum amount at the current zoom so `rect` is on screen.
  // Returns false only when it cannot fit at this zoom, leaving the fallback
  // to the caller; an unmeasured canvas moves nothing. Reads the viewport and
  // the canvas size imperatively so the canvas never re-renders on pan.
  const panToReveal = useCallback((rect: FlowRect): boolean => {
    const { width, height } = canvasSize();
    if (!(width > 0 && height > 0)) return true;
    const viewport = getViewport();
    if (isRectVisible(rect, viewport, { width, height })) return true;
    const next = revealViewport(rect, viewport, { width, height });
    if (!next) return false;
    void setViewport(next, { duration: motionDuration(300) });
    return true;
  }, [canvasSize, getViewport, setViewport]);

  // The canvas is read-only: the graph mirrors the running stack, so a
  // removal from React Flow would only hide an element until the next poll.
  const onNodesChange = useCallback((changes: NodeChange[]) => {
    const kept = changes.filter((change) => change.type !== 'remove');
    if (kept.length === 0) return;
    const store = useStackStore.getState();
    setNodes(applyNodeChanges(kept, store.nodes));
    recordDraggedPositions(kept);
  }, [setNodes, recordDraggedPositions]);

  const onEdgesChange = useCallback((changes: EdgeChange[]) => {
    const kept = changes.filter((change) => change.type !== 'remove');
    if (kept.length === 0) return;
    setEdges(applyEdgeChanges(kept, useStackStore.getState().edges));
  }, [setEdges]);
  // Live theme colors so JS-set edge/grid colors follow [data-theme].
  const colors = useThemeColors();

  // Reset layout when compact cards toggle changes
  const prevCompactRef = useRef(compactCards);
  useEffect(() => {
    if (prevCompactRef.current !== compactCards) {
      prevCompactRef.current = compactCards;
      resetLayout();
    }
  }, [compactCards, resetLayout]);

  // Path highlighting for selected agents. In Access Lens, the draft scope
  // override re-lights the canvas live against unsaved edits.
  const highlightState = usePathHighlight(nodes, edges, selectedNodeId, scopeOverride);

  // Auto-fit the view whenever the visible layout changes, until the user
  // moves the viewport. With a client focused the fit key is its highlighted
  // reachable subgraph; otherwise it is every node id, so expanding or
  // collapsing a server's tool fan-out (or servers appearing on hot reload)
  // re-frames the whole graph at max zoom. The layout epoch folds in
  // resetLayout (reset button, compact-cards toggle), which recomputes every
  // position without changing the id set; an epoch change is an explicit
  // request, so it re-arms automatic fits before the guard. Keying on epoch
  // plus sorted ids keeps the canvas still during status polls and node
  // drags, which change neither.
  //
  // Access Lens uses the whole-graph key even though a client is selected:
  // the operator can grant servers outside the current reach, so every server
  // must be in frame, and scope toggles only change highlighting, never node
  // ids, so draft edits still hold the canvas still while expand/collapse
  // re-frames.
  const layoutEpoch = useStackStore((s) => s.layoutEpoch);
  const fitKey = useMemo(() => {
    const selectedNode = nodes.find((n) => n.id === selectedNodeId);
    const isClientSelected =
      (selectedNode?.data as { type?: string } | undefined)?.type === 'client';
    const fitIds =
      isClientSelected && !lensActive
        ? [...highlightState.highlightedNodeIds].sort().join(',')
        : (nodes ?? []).map((n) => n.id).sort().join(',');
    return fitIds ? `${layoutEpoch}:${fitIds}` : '';
  }, [nodes, selectedNodeId, highlightState, lensActive, layoutEpoch]);
  const fitSetRef = useRef<{ id: string }[]>([]);
  const prevEpochRef = useRef(layoutEpoch);
  useEffect(() => {
    if (prevEpochRef.current !== layoutEpoch) {
      prevEpochRef.current = layoutEpoch;
      userMovedRef.current = false;
    }
    if (!fitKey) return;
    const ids = fitKey.slice(fitKey.indexOf(':') + 1).split(',').map((id) => ({ id }));
    fitSetRef.current = ids;
    if (userMovedRef.current) return;
    // fitView queues internally until new nodes are measured (React Flow
    // 12.5+), but it never re-reads a container resized in the same commit:
    // selecting a client opens the sidebar grid column synchronously with
    // this effect, so defer one frame to let React Flow's ResizeObserver
    // record the narrowed width before framing.
    const raf = requestAnimationFrame(() => {
      if (!userMovedRef.current) autoFit(ids);
    });
    return () => cancelAnimationFrame(raf);
  }, [fitKey, layoutEpoch, autoFit]);

  // Re-frame the current fit set when the canvas container's width changes.
  // The detail sidebar is a grid column, so opening, closing, or dragging it
  // resizes the canvas without touching the fit key (node ids and epoch;
  // under Access Lens the key is even byte-identical across client
  // selection), and React Flow never auto-fits on container resize. Gated on
  // width so height-only changes hold the viewport still, and debounced so
  // the sidebar resize handle re-frames once per gesture. The first
  // observation reports the mount-time size and must not re-frame a canvas
  // nobody resized. A user-moved viewport is kept; the flag is read when the
  // debounce fires, since the user may move during the wait.
  useEffect(() => {
    const el = wrapperRef.current;
    if (!el) return;
    let lastWidth = -1;
    let timer: number | undefined;
    const observer = new ResizeObserver((entries) => {
      const width = entries[0]?.contentRect.width ?? el.clientWidth;
      if (width === lastWidth) return;
      const isInitial = lastWidth === -1;
      lastWidth = width;
      if (isInitial) return;
      window.clearTimeout(timer);
      timer = window.setTimeout(() => {
        const ids = fitSetRef.current;
        if (ids.length === 0 || userMovedRef.current) return;
        autoFit(ids);
      }, 150);
    });
    observer.observe(el);
    return () => {
      window.clearTimeout(timer);
      observer.disconnect();
    };
  }, [autoFit]);

  // Bring a selected node into view at the current zoom. Selection often
  // comes from outside the canvas (palette, toasts, links) for a node the
  // user has panned away from. Deferred one frame so the inspector column has
  // laid out; panToReveal measures the wrapper, not React Flow's stored size,
  // which lags that frame. Before any user move, a selection that
  // also re-keys the fit (a client outside Access Lens) is framed by the
  // fit-key effect alone: two viewport calls in one frame interrupt each
  // other. Previous-value refs, not effect order, decide what changed.
  const prevSelectedRef = useRef(selectedNodeId);
  const prevFitKeyRef = useRef(fitKey);
  useEffect(() => {
    const selectionChanged = prevSelectedRef.current !== selectedNodeId;
    const fitKeyChanged = prevFitKeyRef.current !== fitKey;
    prevSelectedRef.current = selectedNodeId;
    prevFitKeyRef.current = fitKey;
    if (!selectionChanged || !selectedNodeId) return;
    if (!userMovedRef.current && fitKeyChanged) return;
    const id = selectedNodeId;
    const raf = requestAnimationFrame(() => {
      const node = getNode(id);
      if (!node?.measured?.width || !node.measured.height) return;
      panToReveal(nodeRect(node));
    });
    return () => cancelAnimationFrame(raf);
  }, [selectedNodeId, fitKey, getNode, panToReveal]);

  // After a user move, expanding a server keeps the viewport unless its tool
  // band is off screen; then pan just enough to show the server and its
  // tools, or, when they cannot fit at this zoom, frame only them without
  // zooming in. Collapsing moves nothing. Before a user move the fit-key
  // effect re-frames the whole graph instead.
  const expandedServers = useStackStore((s) => s.expandedServers);
  const prevExpandedRef = useRef(expandedServers);
  useEffect(() => {
    const prev = prevExpandedRef.current;
    prevExpandedRef.current = expandedServers;
    if (!userMovedRef.current) return;
    const added = [...expandedServers].filter((id) => !prev.has(id));
    if (added.length === 0) return;
    const raf = requestAnimationFrame(() => {
      const addedSet = new Set(added);
      const storeNodes = useStackStore.getState().nodes;
      const toolIds = storeNodes
        .filter((n) => {
          const owner = (n.data as { serverNodeId?: string } | undefined)?.serverNodeId;
          return owner !== undefined && addedSet.has(owner);
        })
        .map((n) => n.id);
      if (toolIds.length === 0) return;
      // React Flow's lookup carries measurements; the store node covers a
      // node it has not picked up yet.
      const lookup = (id: string) => getNode(id) ?? storeNodes.find((n) => n.id === id);
      const { width, height } = canvasSize();
      if (!(width > 0 && height > 0)) return;
      const viewport = getViewport();
      const toolRects = toolIds.map(lookup).filter((n): n is Node => n !== undefined).map(nodeRect);
      if (toolRects.every((r) => isRectVisible(r, viewport, { width, height }))) return;
      const bandIds = [...added, ...toolIds];
      const band = unionRect(
        bandIds.map(lookup).filter((n): n is Node => n !== undefined).map(nodeRect),
      );
      if (!band || panToReveal(band)) return;
      void fitView({
        nodes: bandIds.map((id) => ({ id })),
        padding: 0.1,
        maxZoom: viewport.zoom,
        duration: motionDuration(400),
      });
    });
    return () => cancelAnimationFrame(raf);
  }, [expandedServers, getNode, getViewport, canvasSize, fitView, panToReveal]);

  // Dynamic edge options based on toggle
  const defaultEdgeOptions = useMemo(() => ({
    type: edgeStyle,
    style: {
      strokeWidth: 2,
      stroke: colors.border,
    },
  }), [edgeStyle, colors.border]);

  // Node ids the toolbar filter fades: non-matching servers, resources, and
  // clients, plus the tool fan-out of a faded server. Null while inactive so
  // the styling below returns today's arrays untouched.
  const filteredOut = useMemo(() => {
    if (!filter || !isFilterActive(filter)) return null;
    const out = new Set<string>();
    for (const row of buildStackRows(nodes ?? [])) {
      if (!matchesFilter(row, filter)) out.add(row.id);
    }
    for (const node of nodes ?? []) {
      const data = node.data as { type?: string; serverNodeId?: string } | undefined;
      if (
        (data?.type === 'tool' || data?.type === 'tool-overflow') &&
        data.serverNodeId !== undefined &&
        out.has(data.serverNodeId)
      ) {
        out.add(node.id);
      }
    }
    return out;
  }, [nodes, filter]);

  // Apply highlighting classes to nodes based on the selected agent's path.
  const styledNodes = useMemo(() => {
    const list = nodes ?? [];
    if (!highlightState.hasSelection && !filteredOut) return list;
    return list.map((node) => {
      const faded = filteredOut?.has(node.id) ?? false;
      if (!highlightState.hasSelection && !faded) return node;
      return {
        ...node,
        className: cn(
          node.className,
          highlightState.hasSelection &&
            (highlightState.highlightedNodeIds.has(node.id) ? 'highlighted' : 'dimmed'),
          faded && 'filtered-out',
        ),
      };
    });
  }, [nodes, highlightState, filteredOut]);

  // Apply highlighting classes to edges.
  // Uses edges (Agent → Server) are always hidden - we show the path through Gateway instead.
  // Animation is not a default: a selection animates the highlighted path, and
  // otherwise only gateway-to-server edges whose token total rose this poll,
  // capped so a busy fleet cannot march every edge.
  const styledEdges = useMemo(() => {
    const list = applyEdgeThemeColors(edges ?? [], colors);
    const animatedIds = gatedAnimatedIds(
      list,
      highlightState.hasSelection,
      highlightState.highlightedEdgeIds,
      activeServers,
    );
    const styled = list.map((edge) => {
      const edgeData = edge.data as { isUsesEdge?: boolean } | undefined;
      if (edgeData?.isUsesEdge) {
        return { ...edge, className: 'hidden' };
      }
      const isHighlighted = highlightState.hasSelection && highlightState.highlightedEdgeIds.has(edge.id);
      const animated = animatedIds.has(edge.id);
      if (!highlightState.hasSelection) {
        if ((edge.animated === true) === animated) return edge;
        return { ...edge, animated };
      }
      return {
        ...edge,
        animated,
        // Freeze the energy-flow animation while editing in Access Lens so the
        // canvas stays calm and the scope edits are the only thing moving.
        className: cn(
          edge.className,
          isHighlighted ? 'highlighted' : 'dimmed',
          lensActive && 'lens-frozen',
        ),
      };
    });
    if (!filteredOut) return styled;
    return styled.map((edge) =>
      filteredOut.has(edge.source) || filteredOut.has(edge.target)
        ? { ...edge, className: cn(edge.className, 'filtered-out') }
        : edge,
    );
  }, [edges, highlightState, lensActive, activeServers, colors, filteredOut]);

  // Handle node selection. Tool fan-out nodes own their own click handling:
  // each renders an anchored detail popover internally, so a canvas-level click
  // must not select them or open the sidebar. The node already stops
  // propagation; this early return is the belt-and-suspenders guard.
  // Returns whether the node was activated, so Space can preventDefault only then.
  // Keyboard activation asks the inspector to take focus; a mouse click and an
  // Access Lens draft toggle leave focus where it is.
  const activateNode = useCallback((node: ActivatableNode, fromKeyboard: boolean): boolean => {
    const nodeType = node.data?.type;
    if (nodeType === 'tool' || nodeType === 'tool-overflow') return false;
    // In Access Lens, a server-node click grants/revokes it in the draft instead
    // of selecting it. The draft mutates, never the stack (commit gate writes).
    if (lensActive && nodeType === 'mcp-server' && node.data?.name) {
      toggleDraftServer(node.data.name);
      return true;
    }
    // An unsaved inspector tool draft asks before the selection moves;
    // re-selecting the draft's own server passes straight through.
    requestInspectorLeave(
      () => {
        selectNode(node.id);
        setSidebarOpen(true);
        requestInspectorFocus(fromKeyboard ? node.id : null);
      },
      { targetServer: nodeType === 'mcp-server' ? node.data?.name : undefined },
    );
    return true;
  }, [selectNode, setSidebarOpen, requestInspectorFocus, lensActive, toggleDraftServer]);

  const onNodeClick = useCallback((_: MouseEvent, node: ActivatableNode) => {
    activateNode(node, false);
  }, [activateNode]);

  // React Flow's node wrapper handles Enter and Space itself and does not call
  // onNodeClick. Keydown does not reach the custom node component, so the
  // wrapper listener is the keyboard path. Inner buttons, links, and fields
  // keep their own activation. Enter is not preventDefault'd, so React Flow's
  // selection toggle still runs. Space is preventDefault'd only after a node
  // activates, and is not stopPropagation'd: React Flow's window listener still
  // arms pan-on-drag while Space is held.
  const onCanvasKeyDown = useCallback((event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key !== 'Enter' && event.key !== ' ') return;
    const target = event.target;
    if (!(target instanceof Element)) return;
    const wrapper = target.closest('.react-flow__node');
    if (!wrapper) return;
    if (
      target !== wrapper &&
      target.matches('button, a, input, select, textarea, [contenteditable]')
    ) {
      return;
    }
    const id = wrapper.getAttribute('data-id');
    if (!id) return;
    const node = nodes.find((item) => item.id === id);
    if (!node) return;
    const activated = activateNode(node, true);
    if (event.key === ' ' && activated) event.preventDefault();
  }, [nodes, activateNode]);

  // Handle pane click: deselect and close the sidebar. Tool fan-outs stay
  // expanded and the viewport stays where the user left it.
  const onPaneClick = useCallback(() => {
    requestInspectorLeave(() => {
      selectNode(null);
      setSidebarOpen(false);
    });
  }, [selectNode, setSidebarOpen]);

  // No-op connect handler. The canvas does not support drawing connections;
  // per-client access is edited through Access Lens, not by dragging edges.
  const onConnect = useCallback(() => {}, []);

  const isEmpty = !nodes || nodes.length === 0;
  const hasActiveStack = connectionStatus === 'connected' && gatewayInfo !== null;
  const edgeStyleLabel = `Switch to ${edgeStyle === 'default' ? 'straight' : 'curved'} edges`;
  const cardStyleLabel = compactCards ? 'Switch to full cards' : 'Switch to compact cards';

  return (
    <div ref={wrapperRef} className="absolute inset-0 canvas-wrapper">
      {/* Film grain overlay */}
      <div className="film-grain" />
      {/* Empty state CTA */}
      {isEmpty && (
        <div className="absolute inset-0 z-raised flex items-center justify-center pointer-events-none">
          <div className="pointer-events-auto glass-panel-elevated rounded-xl p-8 text-center max-w-sm animate-fade-in-up">
            <div className="w-12 h-12 rounded-xl bg-surface-elevated border border-border/40 flex items-center justify-center mx-auto mb-4">
              <Layers size={22} className="text-primary" />
            </div>
            <h3 className="text-sm font-medium text-text-primary mb-1.5">
              Create your first stack
            </h3>
            <p className="text-xs text-text-muted mb-5 leading-relaxed">
              Define MCP servers and resources in a guided wizard to generate your stack spec.
            </p>
            <button
              onClick={() => useWizardStore.getState().open('stack')}
              className={cn(
                'inline-flex items-center gap-2 px-4 py-2 rounded-lg text-xs font-medium',
                'bg-primary/20 text-primary hover:bg-primary/30 border border-primary/30',
                'transition-all duration-200',
              )}
            >
              <Plus size={14} />
              New Stack
            </button>
            {/* Quick-add links — only when a stack is active */}
            {hasActiveStack && (
              <div className="flex items-center gap-2 mt-3">
                <span className="text-3xs text-text-muted">or add:</span>
                {[
                  { type: 'mcp-server' as const, icon: Server, label: 'MCP Server', color: 'text-primary hover:text-primary-light' },
                  { type: 'resource' as const, icon: Database, label: 'Resource', color: 'text-secondary hover:text-secondary/80' },
                ].map((item) => (
                  <button
                    key={item.type}
                    onClick={() => useWizardStore.getState().open(item.type)}
                    className={cn(
                      'inline-flex items-center gap-1 px-2 py-1 rounded-md text-3xs font-medium',
                      'bg-surface-overlay-2 border border-border-subtle hover:border-border',
                      'transition-all duration-200',
                      item.color,
                    )}
                  >
                    <item.icon size={10} />
                    {item.label}
                  </button>
                ))}
              </div>
            )}
          </div>
        </div>
      )}
      <CanvasBase
        nodes={styledNodes}
        edges={styledEdges}
        nodeTypes={nodeTypes}
        nodesConnectable={false}
        edgesFocusable={false}
        deleteKeyCode={null}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onNodeClick={onNodeClick}
        onKeyDown={onCanvasKeyDown}
        onConnect={onConnect}
        onPaneClick={onPaneClick}
        onMoveStart={onMoveStart}
        onMove={onMove}
        defaultEdgeOptions={defaultEdgeOptions}
        fitView
        fitViewOptions={{ padding: 0.2, maxZoom: 1.5 }}
        minZoom={0.1}
        maxZoom={2}
        backgrounds={[
          {
            variant: BackgroundVariant.Lines,
            gap: 100,
            color: colors.border,
          },
        ]}
      >
        {/* Sub-grid dots fade in above 0.8x. Isolated so this canvas does not
            subscribe to the viewport. */}
        <SubGrid />

        {/* Controls */}
        <Panel position="bottom-left" className="flex gap-1">
          <button
            onClick={() => zoomInUser({ duration: 200 })}
            className="control-button"
            title="Zoom in"
            aria-label="Zoom in"
          >
            <Plus className="w-4 h-4" />
          </button>
          <button
            onClick={() => zoomOutUser({ duration: 200 })}
            className="control-button"
            title="Zoom out"
            aria-label="Zoom out"
          >
            <Minus className="w-4 h-4" />
          </button>
          <button
            onClick={() => fit({ padding: 0.2, duration: 300 })}
            className="control-button"
            title="Fit view"
            aria-label="Fit view"
          >
            <Maximize className="w-4 h-4" />
          </button>
          <button
            onClick={resetLayout}
            className="control-button"
            title="Reset layout"
            aria-label="Reset layout"
          >
            <RotateCcw className="w-4 h-4" />
          </button>
          <button
            onClick={toggleEdgeStyle}
            className="control-button"
            title={edgeStyleLabel}
            aria-label={edgeStyleLabel}
          >
            {edgeStyle === 'default' ? (
              <Minus className="w-4 h-4 rotate-45" />
            ) : (
              <Spline className="w-4 h-4" />
            )}
          </button>
          <button
            onClick={toggleCompactCards}
            className={cn(
              'control-button',
              compactCards && 'ring-1 ring-primary/30'
            )}
            title={cardStyleLabel}
            aria-label={cardStyleLabel}
          >
            {compactCards ? (
              <LayoutGrid className="w-4 h-4" />
            ) : (
              <Rows3 className="w-4 h-4" />
            )}
          </button>
          <OverlaysMenu />
          <CanvasLegend />
        </Panel>
      </CanvasBase>
      {showSpecMode && (
        <>
          <div className="absolute inset-0 pointer-events-none bg-secondary/[0.02] z-raised" />
          <SpecModeOverlay className="absolute inset-0 z-sticky" />
        </>
      )}
    </div>
  );
}
