/**
 * Graph transformation orchestrator
 *
 * Converts backend data to React Flow nodes and edges,
 * then applies the selected layout engine.
 */

import type { Node, Edge } from '@xyflow/react';
import type { MCPServerStatus, ResourceStatus, AgentStatus, ClientStatus, RegistryStatus } from '../../types';
import type { LayoutEngine, LayoutOptions } from './types';
import { createAllNodes } from './nodes';
import { createAllEdges, buildNodeTypeSets } from './edges';
import { ButterflyLayoutEngine } from './butterfly';

/**
 * Input for graph transformation
 */
export interface TransformInput {
  gatewayInfo: { name: string; version: string };
  mcpServers: MCPServerStatus[];
  resources: ResourceStatus[];
  agents: AgentStatus[];
  sessions?: number;
  a2aTasks?: number | null;
  clients?: ClientStatus[];
  registryStatus?: RegistryStatus | null;
  codeMode?: string | null;
}

/**
 * Output from graph transformation
 */
export interface TransformOutput {
  nodes: Node[];
  edges: Edge[];
}

/**
 * Options for graph transformation
 */
export interface TransformOptions extends LayoutOptions {
  /** Layout engine to use (defaults to butterfly) */
  layoutEngine?: LayoutEngine;
}

/** Default layout engine - butterfly layout */
const defaultLayoutEngine = new ButterflyLayoutEngine();

/**
 * Transform backend data to React Flow graph with layout applied
 *
 * This is the main entry point for graph creation. It:
 * 1. Creates nodes from backend data
 * 2. Creates edges with proper direction for butterfly layout
 * 3. Applies the layout engine to position nodes
 *
 * @param input - Backend data (gateway, servers, resources, agents)
 * @param options - Transform options including layout engine and preserved positions
 * @returns Positioned nodes and edges for React Flow
 */
export function transformToGraph(
  input: TransformInput,
  options: TransformOptions = {}
): TransformOutput {
  const { gatewayInfo, mcpServers, resources, agents, sessions, a2aTasks, clients = [], registryStatus, codeMode } = input;
  const { layoutEngine = defaultLayoutEngine, preservedPositions } = options;

  // Build lookup sets for edge routing
  const { usedByOtherAgents } = buildNodeTypeSets(mcpServers, agents);

  // Create nodes
  const nodes = createAllNodes(
    gatewayInfo,
    mcpServers,
    resources,
    agents,
    usedByOtherAgents,
    sessions,
    a2aTasks,
    clients,
    registryStatus,
    codeMode
  );

  // Create edges
  const edges = createAllEdges(mcpServers, resources, agents, clients, registryStatus);

  // Apply layout
  const layoutOptions: LayoutOptions = { preservedPositions };
  const { nodes: layoutedNodes, edges: layoutedEdges } = layoutEngine.layout(
    { nodes, edges },
    layoutOptions
  );

  return { nodes: layoutedNodes, edges: layoutedEdges };
}

/**
 * Transform backend data using the default butterfly layout
 *
 * Convenience function that uses the default butterfly layout engine.
 * This is the standard transformation for the gridctl UI.
 *
 * @param gatewayInfo - Gateway name and version
 * @param mcpServers - MCP servers
 * @param resources - Resources
 * @param agents - Agents
 * @param existingPositions - Optional map to preserve user-dragged positions
 */
export function transformToNodesAndEdges(
  gatewayInfo: { name: string; version: string },
  mcpServers: MCPServerStatus[],
  resources: ResourceStatus[] = [],
  agents: AgentStatus[] = [],
  existingPositions?: Map<string, { x: number; y: number }>,
  sessions?: number,
  a2aTasks?: number | null,
  clients?: ClientStatus[],
  registryStatus?: RegistryStatus | null,
  codeMode?: string | null
): TransformOutput {
  return transformToGraph(
    { gatewayInfo, mcpServers, resources, agents, sessions, a2aTasks, clients, registryStatus, codeMode },
    { preservedPositions: existingPositions }
  );
}
