/**
 * Node creation functions for the butterfly layout
 *
 * Creates React Flow nodes from backend data with proper typing and structure.
 */

import type { Node } from '@xyflow/react';
import type { MCPServerStatus, ResourceStatus, ClientStatus, NodeStatus, RegistryStatus } from '../../types';
import { NODE_TYPES } from '../constants';
import { GATEWAY_NODE_ID } from './edges';

/**
 * Determine MCP server status based on its state and health
 */
function getMCPServerStatus(server: MCPServerStatus): NodeStatus {
  if (!server.initialized) {
    return 'initializing';
  }
  // If health check has run and server is unhealthy, show error
  if (server.healthy === false) {
    return 'error';
  }
  return 'running';
}

/**
 * Create the gateway node
 *
 * @param gatewayInfo - Gateway name and version
 * @param mcpServers - MCP servers for count
 * @param resources - Resources for count
 * @param sessions - Active MCP session count
 */
export function createGatewayNode(
  gatewayInfo: { name: string; version: string },
  mcpServers: MCPServerStatus[],
  resources: ResourceStatus[],
  sessions?: number,
  clientCount?: number,
  codeMode?: string | null,
  registryStatus?: RegistryStatus | null
): Node {
  const safeServers = mcpServers ?? [];
  const safeResources = resources ?? [];

  const totalToolCount = safeServers.reduce((sum, s) => sum + s.toolCount, 0);

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
      clientCount: clientCount ?? 0,
      totalToolCount,
      sessions: sessions ?? 0,
      codeMode: codeMode ?? null,
      totalSkills: registryStatus?.totalSkills ?? 0,
      activeSkills: registryStatus?.activeSkills ?? 0,
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
      healthy: server.healthy,
      lastCheck: server.lastCheck,
      healthError: server.healthError,
      openapi: server.openapi,
      openapiSpec: server.openapiSpec,
      outputFormat: server.outputFormat,
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
 * Create client nodes for linked LLM clients
 */
export function createClientNodes(clients: ClientStatus[]): Node[] {
  return clients
    .filter((c) => c.linked)
    .map((client) => ({
      id: `client-${client.slug}`,
      type: NODE_TYPES.CLIENT,
      position: { x: 0, y: 0 },
      data: {
        type: 'client',
        name: client.name,
        slug: client.slug,
        transport: client.transport,
        configPath: client.configPath,
        status: 'running' as const,
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
  sessions?: number,
  clients: ClientStatus[] = [],
  registryStatus?: RegistryStatus | null,
  codeMode?: string | null
): Node[] {
  const linkedClients = clients.filter((c) => c.linked);
  const nodes: Node[] = [
    createGatewayNode(gatewayInfo, mcpServers, resources, sessions, linkedClients.length, codeMode, registryStatus),
    ...createClientNodes(clients),
    ...createMCPServerNodes(mcpServers),
    ...createResourceNodes(resources),
  ];

  return nodes;
}
