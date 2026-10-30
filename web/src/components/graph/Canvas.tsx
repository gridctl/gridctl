import { useCallback, useEffect, useMemo, useRef, type KeyboardEvent, type MouseEvent } from 'react';
import {
  Panel,
  BackgroundVariant,
  applyEdgeChanges,
  applyNodeChanges,
  useReactFlow,
  type EdgeChange,
  type NodeChange,
} from '@xyflow/react';
import { RotateCcw, Spline, Minus, Plus, Maximize, Rows3, LayoutGrid, Layers, Server, Database } from 'lucide-react';

import { nodeTypes } from './nodeTypes';
import { useStackStore } from '../../stores/useStackStore';
import { useCanvasActionsStore } from '../../stores/useCanvasActionsStore';
import { useUIStore } from '../../stores/useUIStore';
import { useWizardStore } from '../../stores/useWizardStore';
import { useAccessLensStore, buildDraftScope, flattenTools, isDirty } from '../../stores/useAccessLensStore';
import { useThemeColors } from '../../themes/useTheme';
import { usePathHighlight } from '../../hooks/usePathHighlight';
import { cn } from '../../lib/cn';
import { CanvasBase } from '../canvas/CanvasBase';
import { SpecModeOverlay } from '../spec/SpecModeOverlay';
import { OverlaysMenu } from './OverlaysMenu';
import { SubGrid } from './SubGrid';

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

type ActivatableNode = {
  id: string;
  data?: { type?: string; name?: string };
};

