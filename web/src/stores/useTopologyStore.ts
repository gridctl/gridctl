import { create } from 'zustand';
import { subscribeWithSelector } from 'zustand/middleware';
import type { Node, Edge, NodeChange, EdgeChange } from '@xyflow/react';
import { applyNodeChanges, applyEdgeChanges } from '@xyflow/react';
import type {
  GatewayStatus,
  MCPServerStatus,
  ResourceStatus,
  Tool,
  ConnectionStatus,
} from '../types';
import { transformToNodesAndEdges } from '../lib/transform';

interface TopologyState {
  // === Raw API Data ===
  gatewayInfo: { name: string; version: string } | null;
  mcpServers: MCPServerStatus[];
  resources: ResourceStatus[];
  tools: Tool[];

  // === React Flow State ===
  nodes: Node[];
  edges: Edge[];

  // === UI State ===
  selectedNodeId: string | null;
  connectionStatus: ConnectionStatus;
  lastUpdated: Date | null;
  isLoading: boolean;
  error: string | null;

  // === Actions ===
  setGatewayStatus: (status: GatewayStatus) => void;
  setTools: (tools: Tool[]) => void;
  setError: (error: string | null) => void;
  setLoading: (loading: boolean) => void;
  setConnectionStatus: (status: ConnectionStatus) => void;
  selectNode: (nodeId: string | null) => void;
  refreshNodesAndEdges: () => void;
  resetLayout: () => void;

  // React Flow callbacks
  onNodesChange: (changes: NodeChange[]) => void;
  onEdgesChange: (changes: EdgeChange[]) => void;
}

export const useTopologyStore = create<TopologyState>()(
  subscribeWithSelector((set, get) => ({
    // Initial state
    gatewayInfo: null,
    mcpServers: [],
    resources: [],
    tools: [],
    nodes: [],
    edges: [],
    selectedNodeId: null,
    connectionStatus: 'disconnected',
    lastUpdated: null,
    isLoading: true,
    error: null,

    // Actions
    setGatewayStatus: (status) => {
      set({
        gatewayInfo: status.gateway,
        mcpServers: status['mcp-servers'] || [],
        resources: status.resources || [],
        lastUpdated: new Date(),
        isLoading: false,
        error: null,
        connectionStatus: 'connected',
      });
      get().refreshNodesAndEdges();
    },

    setTools: (tools) => set({ tools }),

    setError: (error) => set({
      error,
      isLoading: false,
      connectionStatus: error ? 'error' : get().connectionStatus,
    }),

    setLoading: (isLoading) => set({ isLoading }),

    setConnectionStatus: (connectionStatus) => set({ connectionStatus }),

    selectNode: (nodeId) => set({ selectedNodeId: nodeId }),

    refreshNodesAndEdges: () => {
      const { gatewayInfo, mcpServers, resources, nodes: existingNodes } = get();
      if (!gatewayInfo) return;

      // Build map of existing positions to preserve user-dragged positions
      const positionMap = new Map(
        existingNodes.map((n) => [n.id, n.position])
      );

      const { nodes, edges } = transformToNodesAndEdges(
        gatewayInfo,
        mcpServers,
        resources,
        positionMap
      );
      set({ nodes, edges });
    },

    resetLayout: () => {
      const { gatewayInfo, mcpServers, resources } = get();
      if (!gatewayInfo) return;

      // Don't pass positionMap to get default calculated positions
      const { nodes, edges } = transformToNodesAndEdges(
        gatewayInfo,
        mcpServers,
        resources
      );
      set({ nodes, edges });
    },

    onNodesChange: (changes) => {
      set({
        nodes: applyNodeChanges(changes, get().nodes),
      });
    },

    onEdgesChange: (changes) => {
      set({
        edges: applyEdgeChanges(changes, get().edges),
      });
    },
  }))
);

// Selectors
export const useSelectedNode = () => {
  const selectedNodeId = useTopologyStore((s) => s.selectedNodeId);
  const nodes = useTopologyStore((s) => s.nodes);
  return nodes.find((n) => n.id === selectedNodeId);
};

export const useSelectedNodeData = () => {
  const node = useSelectedNode();
  return node?.data as Record<string, unknown> | undefined;
};
