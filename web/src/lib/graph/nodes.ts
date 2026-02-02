/**
 * Node creation functions for the butterfly layout
 *
 * Creates React Flow nodes from backend data with proper typing and structure.
 */

import type { Node } from '@xyflow/react';
import type { MCPServerStatus, ResourceStatus, AgentStatus, NodeStatus } from '../../types';
import { NODE_TYPES } from '../constants';
import { GATEWAY_NODE_ID } from './edges';

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
 * Create the gateway node
 *
 * @param gatewayInfo - Gateway name and version
 * @param mcpServers - MCP servers for count
 * @param resources - Resources for count
 * @param agents - Agents for counts (total and A2A)
 */
export function createGatewayNode(
  gatewayInfo: { name: string; version: string },
  mcpServers: MCPServerStatus[],
  resources: ResourceStatus[],
  agents: AgentStatus[]
): Node {
  // Calculate total tool count (MCP server tools + A2A agent skills)
  const safeServers = mcpServers ?? [];
  const safeResources = resources ?? [];
  const safeAgents = agents ?? [];

  const mcpToolCount = safeServers.reduce((sum, s) => sum + s.toolCount, 0);
  const a2aSkillCount = safeAgents.reduce((sum, a) => sum + (a.skillCount || 0), 0);
  const totalToolCount = mcpToolCount + a2aSkillCount;

  // Count agents with A2A capability
  const a2aAgentCount = safeAgents.filter((a) => a.hasA2A).length;

  return {
    id: GATEWAY_NODE_ID,
    type: NODE_TYPES.GATEWAY,
    position: { x: 0, y: 0 }, // Will be calculated by layout engine
    data: {
      type: 'gateway',
      name: gatewayInfo.name,
      version: gatewayInfo.version,
      serverCount: safeServers.length,
      resourceCount: safeResources.length,
      agentCount: safeAgents.length,
      a2aAgentCount,
      totalToolCount,
    },
    draggable: true,
  };
}

/**
 * Create MCP server nodes
 */
export function createMCPServerNodes(mcpServers: MCPServerStatus[]): Node[] {
  return mcpServers.map((server) => ({
    id: `mcp-${server.name}`,
    type: NODE_TYPES.MCP_SERVER,
    position: { x: 0, y: 0 }, // Will be calculated by layout engine
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
  }));
}

/**
 * Create agent nodes
 *
 * @param agents - All agents
 * @param usedByOtherAgents - Set of agent names that are workers (used by other agents)
 */
export function createAgentNodes(
  agents: AgentStatus[],
  usedByOtherAgents: Set<string>
): Node[] {
  return agents.map((agent) => ({
    id: `agent-${agent.name}`,
    type: NODE_TYPES.AGENT,
    position: { x: 0, y: 0 }, // Will be calculated by layout engine
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
      // Hierarchy info
      isWorker: usedByOtherAgents.has(agent.name),
    },
    draggable: true,
  }));
}

/**
 * Create resource nodes
 */
export function createResourceNodes(resources: ResourceStatus[]): Node[] {
  return resources.map((resource) => ({
    id: `resource-${resource.name}`,
    type: NODE_TYPES.RESOURCE,
    position: { x: 0, y: 0 }, // Will be calculated by layout engine
    data: {
      type: 'resource',
      name: resource.name,
      image: resource.image,
      network: resource.network,
      status: resource.status,
    },
    draggable: true,
  }));
}

/**
 * Create all nodes for the graph
 */
export function createAllNodes(
  gatewayInfo: { name: string; version: string },
  mcpServers: MCPServerStatus[],
  resources: ResourceStatus[],
  agents: AgentStatus[],
  usedByOtherAgents: Set<string>
): Node[] {
  return [
    createGatewayNode(gatewayInfo, mcpServers, resources, agents),
    ...createMCPServerNodes(mcpServers),
    ...createAgentNodes(agents, usedByOtherAgents),
    ...createResourceNodes(resources),
  ];
}
