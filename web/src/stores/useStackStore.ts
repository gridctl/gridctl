import { create } from 'zustand';
import { subscribeWithSelector } from 'zustand/middleware';
import type { Node, Edge, NodeChange } from '@xyflow/react';
import type {
  GatewayStatus,
  ServerInfo,
  MCPServerStatus,
  ResourceStatus,
  ClientStatus,
  SessionEntry,
  Tool,
  TokenUsage,
  FeatureDetail,
  ConnectionStatus,
  AutoscaleDecisionKind,
} from '../types';
import { transformToNodesAndEdges } from '../lib/transform';
import { showToast } from '../components/ui/Toast';
import { appendToolFanout } from '../lib/graph/toolFanout';
import { reuseGraphIdentities, structurallyEqual } from '../lib/graph/identity';
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
  // Full downstream tool inventory (raw descriptions + schemas) from
  // /api/tools/catalog. Unlike `tools` (the MCP-facing aggregated list, which
  // is just the meta-tools in code mode), this always carries per-tool detail,
  // so the Tools workspace sources descriptions/schemas/search from it.
  toolCatalog: Tool[];
  sessions: number;
  // Session entries from /api/sessions, polled while Connections is open.
  // null = not fetched. StatusBar prefers this over the status-poll count
  // when present, so the two surfaces read one array and cannot diverge.
  sessionEntries: SessionEntry[] | null;
  codeMode: string | null;  // Gateway code mode status ("on" when active)
  toolDiscovery: string | null; // "deferred" when deferred discovery is active
  tokenUsage: TokenUsage | null; // Token usage metrics from status response
  stackName: string;        // Active stack name; empty string in stackless mode
  features: Record<string, boolean>;  // Enabled experimental flags, name -> true
  featureDetails: FeatureDetail[];    // Display metadata for the same flags (read-only)

  // === React Flow State ===
  nodes: Node[];
  edges: Edge[];
  draggedPositions: Map<string, { x: number; y: number }>;  // User-dragged node positions

  // === Autoscale observability (client-derived from polling) ===
  autoscaleHistory: Record<string, AutoscaleSample[]>;
  autoscaleDecisions: Record<string, AutoscaleDecision[]>;
  // Last-seen raw timestamps, used to diff polls and detect new scale events.
  autoscaleLastSeen: Record<string, { upAt?: string; downAt?: string }>;
  // Servers whose token total rose since the previous poll. The canvas animates
  // those gateway edges, capped, instead of every live edge.
  activeServers: Set<string>;
  tokenTotalsLastSeen: Record<string, number>;

  // === UI State ===
  selectedNodeId: string | null;
  // Server node ids (e.g. "mcp-github") whose tools are fanned out on the
  // canvas. Independent per server and preserved across polling refreshes so
  // an expanded server stays expanded when the graph data refetches.
  expandedServers: Set<string>;
  // Bumped whenever resetLayout recomputes positions from scratch (reset
  // button, compact-cards toggle) so the canvas can refit even though the
  // node id set is unchanged.
  layoutEpoch: number;
  connectionStatus: ConnectionStatus;
  lastUpdated: Date | null;
  isLoading: boolean;
  error: string | null;

  // === Actions ===
  setGatewayStatus: (status: GatewayStatus) => void;
  setClients: (clients: ClientStatus[]) => void;
  setSessionEntries: (entries: SessionEntry[] | null) => void;
  setTools: (tools: Tool[]) => void;
  setToolCatalog: (toolCatalog: Tool[]) => void;
  setError: (error: string | null) => void;
  setLoading: (loading: boolean) => void;
  setConnectionStatus: (status: ConnectionStatus) => void;
  selectNode: (nodeId: string | null) => void;
  toggleServerExpanded: (serverId: string) => void;
  expandServer: (serverId: string) => void;
  collapseServer: (serverId: string) => void;
  collapseAllServers: () => void;
  refreshNodesAndEdges: () => void;
  resetLayout: () => void;

  setNodes: (nodes: Node[]) => void;
  setEdges: (edges: Edge[]) => void;
  // A position change with dragging === false and a position records the drag.
  recordDraggedPositions: (changes: NodeChange[]) => void;
}

