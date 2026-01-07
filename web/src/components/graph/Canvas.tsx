import { useCallback, useMemo } from 'react';
import {
  ReactFlow,
  Background,
  MiniMap,
  Panel,
  BackgroundVariant,
  useReactFlow,
  useViewport,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import { RotateCcw, Spline, Minus, Plus, Maximize } from 'lucide-react';

import { nodeTypes } from './nodeTypes';
import { useTopologyStore } from '../../stores/useTopologyStore';
import { useUIStore } from '../../stores/useUIStore';
import { COLORS } from '../../lib/constants';

export function Canvas() {
  const nodes = useTopologyStore((s) => s.nodes);
  const edges = useTopologyStore((s) => s.edges);
  const onNodesChange = useTopologyStore((s) => s.onNodesChange);
  const onEdgesChange = useTopologyStore((s) => s.onEdgesChange);
  const selectNode = useTopologyStore((s) => s.selectNode);
  const resetLayout = useTopologyStore((s) => s.resetLayout);
  const setSidebarOpen = useUIStore((s) => s.setSidebarOpen);
  const edgeStyle = useUIStore((s) => s.edgeStyle);
  const toggleEdgeStyle = useUIStore((s) => s.toggleEdgeStyle);

  // React Flow controls
  const { zoomIn, zoomOut, fitView } = useReactFlow();
  const { zoom } = useViewport();

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

  // MiniMap node color based on type and status
  const minimapNodeColor = useCallback((node: { data: Record<string, unknown> }) => {
    const data = node.data;
    if (data.type === 'gateway') return COLORS.primary;

    const status = data.status as string | undefined;
    if (status === 'running') return COLORS.statusRunning;
    if (status === 'error') return COLORS.statusError;
    if (status === 'initializing') return COLORS.statusPending;
    return COLORS.statusStopped;
  }, []);

  return (
    <div className="flex-1 h-full relative canvas-wrapper">
      {/* Film grain overlay */}
      <div className="film-grain" />
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onNodeClick={onNodeClick}
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
        </Panel>

        {/* MiniMap */}
        <MiniMap
          nodeColor={minimapNodeColor}
          maskColor="rgba(15, 23, 42, 0.7)"
          style={{
            height: 120,
            width: 180,
          }}
          zoomable
          pannable
          position="bottom-right"
        />
      </ReactFlow>
    </div>
  );
}
