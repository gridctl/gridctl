import { create } from 'zustand';
import { subscribeWithSelector } from 'zustand/middleware';
import type { Node, Edge, NodeChange, EdgeChange } from '@xyflow/react';
import { applyNodeChanges, applyEdgeChanges } from '@xyflow/react';
import type {
  GatewayStatus,
  MCPServerStatus,
  ResourceStatus,
  AgentStatus,
  ClientStatus,
  Tool,
  ConnectionStatus,
} from '../types';
import { transformToNodesAndEdges } from '../lib/transform';
import { useRegistryStore } from './useRegistryStore';

interface StackState {
  // === Raw API Data ===
  gatewayInfo: { name: string; version: string } | null;
  mcpServers: MCPServerStatus[];
  resources: ResourceStatus[];
  agents: AgentStatus[];  // Unified: includes both local and remote agents with A2A info
  clients: ClientStatus[];  // Detected/linked LLM clients
  tools: Tool[];
  sessions: number;
  a2aTasks: number | null;
  codeMode: string | null;  // Gateway code mode status ("on" when active)

  // === React Flow State ===
  nodes: Node[];
  edges: Edge[];
  draggedPositions: Map<string, { x: number; y: number }>;  // User-dragged node positions

  // === UI State ===
  selectedNodeId: string | null;
  connectionStatus: ConnectionStatus;
  lastUpdated: Date | null;
  isLoading: boolean;
  error: string | null;

  // === Actions ===
  setGatewayStatus: (status: GatewayStatus) => void;
  setClients: (clients: ClientStatus[]) => void;
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
    clients: [],
    tools: [],
    sessions: 0,
    a2aTasks: null,
    codeMode: null,
    nodes: [],
    edges: [],
    draggedPositions: new Map(),
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
        sessions: status.sessions ?? 0,
        a2aTasks: status.a2a_tasks ?? null,
        codeMode: status.code_mode || null,
        lastUpdated: new Date(),
        isLoading: false,
        error: null,
        connectionStatus: 'connected',
      });
      get().refreshNodesAndEdges();
    },

    setClients: (clients) => {
      set({ clients });
      // No refreshNodesAndEdges here -- setGatewayStatus already triggers it,
      // and clients are read from store state during refresh.
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
      const { gatewayInfo, mcpServers, resources, agents, clients, sessions, a2aTasks, codeMode, draggedPositions } = get();
      if (!gatewayInfo) return;

      const registryStatus = useRegistryStore.getState().status;

      // Only preserve positions for nodes the user has explicitly dragged
      const { nodes, edges } = transformToNodesAndEdges(
        gatewayInfo,
        mcpServers,
        resources,
        agents,
        draggedPositions.size > 0 ? draggedPositions : undefined,
        sessions,
        a2aTasks,
        clients,
        registryStatus,
        codeMode
      );
      set({ nodes, edges });
    },

    resetLayout: () => {
      const { gatewayInfo, mcpServers, resources, agents, clients, sessions, a2aTasks, codeMode } = get();
      if (!gatewayInfo) return;

      const registryStatus = useRegistryStore.getState().status;

      // Clear dragged positions and recalculate from scratch
      const { nodes, edges } = transformToNodesAndEdges(
        gatewayInfo,
        mcpServers,
        resources,
        agents,
        undefined,
        sessions,
        a2aTasks,
        clients,
        registryStatus,
        codeMode
      );
      set({ nodes, edges, draggedPositions: new Map() });
    },

    onNodesChange: (changes) => {
      const nodes = applyNodeChanges(changes, get().nodes);

      // Track user-dragged positions (drag end events)
      const draggedPositions = new Map(get().draggedPositions);
      for (const change of changes) {
        if (change.type === 'position' && change.dragging === false && change.position) {
          draggedPositions.set(change.id, change.position);
        }
      }

      set({ nodes, draggedPositions });
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
