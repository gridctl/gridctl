import { useCallback, useEffect, useMemo, useRef } from 'react';
import {
  ReactFlow,
  Background,
  Panel,
  BackgroundVariant,
  useReactFlow,
  useViewport,
  type Connection,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import { RotateCcw, Spline, Minus, Plus, Maximize, Rows3, LayoutGrid, Flame, Layers, Server, Database, GitCompareArrows, Eye, Cable, KeyRound } from 'lucide-react';

import { nodeTypes } from './nodeTypes';
import { useStackStore } from '../../stores/useStackStore';
import { useUIStore } from '../../stores/useUIStore';
import { usePlaygroundStore } from '../../stores/usePlaygroundStore';
import { useWizardStore } from '../../stores/useWizardStore';
import { COLORS } from '../../lib/constants';
import { usePathHighlight } from '../../hooks/usePathHighlight';
import { cn } from '../../lib/cn';
import { DriftOverlay } from '../spec/DriftOverlay';
import { SpecModeOverlay } from '../spec/SpecModeOverlay';
import { SecretHeatmapOverlay } from '../spec/SecretHeatmapOverlay';
import { WiringModeOverlay } from './WiringModeOverlay';

export function Canvas() {
  const nodes = useStackStore((s) => s.nodes);
  const edges = useStackStore((s) => s.edges);
  const onNodesChange = useStackStore((s) => s.onNodesChange);
  const onEdgesChange = useStackStore((s) => s.onEdgesChange);
  const selectNode = useStackStore((s) => s.selectNode);
  const selectedNodeId = useStackStore((s) => s.selectedNodeId);
  const resetLayout = useStackStore((s) => s.resetLayout);
  const setSidebarOpen = useUIStore((s) => s.setSidebarOpen);
  const edgeStyle = useUIStore((s) => s.edgeStyle);
  const toggleEdgeStyle = useUIStore((s) => s.toggleEdgeStyle);
  const compactCards = useUIStore((s) => s.compactCards);
  const toggleCompactCards = useUIStore((s) => s.toggleCompactCards);
  const showHeatMap = useUIStore((s) => s.showHeatMap);
  const toggleHeatMap = useUIStore((s) => s.toggleHeatMap);
  const showDriftOverlay = useUIStore((s) => s.showDriftOverlay);
  const toggleDriftOverlay = useUIStore((s) => s.toggleDriftOverlay);
  const showSpecMode = useUIStore((s) => s.showSpecMode);
  const toggleSpecMode = useUIStore((s) => s.toggleSpecMode);
  const showWiringMode = useUIStore((s) => s.showWiringMode);
  const toggleWiringMode = useUIStore((s) => s.toggleWiringMode);
  const showSecretHeatmap = useUIStore((s) => s.showSecretHeatmap);
  const toggleSecretHeatmap = useUIStore((s) => s.toggleSecretHeatmap);
  const agentIsThinking = usePlaygroundStore((s) => s.agentIsThinking);
  const activeToolCallEdges = usePlaygroundStore((s) => s.activeToolCallEdges);

  // React Flow controls
  const { zoomIn, zoomOut, fitView } = useReactFlow();
  const { zoom } = useViewport();

  // Reset layout when compact cards toggle changes
  const prevCompactRef = useRef(compactCards);
  useEffect(() => {
    if (prevCompactRef.current !== compactCards) {
      prevCompactRef.current = compactCards;
      resetLayout();
    }
  }, [compactCards, resetLayout]);

  // Path highlighting for selected agents
  const highlightState = usePathHighlight(nodes, edges, selectedNodeId);

  // Evolvable grid - main lines at 100px, sub-grid dots at 20px fade in at >0.8x
  const showSubGrid = zoom > 0.8;
  const subGridOpacity = Math.min((zoom - 0.8) * 2.5, 1); // Fade from 0.8 to 1.2

  // Dynamic edge options based on toggle
  const defaultEdgeOptions = useMemo(() => ({
    type: edgeStyle,
    style: {
      strokeWidth: 2,
      stroke: COLORS.border,
    },
  }), [edgeStyle]);

  // Apply highlighting classes and playground animation state to nodes
  const styledNodes = useMemo(() => {
    const hasPlaygroundActivity = agentIsThinking || activeToolCallEdges.size > 0;
    if (!highlightState.hasSelection && !hasPlaygroundActivity) return nodes ?? [];

    return (nodes ?? []).map((node) => {
      const nodeData = node.data as { type?: string; name?: string };
      let updatedData = node.data;

      if (nodeData.type === 'mcp-server') {
        updatedData = { ...updatedData, isProcessing: activeToolCallEdges.has(nodeData.name ?? '') };
      }

      const base = updatedData !== node.data ? { ...node, data: updatedData } : node;

      if (!highlightState.hasSelection) return base;

      return {
        ...base,
        className: cn(
          node.className,
          highlightState.highlightedNodeIds.has(node.id) ? 'highlighted' : 'dimmed'
        ),
      };
    });
  }, [nodes, highlightState, agentIsThinking, activeToolCallEdges]);

  // Apply highlighting classes and playground animation to edges
  // Uses edges (Agent → Server) are always hidden - we show the path through Gateway instead
  const styledEdges = useMemo(() => {
    return (edges ?? []).map((edge) => {
      const edgeData = edge.data as { isUsesEdge?: boolean } | undefined;
      const isUsesEdge = edgeData?.isUsesEdge;

      // Always hide uses edges - path is shown via Gateway highlighting
      if (isUsesEdge) {
        return {
          ...edge,
          className: 'hidden',
        };
      }

      // Animate edges to servers with active tool calls
      // Gateway→server edge IDs follow: edge-gateway-mcp-${serverName}
      let isAnimated = false;
      if (activeToolCallEdges.size > 0) {
        for (const serverName of activeToolCallEdges) {
          if (edge.id === `edge-gateway-mcp-${serverName}`) {
            isAnimated = true;
            break;
          }
        }
      }

      // No selection: show edges with animation applied
      if (!highlightState.hasSelection) {
        return isAnimated ? { ...edge, animated: true } : edge;
      }

      // With selection: highlight the path, dim everything else
      const isHighlighted = highlightState.highlightedEdgeIds.has(edge.id);
      return {
        ...edge,
        animated: isAnimated,
        className: cn(edge.className, isHighlighted ? 'highlighted' : 'dimmed'),
      };
    });
  }, [edges, highlightState, activeToolCallEdges]);

  // Handle node selection
  const onNodeClick = useCallback((_: React.MouseEvent, node: { id: string }) => {
    selectNode(node.id);
    setSidebarOpen(true);
  }, [selectNode, setSidebarOpen]);

  // Handle pane click (deselect)
  const onPaneClick = useCallback(() => {
    selectNode(null);
    setSidebarOpen(false);
  }, [selectNode, setSidebarOpen]);

  // No-op connect handler (wiring mode no longer supports agent connections)
  const onConnect = useCallback((_connection: Connection) => {}, []);

  const isEmpty = !nodes || nodes.length === 0;

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
            {/* Quick-add links */}
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
          </div>
        </div>
      )}
      <ReactFlow
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
        fitViewOptions={{
          padding: 0.2,
          maxZoom: 1.5,
        }}
        minZoom={0.1}
        maxZoom={2}
        proOptions={{ hideAttribution: true }}
      >
        {/* Main grid - Lines at 100px intervals */}
        <Background
          variant={BackgroundVariant.Lines}
          gap={100}
          color="rgba(100, 116, 139, 0.15)"
        />

        {/* Sub-grid - Dots at 20px, fades in when zoom > 0.8 */}
        {showSubGrid && (
          <Background
            id="sub-grid"
            variant={BackgroundVariant.Dots}
            gap={20}
            size={1}
            color={`rgba(0, 202, 255, ${0.1 * subGridOpacity})`}
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
          <button
            onClick={toggleHeatMap}
            className={cn(
              'control-button',
              showHeatMap && 'ring-1 ring-primary/30'
            )}
            title={showHeatMap ? 'Hide token heat map' : 'Show token heat map'}
          >
            <Flame className="w-4 h-4" />
          </button>
          <button
            onClick={toggleDriftOverlay}
            className={cn(
              'control-button',
              showDriftOverlay && 'ring-1 ring-primary/30'
            )}
            title={showDriftOverlay ? 'Hide drift overlay' : 'Show drift overlay'}
          >
            <GitCompareArrows className="w-4 h-4" />
          </button>
          <button
            onClick={toggleSpecMode}
            className={cn(
              'control-button',
              showSpecMode && 'ring-1 ring-secondary/30'
            )}
            title={showSpecMode ? 'Exit spec mode' : 'Enter spec mode'}
          >
            <Eye className="w-4 h-4" />
          </button>
          <button
            onClick={toggleWiringMode}
            className={cn(
              'control-button',
              showWiringMode && 'ring-1 ring-tertiary/30'
            )}
            title={showWiringMode ? 'Exit wiring mode' : 'Enter wiring mode'}
          >
            <Cable className="w-4 h-4" />
          </button>
          <button
            onClick={toggleSecretHeatmap}
            className={cn(
              'control-button',
              showSecretHeatmap && 'ring-1 ring-tertiary/30'
            )}
            title={showSecretHeatmap ? 'Hide secret heatmap' : 'Show secret heatmap'}
          >
            <KeyRound className="w-4 h-4" />
          </button>
        </Panel>
      </ReactFlow>
      {showDriftOverlay && (
        <div className="absolute inset-0 pointer-events-none bg-primary/[0.02] z-10" />
      )}
      {showDriftOverlay && (
        <DriftOverlay className="absolute inset-0 z-20" />
      )}
      {showSpecMode && (
        <>
          <div className="absolute inset-0 pointer-events-none bg-secondary/[0.02] z-10" />
          <SpecModeOverlay className="absolute inset-0 z-20" />
        </>
      )}
      {showWiringMode && (
        <>
          <div className="absolute inset-0 pointer-events-none bg-tertiary/[0.02] z-10" />
          <WiringModeOverlay className="absolute inset-0 z-20" />
        </>
      )}
      {showSecretHeatmap && (
        <>
          <div className="absolute inset-0 pointer-events-none bg-tertiary/[0.02] z-10" />
          <SecretHeatmapOverlay className="absolute inset-0 z-20" />
        </>
      )}
    </div>
  );
}
