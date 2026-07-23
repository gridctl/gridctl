import { useCallback, useEffect, useMemo, useRef } from 'react';
import {
  Background,
  Panel,
  BackgroundVariant,
  useReactFlow,
  useViewport,
} from '@xyflow/react';
import { RotateCcw, Spline, Minus, Plus, Maximize, Rows3, LayoutGrid, Layers, Server, Database } from 'lucide-react';

import { nodeTypes } from './nodeTypes';
import { useStackStore } from '../../stores/useStackStore';
import { useUIStore } from '../../stores/useUIStore';
import { useWizardStore } from '../../stores/useWizardStore';
import { useAccessLensStore, buildDraftScope, flattenTools, isDirty } from '../../stores/useAccessLensStore';
import { useThemeColors } from '../../themes/useTheme';
import { usePathHighlight } from '../../hooks/usePathHighlight';
import { cn } from '../../lib/cn';
import { CanvasBase } from '../canvas/CanvasBase';
import { SpecModeOverlay } from '../spec/SpecModeOverlay';
import { OverlaysMenu } from './OverlaysMenu';

export function Canvas() {
  const nodes = useStackStore((s) => s.nodes);
  const edges = useStackStore((s) => s.edges);
  const onNodesChange = useStackStore((s) => s.onNodesChange);
  const onEdgesChange = useStackStore((s) => s.onEdgesChange);
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

  // React Flow controls
  const { zoomIn, zoomOut, fitView } = useReactFlow();
  const { zoom } = useViewport();
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
  // re-frames. The sidebar is a grid column, so fitView fits the narrowed
  // canvas area on its own.
  const layoutEpoch = useStackStore((s) => s.layoutEpoch);
  const selectedNode = nodes.find((n) => n.id === selectedNodeId);
  const isClientSelected =
    (selectedNode?.data as { type?: string } | undefined)?.type === 'client';
  const fitIds =
    isClientSelected && !lensActive
      ? [...highlightState.highlightedNodeIds].sort().join(',')
      : (nodes ?? []).map((n) => n.id).sort().join(',');
  const fitKey = fitIds ? `${layoutEpoch}:${fitIds}` : '';
  useEffect(() => {
    if (!fitKey) return;
    const ids = fitKey.slice(fitKey.indexOf(':') + 1).split(',').map((id) => ({ id }));
    // fitView queues internally until new nodes are measured (React Flow 12.5+),
    // so no frame deferral is needed before fitting freshly-mounted tool nodes.
    fitView({ nodes: ids, padding: 0.25, duration: 400, maxZoom: 1.5 });
  }, [fitKey, fitView]);

  // Evolvable grid - main lines at 100px, sub-grid dots at 20px fade in at >0.8x
  const showSubGrid = zoom > 0.8;
  const subGridOpacity = Math.min((zoom - 0.8) * 2.5, 1); // Fade from 0.8 to 1.2

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
  // Uses edges (Agent → Server) are always hidden - we show the path through Gateway instead
  const styledEdges = useMemo(() => {
    return (edges ?? []).map((edge) => {
      const edgeData = edge.data as { isUsesEdge?: boolean } | undefined;
      if (edgeData?.isUsesEdge) {
        return { ...edge, className: 'hidden' };
      }
      if (!highlightState.hasSelection) return edge;
      const isHighlighted = highlightState.highlightedEdgeIds.has(edge.id);
      return {
        ...edge,
        // Freeze the energy-flow animation while editing in Access Lens so the
        // canvas stays calm and the scope edits are the only thing moving.
        className: cn(
          edge.className,
          isHighlighted ? 'highlighted' : 'dimmed',
          lensActive && 'lens-frozen',
        ),
      };
    });
  }, [edges, highlightState, lensActive]);

  // Handle node selection. Tool fan-out nodes own their own click handling:
  // each renders an anchored detail popover internally, so a canvas-level click
  // must not select them or open the sidebar. The node already stops
  // propagation; this early return is the belt-and-suspenders guard.
  const onNodeClick = useCallback((_: React.MouseEvent, node: { id: string; data?: { type?: string; name?: string } }) => {
    const nodeType = node.data?.type;
    if (nodeType === 'tool' || nodeType === 'tool-overflow') return;
    // In Access Lens, a server-node click grants/revokes it in the draft instead
    // of selecting it — the draft mutates, never the stack (commit gate writes).
    if (lensActive && nodeType === 'mcp-server' && node.data?.name) {
      toggleDraftServer(node.data.name);
      return;
    }
    selectNode(node.id);
    setSidebarOpen(true);
  }, [selectNode, setSidebarOpen, lensActive, toggleDraftServer]);

  // Handle pane click (deselect). Reset focus and zoom back out to frame the
  // whole graph, complementing the zoom-to-fit on client selection.
  const onPaneClick = useCallback(() => {
    selectNode(null);
    setSidebarOpen(false);
    fitView({ padding: 0.2, duration: 400 });
  }, [selectNode, setSidebarOpen, fitView]);

  // No-op connect handler. The canvas does not support drawing connections;
  // per-client access is edited through Access Lens, not by dragging edges.
  const onConnect = useCallback(() => {}, []);

  const isEmpty = !nodes || nodes.length === 0;
  const hasActiveStack = connectionStatus === 'connected' && gatewayInfo !== null;

  return (
    <div className="absolute inset-0 canvas-wrapper">
      {/* Film grain overlay */}
      <div className="film-grain" />
      {/* Empty state CTA */}
      {isEmpty && (
        <div className="absolute inset-0 z-10 flex items-center justify-center pointer-events-none">
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
        {/* Sub-grid — dots at 20px, fades in when zoom > 0.8. Rendered as a
            child so the zoom-conditional layer stays local to this canvas. */}
        {showSubGrid && (
          <Background
            id="sub-grid"
            variant={BackgroundVariant.Dots}
            gap={20}
            size={1}
            color={`rgba(13, 148, 136, ${0.1 * subGridOpacity})`}
          />
        )}

        {/* Controls */}
        <Panel position="bottom-left" className="flex gap-1">
          <button
            onClick={() => zoomIn({ duration: 200 })}
            className="control-button"
            title="Zoom in"
          >
            <Plus className="w-4 h-4" />
          </button>
          <button
            onClick={() => zoomOut({ duration: 200 })}
            className="control-button"
            title="Zoom out"
          >
            <Minus className="w-4 h-4" />
          </button>
          <button
            onClick={() => fitView({ padding: 0.2, duration: 300 })}
            className="control-button"
            title="Fit view"
          >
            <Maximize className="w-4 h-4" />
          </button>
          <button
            onClick={resetLayout}
            className="control-button"
            title="Reset layout"
          >
            <RotateCcw className="w-4 h-4" />
          </button>
          <button
            onClick={toggleEdgeStyle}
            className="control-button"
            title={`Switch to ${edgeStyle === 'default' ? 'straight' : 'curved'} edges`}
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
            title={compactCards ? 'Switch to full cards' : 'Switch to compact cards'}
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
          <div className="absolute inset-0 pointer-events-none bg-secondary/[0.02] z-10" />
          <SpecModeOverlay className="absolute inset-0 z-20" />
        </>
      )}
    </div>
  );
}
