import type { Node, Edge } from '@xyflow/react';
import { MarkerType } from '@xyflow/react';
import type {
  MCPServerStatus,
  ResourceStatus,
  AgentStatus,
  NodeStatus,
  ToolSelector,
} from '../types';
import { NODE_TYPES, COLORS, TOOL_NAME_DELIMITER } from './constants';
import { applyDagreLayout, applyDagreLayoutWithPreserved } from './layout';

// Arrow marker for edge endpoints
const arrowMarker = {
  type: MarkerType.ArrowClosed,
  width: 16,
  height: 16,
  color: COLORS.edgeDefault,
};

// Gateway node ID constant
export const GATEWAY_NODE_ID = 'gateway';

/**
 * Determine MCP server status based on its state
 */
function getMCPServerStatus(server: MCPServerStatus): NodeStatus {
  if (!server.initialized) {
    return 'initializing';
  }
  return 'running';
}

/**
 * Transform backend data to React Flow nodes and edges with Left-to-Right dagre layout
 *
 * Layout tiers (Left to Right):
 * - Tier 1: Gateway (entry point/controller) - far left
 * - Tier 2: MCP Servers, Local Agents - middle (connected to gateway)
 * - Tier 3: Resources, Remote Agents - right side (can be dependencies)
 *
 * @param existingPositions - Optional map of node IDs to positions to preserve user-dragged positions
 */
