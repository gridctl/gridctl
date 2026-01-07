import type { Node, Edge } from '@xyflow/react';
import type {
  MCPServerStatus,
  ResourceStatus,
  NodeStatus,
} from '../types';
import { LAYOUT, NODE_TYPES } from './constants';

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
 * Calculate node positions in a radial layout around the gateway
 */
function calculateRadialPosition(
  index: number,
  total: number,
  centerX: number,
  centerY: number,
  radius: number,
  startAngle = -Math.PI / 2 // Start from top
): { x: number; y: number } {
  const angleStep = (2 * Math.PI) / Math.max(total, 1);
  const angle = startAngle + index * angleStep;

  return {
    x: centerX + radius * Math.cos(angle) - LAYOUT.NODE_WIDTH / 2,
    y: centerY + radius * Math.sin(angle) - LAYOUT.NODE_HEIGHT / 2,
  };
}

/**
 * Transform backend data to React Flow nodes and edges
 * @param existingPositions - Optional map of node IDs to positions to preserve user-dragged positions
 */
export function transformToNodesAndEdges(
  gatewayInfo: { name: string; version: string },
  mcpServers: MCPServerStatus[],
  resources: ResourceStatus[] = [],
  existingPositions?: Map<string, { x: number; y: number }>
): { nodes: Node[]; edges: Edge[] } {
  const nodes: Node[] = [];
  const edges: Edge[] = [];

  // Calculate total tool count
  const totalToolCount = mcpServers.reduce((sum, s) => sum + s.toolCount, 0);

  // Default gateway position
  const defaultGatewayPosition = { x: LAYOUT.CENTER_X - 112, y: LAYOUT.CENTER_Y - 80 };

  // Create gateway node at center
  const gatewayNode: Node = {
    id: GATEWAY_NODE_ID,
    type: NODE_TYPES.GATEWAY,
    position: existingPositions?.get(GATEWAY_NODE_ID) ?? defaultGatewayPosition,
    data: {
      type: 'gateway',
      name: gatewayInfo.name,
      version: gatewayInfo.version,
      serverCount: mcpServers.length,
      resourceCount: resources.length,
      totalToolCount,
    },
    draggable: true,
  };
  nodes.push(gatewayNode);

  // Create MCP server nodes in radial layout (left side)
  mcpServers.forEach((server, index) => {
    const nodeId = `mcp-${server.name}`;
    const defaultPosition = calculateRadialPosition(
      index,
      mcpServers.length,
      LAYOUT.CENTER_X,
      LAYOUT.CENTER_Y,
      LAYOUT.MCP_RADIUS,
      0 // Start from right
    );

    const serverNode: Node = {
      id: nodeId,
      type: NODE_TYPES.MCP_SERVER,
      position: existingPositions?.get(nodeId) ?? defaultPosition,
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
      },
      draggable: true,
    };
    nodes.push(serverNode);

    // Create edge from gateway to server
    // Note: type is not set here - it's controlled by defaultEdgeOptions in Canvas
    edges.push({
      id: `edge-gateway-${server.name}`,
      source: GATEWAY_NODE_ID,
      target: nodeId,
      animated: server.initialized,
    });
  });

  // Create resource nodes (right side)
  resources.forEach((resource, index) => {
    const nodeId = `resource-${resource.name}`;
    const defaultPosition = calculateRadialPosition(
      index,
      resources.length,
      LAYOUT.CENTER_X,
      LAYOUT.CENTER_Y,
      LAYOUT.RESOURCE_RADIUS,
      0 // Start from right
    );

    const resourceNode: Node = {
      id: nodeId,
      type: NODE_TYPES.RESOURCE,
      position: existingPositions?.get(nodeId) ?? defaultPosition,
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

    // Create edge from gateway to resource
    // Note: type is not set here - it's controlled by defaultEdgeOptions in Canvas
    edges.push({
      id: `edge-gateway-${resource.name}`,
      source: GATEWAY_NODE_ID,
      target: nodeId,
      animated: resource.status === 'running',
    });
  });

  return { nodes, edges };
}

/**
 * Parse prefixed tool name into agent and tool names
 * Matches the format from pkg/mcp/router.go: "agent--tool"
 */
export function parsePrefixedToolName(prefixed: string): {
  agentName: string;
  toolName: string;
} {
  const parts = prefixed.split('--');
  if (parts.length !== 2) {
    return { agentName: '', toolName: prefixed };
  }
  return { agentName: parts[0], toolName: parts[1] };
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
