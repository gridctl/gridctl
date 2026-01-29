/**
 * Edge creation functions for the butterfly layout
 *
 * Implements the hub-and-spoke edge pattern:
 * - Agents initiate: Agent -> Gateway
 * - Gateway exposes: Gateway -> MCP Servers
 * - Gateway manages: Gateway -> Resources
 * - Agents use: Agent -> MCP Servers/Agents (via uses field)
 */

import type { Edge } from '@xyflow/react';
import { MarkerType } from '@xyflow/react';
import type { MCPServerStatus, ResourceStatus, AgentStatus, ToolSelector } from '../../types';
import type { EdgeMetadata } from './types';
import { COLORS } from '../constants';

/** Gateway node ID constant */
export const GATEWAY_NODE_ID = 'gateway';

/** Arrow marker configuration */
const arrowMarker = {
  type: MarkerType.ArrowClosed,
  width: 16,
  height: 16,
  color: COLORS.edgeDefault,
};

/**
 * Extended edge type with metadata for highlighting
 */
export type EnhancedEdge = Edge & {
  data?: EdgeMetadata;
};

/**
 * Create edges from agents TO gateway (butterfly: agents on left initiate)
 *
 * Only creates edges for "primary" agents - those not used by other agents.
 * Worker agents connect via their orchestrator agent's "uses" edges.
 *
 * @param agents - All agents
 * @param usedByOtherAgents - Set of agent names that are used by other agents
 */
export function createAgentToGatewayEdges(
  agents: AgentStatus[],
  usedByOtherAgents: Set<string>
): EnhancedEdge[] {
  return agents
    .filter((agent) => !usedByOtherAgents.has(agent.name))
    .map((agent) => {
      const nodeId = `agent-${agent.name}`;
      const isRunning = agent.status === 'running';

      // Style varies based on A2A capability
      const edgeColor = agent.hasA2A ? COLORS.secondary : '#8b5cf6';
      const style = agent.hasA2A
        ? { stroke: edgeColor, strokeDasharray: '8,4', strokeWidth: 2 }
        : { stroke: edgeColor, strokeDasharray: '5,5' };

      return {
        id: `edge-agent-gateway-${agent.name}`,
        source: nodeId,
        target: GATEWAY_NODE_ID,
        animated: isRunning,
        style,
        markerEnd: { ...arrowMarker, color: edgeColor },
        data: {
          relationType: 'agent-to-gateway' as const,
          sourceAgent: agent.name,
          isHighlightable: true,
        },
      };
    });
}

/**
 * Create edges from gateway to MCP servers
 *
 * Gateway "exposes" MCP servers to agents.
 */
export function createGatewayToServerEdges(
  servers: MCPServerStatus[]
): EnhancedEdge[] {
  return servers.map((server) => ({
    id: `edge-gateway-mcp-${server.name}`,
    source: GATEWAY_NODE_ID,
    target: `mcp-${server.name}`,
    animated: server.initialized,
    markerEnd: { ...arrowMarker, color: COLORS.external },
    data: {
      relationType: 'gateway-to-server' as const,
      isHighlightable: true,
    },
  }));
}

/**
 * Create edges from gateway to resources
 *
 * Gateway "manages" resources (infrastructure).
 */
export function createGatewayToResourceEdges(
  resources: ResourceStatus[]
): EnhancedEdge[] {
  return resources.map((resource) => ({
    id: `edge-gateway-resource-${resource.name}`,
    source: GATEWAY_NODE_ID,
    target: `resource-${resource.name}`,
    animated: resource.status === 'running',
    markerEnd: { ...arrowMarker, color: COLORS.secondary },
    data: {
      relationType: 'gateway-to-resource' as const,
      isHighlightable: false, // Resources not in agent highlight path
    },
  }));
}

/**
 * Create edges for agent "uses" relationships
 *
 * Agents connect to MCP servers and other agents they use.
 * This creates the dependency visualization.
 */
export function createAgentUsesEdges(
  agents: AgentStatus[],
  mcpServerNames: Set<string>,
  agentNames: Set<string>
): EnhancedEdge[] {
  const edges: EnhancedEdge[] = [];

  for (const agent of agents) {
    if (!agent.uses) continue;

    for (const selector of agent.uses as ToolSelector[]) {
      const serverName = selector.server;

      // Determine target and relationship type
      // These "uses" edges are hidden by default and shown on agent selection
      if (mcpServerNames.has(serverName)) {
        edges.push({
          id: `edge-uses-${agent.name}-${serverName}`,
          source: `agent-${agent.name}`,
          target: `mcp-${serverName}`,
          animated: agent.status === 'running',
          style: {
            stroke: COLORS.secondary,
            strokeDasharray: '4,4',
            strokeWidth: 1.5,
          },
          markerEnd: { ...arrowMarker, color: COLORS.secondary },
          className: 'uses-edge', // Hidden by default
          data: {
            relationType: 'agent-uses-server' as const,
            sourceAgent: agent.name,
            isHighlightable: true,
            isUsesEdge: true,
          },
        });
      } else if (agentNames.has(serverName)) {
        edges.push({
          id: `edge-uses-${agent.name}-${serverName}`,
          source: `agent-${agent.name}`,
          target: `agent-${serverName}`,
          animated: agent.status === 'running',
          style: {
            stroke: COLORS.secondary,
            strokeDasharray: '4,4',
            strokeWidth: 1.5,
          },
          markerEnd: { ...arrowMarker, color: COLORS.secondary },
          className: 'uses-edge', // Hidden by default
          data: {
            relationType: 'agent-uses-agent' as const,
            sourceAgent: agent.name,
            isHighlightable: true,
            isUsesEdge: true,
          },
        });
      }
    }
  }

  return edges;
}

/**
 * Build sets for identifying node types
 * Used for edge routing decisions
 */
export function buildNodeTypeSets(
  mcpServers: MCPServerStatus[],
  agents: AgentStatus[]
): { mcpServerNames: Set<string>; agentNames: Set<string>; usedByOtherAgents: Set<string> } {
  const mcpServerNames = new Set(mcpServers.map((s) => s.name));
  const agentNames = new Set(agents.map((a) => a.name));

  // Track which agents are "used" by other agents
  const usedByOtherAgents = new Set<string>();
  agents.forEach((agent) => {
    agent.uses?.forEach((selector: ToolSelector) => {
      const serverName = selector.server;
      if (agentNames.has(serverName)) {
        usedByOtherAgents.add(serverName);
      }
    });
  });

  return { mcpServerNames, agentNames, usedByOtherAgents };
}

/**
 * Create all edges for the butterfly layout
 *
 * Combines all edge types:
 * - Agent -> Gateway (primary agents only)
 * - Gateway -> MCP Servers
 * - Gateway -> Resources
 * - Agent -> things it uses
 */
export function createAllEdges(
  mcpServers: MCPServerStatus[],
  resources: ResourceStatus[],
  agents: AgentStatus[]
): EnhancedEdge[] {
  const { mcpServerNames, agentNames, usedByOtherAgents } = buildNodeTypeSets(
    mcpServers,
    agents
  );

  return [
    ...createAgentToGatewayEdges(agents, usedByOtherAgents),
    ...createGatewayToServerEdges(mcpServers),
    ...createGatewayToResourceEdges(resources),
    ...createAgentUsesEdges(agents, mcpServerNames, agentNames),
  ];
}
