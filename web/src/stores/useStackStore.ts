import { create } from 'zustand';
import { subscribeWithSelector } from 'zustand/middleware';
import type { Node, Edge, NodeChange, EdgeChange } from '@xyflow/react';
import { applyNodeChanges, applyEdgeChanges } from '@xyflow/react';
import type {
  GatewayStatus,
  MCPServerStatus,
  ResourceStatus,
  AgentStatus,
  Tool,
  ConnectionStatus,
} from '../types';
import { transformToNodesAndEdges } from '../lib/transform';

interface StackState {
  // === Raw API Data ===
  gatewayInfo: { name: string; version: string } | null;
  mcpServers: MCPServerStatus[];
  resources: ResourceStatus[];
  agents: AgentStatus[];  // Unified: includes both local and remote agents with A2A info
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

export const useStackStore = create<StackState>()(
  subscribeWithSelector((set, get) => ({
    // Initial state
    gatewayInfo: null,
    mcpServers: [],
    resources: [],
    agents: [],
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
        agents: status.agents || [],  // Unified agents (includes A2A info)
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
      const { gatewayInfo, mcpServers, resources, agents, nodes: existingNodes } = get();
      if (!gatewayInfo) return;

      // Build map of existing positions to preserve user-dragged positions
      const positionMap = new Map(
        existingNodes.map((n) => [n.id, n.position])
      );

      const { nodes, edges } = transformToNodesAndEdges(
        gatewayInfo,
        mcpServers,
        resources,
        agents,
        positionMap
      );
      set({ nodes, edges });
    },

    resetLayout: () => {
      const { gatewayInfo, mcpServers, resources, agents } = get();
      if (!gatewayInfo) return;

      // Don't pass positionMap to get default calculated positions
      const { nodes, edges } = transformToNodesAndEdges(
        gatewayInfo,
        mcpServers,
        resources,
        agents
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
  const selectedNodeId = useStackStore((s) => s.selectedNodeId);
  const nodes = useStackStore((s) => s.nodes);
  return nodes.find((n) => n.id === selectedNodeId);
};

export const useSelectedNodeData = () => {
  const node = useSelectedNode();
  return node?.data as Record<string, unknown> | undefined;
};
