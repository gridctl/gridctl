import { create } from 'zustand';
import { subscribeWithSelector } from 'zustand/middleware';
import type { Node, Edge, NodeChange, EdgeChange } from '@xyflow/react';
import { applyNodeChanges, applyEdgeChanges } from '@xyflow/react';
import type {
  GatewayStatus,
  MCPServerStatus,
  ResourceStatus,
  ClientStatus,
  Tool,
  TokenUsage,
  ConnectionStatus,
} from '../types';
import { transformToNodesAndEdges } from '../lib/transform';
import { useRegistryStore } from './useRegistryStore';
import { useUIStore } from './useUIStore';

interface StackState {
  // === Raw API Data ===
  gatewayInfo: { name: string; version: string } | null;
  mcpServers: MCPServerStatus[];
  resources: ResourceStatus[];
  clients: ClientStatus[];  // Detected/linked LLM clients
  tools: Tool[];
  sessions: number;
  codeMode: string | null;  // Gateway code mode status ("on" when active)
  tokenUsage: TokenUsage | null; // Token usage metrics from status response

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
    clients: [],
    tools: [],
    sessions: 0,
    codeMode: null,
    tokenUsage: null,
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
        sessions: status.sessions ?? 0,
        codeMode: status.code_mode || null,
        tokenUsage: status.token_usage ?? null,
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
      const { gatewayInfo, mcpServers, resources, clients, sessions, codeMode, draggedPositions } = get();
      if (!gatewayInfo) return;

      const registryStatus = useRegistryStore.getState().status;
      const isCompact = useUIStore.getState().compactCards;

      // Only preserve positions for nodes the user has explicitly dragged
      const { nodes, edges } = transformToNodesAndEdges(
        gatewayInfo,
        mcpServers,
        resources,
        draggedPositions.size > 0 ? draggedPositions : undefined,
        sessions,
        clients,
        registryStatus,
        codeMode,
        isCompact
      );
      set({ nodes, edges });
    },

    resetLayout: () => {
      const { gatewayInfo, mcpServers, resources, clients, sessions, codeMode } = get();
      if (!gatewayInfo) return;

      const registryStatus = useRegistryStore.getState().status;
      const isCompact = useUIStore.getState().compactCards;

      // Clear dragged positions and recalculate from scratch
      const { nodes, edges } = transformToNodesAndEdges(
        gatewayInfo,
        mcpServers,
        resources,
        undefined,
        sessions,
        clients,
        registryStatus,
        codeMode,
        isCompact
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