export function Canvas() {
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

  // React Flow controls. Registered for shell shortcuts that live outside this tree.
  const { zoomIn, zoomOut, fitView } = useReactFlow();
  const registerCanvasActions = useCanvasActionsStore((s) => s.register);
  const clearCanvasActions = useCanvasActionsStore((s) => s.clear);
  useEffect(() => {
    registerCanvasActions({ fitView, zoomIn, zoomOut });
    return () => clearCanvasActions();
  }, [fitView, zoomIn, zoomOut, registerCanvasActions, clearCanvasActions]);

  const onNodesChange = useCallback((changes: NodeChange[]) => {
    const store = useStackStore.getState();
    setNodes(applyNodeChanges(changes, store.nodes));
    recordDraggedPositions(changes);
  }, [setNodes, recordDraggedPositions]);

  const onEdgesChange = useCallback((changes: EdgeChange[]) => {
    setEdges(applyEdgeChanges(changes, useStackStore.getState().edges));
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

  // Auto-fit the view whenever the visible layout changes. With a client
  // focused the fit key is its highlighted reachable subgraph; otherwise it is
  // every node id, so expanding or collapsing a server's tool fan-out (or
  // servers appearing on hot reload) re-frames the whole graph at max zoom.
  // The layout epoch folds in resetLayout (reset button, compact-cards
  // toggle), which recomputes every position without changing the id set.
  // Keying on epoch plus sorted ids keeps the canvas still during status
  // polls and node drags, which change neither.
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
  useEffect(() => {
    if (!fitKey) return;
    const ids = fitKey.slice(fitKey.indexOf(':') + 1).split(',').map((id) => ({ id }));
    fitSetRef.current = ids;
    // fitView queues internally until new nodes are measured (React Flow
    // 12.5+), but it never re-reads a container resized in the same commit:
    // selecting a client opens the sidebar grid column synchronously with
    // this effect, so defer one frame to let React Flow's ResizeObserver
    // record the narrowed width before framing.
    const raf = requestAnimationFrame(() => {
      fitView({ nodes: ids, padding: 0.25, duration: 400, maxZoom: 1.5 });
    });
    return () => cancelAnimationFrame(raf);
  }, [fitKey, fitView]);

  // Re-frame the current fit set when the canvas container's width changes.
  // The detail sidebar is a grid column, so opening, closing, or dragging it
  // resizes the canvas without touching the fit key (node ids and epoch;
  // under Access Lens the key is even byte-identical across client
  // selection), and React Flow never auto-fits on container resize. Gated on
  // width so height-only changes hold the viewport still, and debounced so
  // the sidebar resize handle re-frames once per gesture. The first
  // observation reports the mount-time size and must not re-frame a canvas
  // nobody resized.
  const wrapperRef = useRef<HTMLDivElement>(null);
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
        if (ids.length === 0) return;
        fitView({ nodes: ids, padding: 0.25, duration: 400, maxZoom: 1.5 });
      }, 150);
    });
    observer.observe(el);
    return () => {
      window.clearTimeout(timer);
      observer.disconnect();
    };
  }, [fitView]);

  // Dynamic edge options based on toggle
  const defaultEdgeOptions = useMemo(() => ({
    type: edgeStyle,
    style: {
      strokeWidth: 2,
      stroke: colors.border,
    },
  }), [edgeStyle, colors.border]);

  // Apply highlighting classes to nodes based on the selected agent's path.
  const styledNodes = useMemo(() => {
    if (!highlightState.hasSelection) return nodes ?? [];
    return (nodes ?? []).map((node) => ({
      ...node,
      className: cn(
        node.className,
        highlightState.highlightedNodeIds.has(node.id) ? 'highlighted' : 'dimmed'
      ),
    }));
  }, [nodes, highlightState]);

  // Apply highlighting classes to edges.
  // Uses edges (Agent → Server) are always hidden - we show the path through Gateway instead.
  // Animation is not a default: a selection animates the highlighted path, and
  // otherwise only gateway-to-server edges whose token total rose this poll,
  // capped so a busy fleet cannot march every edge.
  const styledEdges = useMemo(() => {
    const list = edges ?? [];
    const animatedIds = gatedAnimatedIds(
      list,
      highlightState.hasSelection,
      highlightState.highlightedEdgeIds,
      activeServers,
    );
    return list.map((edge) => {
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
  }, [edges, highlightState, lensActive, activeServers]);

  // Handle node selection. Tool fan-out nodes own their own click handling:
  // each renders an anchored detail popover internally, so a canvas-level click
  // must not select them or open the sidebar. The node already stops
  // propagation; this early return is the belt-and-suspenders guard.
  // Returns whether the node was activated, so Space can preventDefault only then.
  const activateNode = useCallback((node: ActivatableNode): boolean => {
    const nodeType = node.data?.type;
    if (nodeType === 'tool' || nodeType === 'tool-overflow') return false;
    // In Access Lens, a server-node click grants/revokes it in the draft instead
    // of selecting it. The draft mutates, never the stack (commit gate writes).
    if (lensActive && nodeType === 'mcp-server' && node.data?.name) {
      toggleDraftServer(node.data.name);
      return true;
    }
    selectNode(node.id);
    setSidebarOpen(true);
    return true;
  }, [selectNode, setSidebarOpen, lensActive, toggleDraftServer]);

  const onNodeClick = useCallback((_: MouseEvent, node: ActivatableNode) => {
    activateNode(node);
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
    const activated = activateNode(node);
    if (event.key === ' ' && activated) event.preventDefault();
  }, [nodes, activateNode]);

  // Handle pane click: return to the default view. Deselect, close the
  // sidebar, and collapse every tool fan-out (which also unmounts any open
  // tool detail popover), then zoom back out to frame the whole graph. When
  // fan-outs were open the collapse changes the node-id set and the auto-fit
  // above re-frames; the explicit fitView covers the nothing-was-open case.
  const collapseAllServers = useStackStore((s) => s.collapseAllServers);
  const onPaneClick = useCallback(() => {
    selectNode(null);
    setSidebarOpen(false);
    collapseAllServers();
    fitView({ padding: 0.2, duration: 400 });
  }, [selectNode, setSidebarOpen, collapseAllServers, fitView]);

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
                <span className="text-[10px] text-text-muted">or add:</span>
                {[
                  { type: 'mcp-server' as const, icon: Server, label: 'Server', color: 'text-primary hover:text-primary/80' },
                  { type: 'resource' as const, icon: Database, label: 'Resource', color: 'text-secondary hover:text-secondary/80' },
                ].map((item) => (
                  <button
                    key={item.type}
                    onClick={() => useWizardStore.getState().open(item.type)}
                    className={cn(
                      'inline-flex items-center gap-1 px-2 py-1 rounded-md text-[10px] font-medium',
                      'bg-white/[0.03] border border-white/[0.06] hover:border-white/[0.12]',
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
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onNodeClick={onNodeClick}
        onKeyDown={onCanvasKeyDown}
        onConnect={onConnect}
        onPaneClick={onPaneClick}
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
            onClick={() => zoomIn({ duration: 200 })}
            className="control-button"
            title="Zoom in"
            aria-label="Zoom in"
          >
            <Plus className="w-4 h-4" />
          </button>
          <button
            onClick={() => zoomOut({ duration: 200 })}
            className="control-button"
            title="Zoom out"
            aria-label="Zoom out"
          >
            <Minus className="w-4 h-4" />
          </button>
          <button
            onClick={() => fitView({ padding: 0.2, duration: 300 })}
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
