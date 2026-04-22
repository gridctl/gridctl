import { create } from 'zustand';
import { subscribeWithSelector } from 'zustand/middleware';
import type { Node, Edge, NodeChange, EdgeChange } from '@xyflow/react';
import { applyNodeChanges, applyEdgeChanges } from '@xyflow/react';
import type {
  GatewayStatus,
  ServerInfo,
  MCPServerStatus,
  ResourceStatus,
  ClientStatus,
  Tool,
  TokenUsage,
  ConnectionStatus,
  AutoscaleDecisionKind,
} from '../types';
import { transformToNodesAndEdges } from '../lib/transform';
import { useRegistryStore } from './useRegistryStore';
import { useUIStore } from './useUIStore';
import { usePinsStore } from './usePinsStore';

// Per-server autoscale sample. Populated every poll cycle; capped at
// AUTOSCALE_HISTORY_CAP entries so the ring buffer stays O(1) in memory.
export interface AutoscaleSample {
  t: number;              // ms since epoch (client-local)
  current: number;
  target: number;
  medianInFlight: number;
}

// Client-derived scale event. The backend exposes only the last-decision
// snapshot (lastScaleUpAt/lastScaleDownAt), not an event stream, so we
// diff consecutive polls. Two scale events within one polling window
// (3s) can be coalesced — that's a known limitation, not a bug.
export interface AutoscaleDecision {
  t: number;              // ms since epoch (client-local observation time)
  kind: Exclude<AutoscaleDecisionKind, 'noop'>;
  from: number;
  to: number;
  reason: string;
}

export const AUTOSCALE_HISTORY_CAP = 120; // 6 minutes at 3s polling
export const AUTOSCALE_DECISIONS_CAP = 10;

interface StackState {
  // === Raw API Data ===
  gatewayInfo: ServerInfo | null;
  mcpServers: MCPServerStatus[];
  resources: ResourceStatus[];
  clients: ClientStatus[];  // Detected/linked LLM clients
  tools: Tool[];
  sessions: number;
  codeMode: string | null;  // Gateway code mode status ("on" when active)
  tokenUsage: TokenUsage | null; // Token usage metrics from status response
  stackName: string;        // Active stack name; empty string in stackless mode

  // === React Flow State ===
  nodes: Node[];
  edges: Edge[];
  draggedPositions: Map<string, { x: number; y: number }>;  // User-dragged node positions

  // === Autoscale observability (client-derived from polling) ===
  autoscaleHistory: Record<string, AutoscaleSample[]>;
  autoscaleDecisions: Record<string, AutoscaleDecision[]>;
  // Last-seen raw timestamps, used to diff polls and detect new scale events.
  autoscaleLastSeen: Record<string, { upAt?: string; downAt?: string }>;

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
    stackName: '',
    nodes: [],
    edges: [],
    draggedPositions: new Map(),
    autoscaleHistory: {},
    autoscaleDecisions: {},
    autoscaleLastSeen: {},
    selectedNodeId: null,
    connectionStatus: 'disconnected',
    lastUpdated: null,
    isLoading: true,
    error: null,

