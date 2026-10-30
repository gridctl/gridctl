/**
 * Edge creation functions for the butterfly layout
 *
 * Implements the hub-and-spoke edge pattern:
 * - Client -> Gateway (linked LLM clients)
 * - Gateway exposes: Gateway -> MCP Servers
 * - Gateway manages: Gateway -> Resources
 */

import type { Edge } from '@xyflow/react';
import type { MCPServerStatus, ResourceStatus, ClientStatus, AgentSkill } from '../../types';
import type { EdgeMetadata } from './types';
import { COLORS } from '../constants';

/** Gateway node ID constant */
export const GATEWAY_NODE_ID = 'gateway';

/** Arrow marker configuration. The literal keeps React Flow's marker type without importing the enum. */
function arrowMarker(color: string): { type: 'arrowclosed'; width: number; height: number; color: string } {
  return {
    type: 'arrowclosed',
    width: 16,
    height: 16,
    color,
  };
}

/**
 * Extended edge type with metadata for highlighting
 */
export type EnhancedEdge = Edge & {
  data?: EdgeMetadata;
};

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
    markerEnd: arrowMarker(COLORS.external),
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
    markerEnd: arrowMarker(COLORS.secondary),
    data: {
      relationType: 'gateway-to-resource' as const,
      isHighlightable: false, // Resources not in agent highlight path
    },
  }));
}

/**
 * Create edges from linked LLM clients TO gateway
 *
 * Clients connect to the gateway via SSE or HTTP (northbound).
 */
export function createClientToGatewayEdges(
  clients: ClientStatus[]
): EnhancedEdge[] {
  return clients
    .filter((c) => c.linked)
    .map((client) => ({
      id: `edge-client-gateway-${client.slug}`,
      source: `client-${client.slug}`,
      target: GATEWAY_NODE_ID,
      data: {
        relationType: 'client-to-gateway' as const,
        isHighlightable: true,
      },
    }));
}

/**
 * Create edges from gateway to skill group nodes (one per top-level directory)
 */
export function createGatewayToSkillGroupEdges(skills: AgentSkill[]): EnhancedEdge[] {
  const active = skills.filter((s) => s.state === 'active');
  const groupNames = new Set(active.map((s) => (s.dir ? s.dir.split('/')[0] : s.name)));

  return Array.from(groupNames).map((groupName) => ({
    id: `edge-gateway-skill-group-${groupName}`,
    source: GATEWAY_NODE_ID,
    target: `skill-group-${groupName}`,
    markerEnd: arrowMarker(COLORS.tertiary),
    data: {
      relationType: 'gateway-to-skill-group' as const,
      isHighlightable: false,
    },
  }));
}

/**
 * Create all edges for the butterfly layout
 *
 * Combines all edge types:
 * - Client -> Gateway (linked LLM clients)
 * - Gateway -> MCP Servers
 * - Gateway -> Resources
 * - Gateway -> Skills
 */
export function createAllEdges(
  mcpServers: MCPServerStatus[],
  resources: ResourceStatus[],
  clients: ClientStatus[] = [],
): EnhancedEdge[] {
  return [
    ...createClientToGatewayEdges(clients),
    ...createGatewayToServerEdges(mcpServers),
    ...createGatewayToResourceEdges(resources),
  ];
}