export const useStackStore = create<StackState>()(
  subscribeWithSelector((set, get) => ({
    // Initial state
    gatewayInfo: null,
    mcpServers: [],
    resources: [],
    clients: [],
    tools: [],
    toolCatalog: [],
    sessions: 0,
    sessionEntries: null,
    codeMode: null,
    toolDiscovery: null,
    tokenUsage: null,
    stackName: '',
    features: {},
    featureDetails: [],
    nodes: [],
    edges: [],
    draggedPositions: new Map(),
    autoscaleHistory: {},
    autoscaleDecisions: {},
    autoscaleLastSeen: {},
    activeServers: new Set<string>(),
    tokenTotalsLastSeen: {},
    selectedNodeId: null,
    expandedServers: new Set<string>(),
    layoutEpoch: 0,
    connectionStatus: 'disconnected',
    lastUpdated: null,
    isLoading: true,
    error: null,

    // Actions
    setGatewayStatus: (status) => {
      const mcpServers = status['mcp-servers'] || [];
      const { autoscaleHistory, autoscaleDecisions, autoscaleLastSeen } = get();

      // Toast once per transition INTO needs_auth. First sight establishes a
      // baseline only (a page load over an already-pending server must not
      // toast), and a server staying pending must not re-toast every poll.
      const prevAuth = new Map(get().mcpServers.map((s) => [s.name, s.authStatus]));
      for (const s of mcpServers) {
        if (
          s.authStatus === 'needs_auth' &&
          prevAuth.has(s.name) &&
          prevAuth.get(s.name) !== 'needs_auth'
        ) {
          showToast('warning', `${s.name} requires authorization`, {
            action: {
              label: 'Authorize',
              onClick: () => {
                get().selectNode(`mcp-${s.name}`);
                useUIStore.getState().setSidebarOpen(true);
              },
            },
          });
        }
      }
      const folded = updateAutoscaleObservability(
        mcpServers,
        autoscaleHistory,
        autoscaleDecisions,
        autoscaleLastSeen,
      );
      const tokens = observeTokenTotals(
        status.token_usage,
        get().tokenTotalsLastSeen,
        get().activeServers,
      );
      set({
        gatewayInfo: status.gateway,
        mcpServers,
        resources: status.resources || [],
        sessions: status.sessions ?? 0,
        codeMode: status.code_mode || null,
        toolDiscovery: status.tool_discovery || null,
        tokenUsage: status.token_usage ?? null,
        stackName: status.stack_name || '',
        features: status.features ?? {},
        featureDetails: status.feature_details ?? [],
        autoscaleHistory: folded.history,
        autoscaleDecisions: folded.decisions,
        autoscaleLastSeen: folded.lastSeen,
        activeServers: tokens.activeServers,
        tokenTotalsLastSeen: tokens.tokenTotalsLastSeen,
        lastUpdated: new Date(),
        isLoading: false,
        error: null,
        connectionStatus: 'connected',
      });
      get().refreshNodesAndEdges();
    },

    setSessionEntries: (entries) => set({ sessionEntries: entries }),
    setClients: (clients) => {
      // The poll applies status before clients. Skip a no-op write, and
      // refresh when the list changed so a link, unlink, or scope edit paints
      // in the same cycle instead of waiting for the next status poll.
      if (structurallyEqual(get().clients, clients)) return;
      set({ clients });
      if (get().gatewayInfo) get().refreshNodesAndEdges();
    },

    setTools: (tools) => set({ tools }),

    setToolCatalog: (toolCatalog) => set({ toolCatalog }),

    setError: (error) => set({
      error,
      isLoading: false,
      connectionStatus: error ? 'error' : get().connectionStatus,
    }),

    setLoading: (isLoading) => set({ isLoading }),

    setConnectionStatus: (connectionStatus) => set({ connectionStatus }),

    // selected is carried across polls, so clearing the store selection has to
    // clear React Flow's flag too. Escape and the inspector close often run
    // while focus is outside the node, where React Flow's own handler does not.
    selectNode: (nodeId) => {
      if (nodeId === null) {
        const nodes = get().nodes;
        if (nodes.some((node) => node.selected)) {
          set({
            nodes: nodes.map((node) => (node.selected ? { ...node, selected: false } : node)),
            selectedNodeId: null,
          });
          return;
        }
      }
      set({ selectedNodeId: nodeId });
    },

    toggleServerExpanded: (serverId) => {
      const next = new Set(get().expandedServers);
      if (next.has(serverId)) {
        next.delete(serverId);
      } else {
        next.add(serverId);
      }
      set({ expandedServers: next });
      get().refreshNodesAndEdges();
    },

    expandServer: (serverId) => {
      if (get().expandedServers.has(serverId)) return;
      const next = new Set(get().expandedServers);
      next.add(serverId);
      set({ expandedServers: next });
      get().refreshNodesAndEdges();
    },

    collapseServer: (serverId) => {
      if (!get().expandedServers.has(serverId)) return;
      const next = new Set(get().expandedServers);
      next.delete(serverId);
      set({ expandedServers: next });
      get().refreshNodesAndEdges();
    },

    // A blank-pane click resets the canvas to its default view; collapsing
    // every fan-out also unmounts any open tool detail popover, whose state
    // lives in the tool nodes themselves.
    collapseAllServers: () => {
      if (get().expandedServers.size === 0) return;
      set({ expandedServers: new Set<string>() });
      get().refreshNodesAndEdges();
    },

    refreshNodesAndEdges: () => {
      const { gatewayInfo, mcpServers, resources, clients, sessions, codeMode, toolDiscovery, draggedPositions } = get();
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
        toolDiscovery,
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

      // Append tool fan-out for any expanded servers AFTER backbone layout so
      // expanding never reflows the three-column backbone (tool nodes are laid
      // out locally relative to their server, not through the zone engine).
      const fanned = appendToolFanout(nodes, edges, get().expandedServers, {
        compact: isCompact,
        draggedPositions: draggedPositions.size > 0 ? draggedPositions : undefined,
      });

      // Reuse after pin annotation and fan-out so a reused node cannot keep a
      // stale pinStatus. Skip the write when both arrays are unchanged.
      const previousNodes = get().nodes;
      const previousEdges = get().edges;
      const reused = reuseGraphIdentities(
        previousNodes,
        fanned.nodes,
        previousEdges,
        fanned.edges,
      );
      if (reused.nodes === previousNodes && reused.edges === previousEdges) return;
      set({ nodes: reused.nodes, edges: reused.edges });
    },

    resetLayout: () => {
      const { gatewayInfo, mcpServers, resources, clients, sessions, codeMode, toolDiscovery } = get();
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
        toolDiscovery,
        isCompact,
      );
      // Re-derive tool fan-out from scratch too (positions are local to the
      // freshly-laid-out servers; no dragged positions survive a reset).
      const fanned = appendToolFanout(nodes, edges, get().expandedServers, {
        compact: isCompact,
      });
      set({
        nodes: fanned.nodes,
        edges: fanned.edges,
        draggedPositions: new Map(),
        layoutEpoch: get().layoutEpoch + 1,
      });
    },

    setNodes: (nodes) => set({ nodes }),

    setEdges: (edges) => set({ edges }),

    recordDraggedPositions: (changes) => {
      const draggedPositions = new Map(get().draggedPositions);
      let changed = false;
      for (const change of changes) {
        if (change.type === 'position' && change.dragging === false && change.position) {
          draggedPositions.set(change.id, change.position);
          changed = true;
        }
      }
      if (changed) set({ draggedPositions });
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

function sameMembers(a: Set<string>, b: Set<string>): boolean {
  if (a.size !== b.size) return false;
  for (const item of a) {
    if (!b.has(item)) return false;
  }
  return true;
}

// Servers whose token total rose since the previous poll. First sight is a
// baseline only, matching the autoscale decision feed. The previous active set
// is reused when membership is unchanged so the canvas does not re-render.
function observeTokenTotals(
  usage: TokenUsage | null | undefined,
  prevTotals: Record<string, number>,
  prevActive: Set<string>,
): { activeServers: Set<string>; tokenTotalsLastSeen: Record<string, number> } {
  const tokenTotalsLastSeen: Record<string, number> = {};
  const nextActive = new Set<string>();
  for (const [name, counts] of Object.entries(usage?.per_server ?? {})) {
    const total = counts.total_tokens ?? 0;
    tokenTotalsLastSeen[name] = total;
    const seen = prevTotals[name];
    if (seen !== undefined && total > seen) nextActive.add(name);
  }
  return {
    activeServers: sameMembers(nextActive, prevActive) ? prevActive : nextActive,
    tokenTotalsLastSeen,
  };
}

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