    // Actions
    setGatewayStatus: (status) => {
      const mcpServers = status['mcp-servers'] || [];
      const { autoscaleHistory, autoscaleDecisions, autoscaleLastSeen } = get();
      const folded = updateAutoscaleObservability(
        mcpServers,
        autoscaleHistory,
        autoscaleDecisions,
        autoscaleLastSeen,
      );
      set({
        gatewayInfo: status.gateway,
        mcpServers,
        resources: status.resources || [],
        sessions: status.sessions ?? 0,
        codeMode: status.code_mode || null,
        tokenUsage: status.token_usage ?? null,
        stackName: status.stack_name || '',
        autoscaleHistory: folded.history,
        autoscaleDecisions: folded.decisions,
        autoscaleLastSeen: folded.lastSeen,
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
        isCompact,
      );

      // Cross-reference pins store to annotate MCP server nodes with pin state
      const pins = usePinsStore.getState().pins;
      if (pins) {
        for (const node of nodes) {
          const d = node.data as Record<string, unknown>;
          if (d.type === 'mcp-server' && typeof d.name === 'string') {
            const sp = pins[d.name];
            if (sp) {
              d.pinStatus = sp.status;
              d.pinDriftCount = sp.tool_count;
            }
          }
        }
      }

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
        isCompact,
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

// Fold the latest poll into the autoscale ring buffer and derived decision
// feed. Exported for unit-testing; not intended for direct use from components.
export function updateAutoscaleObservability(
  mcpServers: MCPServerStatus[],
  prevHistory: Record<string, AutoscaleSample[]>,
  prevDecisions: Record<string, AutoscaleDecision[]>,
  prevLastSeen: Record<string, { upAt?: string; downAt?: string }>,
): {
  history: Record<string, AutoscaleSample[]>;
  decisions: Record<string, AutoscaleDecision[]>;
  lastSeen: Record<string, { upAt?: string; downAt?: string }>;
} {
  const now = Date.now();
  const history: Record<string, AutoscaleSample[]> = { ...prevHistory };
  const decisions: Record<string, AutoscaleDecision[]> = { ...prevDecisions };
  const lastSeen: Record<string, { upAt?: string; downAt?: string }> = { ...prevLastSeen };

  for (const server of mcpServers) {
    const as = server.autoscale;
    if (!as) continue;

    const prevSamples = history[server.name] ?? [];
    const lastSample = prevSamples[prevSamples.length - 1];
    const nextSample: AutoscaleSample = {
      t: now,
      current: as.current,
      target: as.target,
      medianInFlight: as.medianInFlight,
    };
    const nextSamples =
      prevSamples.length >= AUTOSCALE_HISTORY_CAP
        ? [...prevSamples.slice(prevSamples.length - AUTOSCALE_HISTORY_CAP + 1), nextSample]
        : [...prevSamples, nextSample];
    history[server.name] = nextSamples;

    // Derive a decision entry when lastScaleUpAt/lastScaleDownAt advances vs.
    // the last observation. First-sight values establish a baseline only —
    // recording them would flood the feed with stale events on a page refresh.
    // Two scale events inside one 3s polling window can coalesce — accepted
    // limitation of snapshot-diff observation.
    const seenBefore = server.name in prevLastSeen;
    const seen = prevLastSeen[server.name] ?? {};
    const newDecisions: AutoscaleDecision[] = [];
    if (seenBefore && as.lastScaleUpAt && as.lastScaleUpAt !== seen.upAt) {
      newDecisions.push({
        t: Date.parse(as.lastScaleUpAt) || now,
        kind: 'up',
        from: lastSample?.current ?? as.current,
        to: as.current,
        reason: `median in-flight ${as.medianInFlight} ≥ target ${as.targetInFlight}`,
      });
    }
    if (seenBefore && as.lastScaleDownAt && as.lastScaleDownAt !== seen.downAt) {
      newDecisions.push({
        t: Date.parse(as.lastScaleDownAt) || now,
        kind: 'down',
        from: lastSample?.current ?? as.current,
        to: as.current,
        reason: `median in-flight ${as.medianInFlight} below target ${as.targetInFlight}`,
      });
    }
    if (newDecisions.length > 0) {
      const prevServerDecisions = prevDecisions[server.name] ?? [];
      // Newest first within this tick; then prepend to prior feed; cap.
      newDecisions.sort((a, b) => b.t - a.t);
      decisions[server.name] = [...newDecisions, ...prevServerDecisions].slice(0, AUTOSCALE_DECISIONS_CAP);
    }
    lastSeen[server.name] = {
      upAt: as.lastScaleUpAt,
      downAt: as.lastScaleDownAt,
    };
  }

  return { history, decisions, lastSeen };
}