export function transformToNodesAndEdges(
  gatewayInfo: { name: string; version: string },
  mcpServers: MCPServerStatus[],
  resources: ResourceStatus[] = [],
  agents: AgentStatus[] = [],
  existingPositions?: Map<string, { x: number; y: number }>
): { nodes: Node[]; edges: Edge[] } {
  const nodes: Node[] = [];
  const edges: Edge[] = [];

  // Calculate total tool count (MCP server tools + A2A agent skills)
  const mcpToolCount = mcpServers.reduce((sum, s) => sum + s.toolCount, 0);
  const a2aSkillCount = agents.reduce((sum, a) => sum + (a.skillCount || 0), 0);
  const totalToolCount = mcpToolCount + a2aSkillCount;

  // Count agents with A2A capability for gateway stats
  const a2aAgentCount = agents.filter((a) => a.hasA2A).length;

  // === Create Gateway Node (Tier 1 - Root) ===
  const gatewayNode: Node = {
    id: GATEWAY_NODE_ID,
    type: NODE_TYPES.GATEWAY,
    position: { x: 0, y: 0 }, // Will be calculated by dagre
    data: {
      type: 'gateway',
      name: gatewayInfo.name,
      version: gatewayInfo.version,
      serverCount: mcpServers.length,
      resourceCount: resources.length,
      agentCount: agents.length,
      a2aAgentCount,
      totalToolCount,
    },
    draggable: true,
  };
  nodes.push(gatewayNode);

  // === Create MCP Server Nodes (Tier 2) ===
  mcpServers.forEach((server) => {
    const nodeId = `mcp-${server.name}`;

    const serverNode: Node = {
      id: nodeId,
      type: NODE_TYPES.MCP_SERVER,
      position: { x: 0, y: 0 }, // Will be calculated by dagre
      data: {
        type: 'mcp-server',
        name: server.name,
        transport: server.transport || 'http',
        endpoint: server.endpoint,
        containerId: server.containerId,
        initialized: server.initialized,
        toolCount: server.toolCount,
        tools: server.tools,
        status: getMCPServerStatus(server),
        external: server.external,
        localProcess: server.localProcess,
        ssh: server.ssh,
        sshHost: server.sshHost,
      },
      draggable: true,
    };
    nodes.push(serverNode);

    // Edge: Gateway -> MCP Server (violet to match target node)
    edges.push({
      id: `edge-gateway-${server.name}`,
      source: GATEWAY_NODE_ID,
      target: nodeId,
      animated: server.initialized,
      markerEnd: { ...arrowMarker, color: COLORS.external },
    });
  });

  // Build sets for identifying node types (for uses edge routing)
  const mcpServerNames = new Set(mcpServers.map((s) => s.name));
  const agentNames = new Set(agents.map((a) => a.name));

  // Track which agents are "used" by other agents (they shouldn't connect directly to gateway)
  const usedByOtherAgents = new Set<string>();
  agents.forEach((agent) => {
    agent.uses?.forEach((selector: ToolSelector) => {
      const serverName = selector.server;
      if (agentNames.has(serverName)) {
        usedByOtherAgents.add(serverName);
      }
    });
  });

  // === Create Agent Nodes (Tier 2 for local, Tier 3 for remote) ===
  agents.forEach((agent) => {
    const nodeId = `agent-${agent.name}`;
    const isRunning = agent.status === 'running';

    const agentNode: Node = {
      id: nodeId,
      type: NODE_TYPES.AGENT,
      position: { x: 0, y: 0 }, // Will be calculated by dagre
      data: {
        type: 'agent',
        name: agent.name,
        status: agent.status,
        variant: agent.variant,
        // Container fields (local variant)
        image: agent.image,
        containerId: agent.containerId,
        uses: agent.uses,
        // A2A fields (when hasA2A is true)
        hasA2A: agent.hasA2A,
        role: agent.role,
        url: agent.url,
        endpoint: agent.endpoint,
        skillCount: agent.skillCount,
        skills: agent.skills,
        description: agent.description,
      },
      draggable: true,
    };
    nodes.push(agentNode);

    // Edge: Gateway -> Agent (only if not used by another agent)
    // This creates proper hierarchy: gateway -> orchestrator -> worker agents
    if (!usedByOtherAgents.has(agent.name)) {
      // Style varies based on whether agent has A2A capability
      const edgeColor = agent.hasA2A ? COLORS.secondary : '#8b5cf6';
      const edgeStyle = agent.hasA2A
        ? { stroke: edgeColor, strokeDasharray: '8,4', strokeWidth: 2 }
        : { stroke: edgeColor, strokeDasharray: '5,5' };

      edges.push({
        id: `edge-gateway-agent-${agent.name}`,
        source: GATEWAY_NODE_ID,
        target: nodeId,
        animated: isRunning,
        style: edgeStyle,
        markerEnd: { ...arrowMarker, color: edgeColor },
      });
    }

    // Edges: Agent -> things it uses (MCP servers or other agents)
    agent.uses?.forEach((selector: ToolSelector) => {
      const serverName = selector.server;
      let targetNodeId: string | null = null;

      if (mcpServerNames.has(serverName)) {
        targetNodeId = `mcp-${serverName}`;
      } else if (agentNames.has(serverName)) {
        // Agent using another agent (via A2A)
        targetNodeId = `agent-${serverName}`;
      }

      if (targetNodeId) {
        edges.push({
          id: `edge-uses-${agent.name}-${serverName}`,
          source: nodeId,
          target: targetNodeId,
          animated: isRunning,
          style: {
            stroke: COLORS.secondary,
            strokeDasharray: '4,4',
            strokeWidth: 1.5,
          },
          markerEnd: { ...arrowMarker, color: COLORS.secondary },
        });
      }
    });
  });

  // === Create Resource Nodes (Tier 3) ===
  resources.forEach((resource) => {
    const nodeId = `resource-${resource.name}`;

    const resourceNode: Node = {
      id: nodeId,
      type: NODE_TYPES.RESOURCE,
      position: { x: 0, y: 0 }, // Will be calculated by dagre
      data: {
        type: 'resource',
        name: resource.name,
        image: resource.image,
        network: resource.network,
        status: resource.status,
      },
      draggable: true,
    };
    nodes.push(resourceNode);

    // Edge: Gateway -> Resource
    edges.push({
      id: `edge-gateway-${resource.name}`,
      source: GATEWAY_NODE_ID,
      target: nodeId,
      animated: resource.status === 'running',
      markerEnd: { ...arrowMarker, color: COLORS.secondary },
    });
  });

  // === Apply Dagre Layout (Left-to-Right) ===
  const layoutedNodes = existingPositions
    ? applyDagreLayoutWithPreserved(nodes, edges, existingPositions, { direction: 'LR' })
    : applyDagreLayout(nodes, edges, { direction: 'LR' });

  return { nodes: layoutedNodes, edges };
}

/**
 * Parse prefixed tool name into agent and tool names
 * Matches the format from pkg/mcp/router.go: "agent__tool"
 */
export function parsePrefixedToolName(prefixed: string): {
  agentName: string;
  toolName: string;
} {
  const idx = prefixed.indexOf(TOOL_NAME_DELIMITER);
  if (idx === -1) {
    return { agentName: '', toolName: prefixed };
  }
  return {
    agentName: prefixed.slice(0, idx),
    toolName: prefixed.slice(idx + TOOL_NAME_DELIMITER.length),
  };
}

/**
 * Group tools by their owning MCP server
 */
export function groupToolsByServer(
  tools: { name: string }[]
): Map<string, string[]> {
  const grouped = new Map<string, string[]>();

  for (const tool of tools) {
    const { agentName, toolName } = parsePrefixedToolName(tool.name);
    const existing = grouped.get(agentName) || [];
    existing.push(toolName);
    grouped.set(agentName, existing);
  }

  return grouped;
}
