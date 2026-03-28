/**
 * Graph transformation orchestrator
 *
 * Converts backend data to React Flow nodes and edges,
 * then applies the selected layout engine.
 */

import type { Node, Edge } from '@xyflow/react';
import type { MCPServerStatus, ResourceStatus, ClientStatus, RegistryStatus, AgentSkill } from '../../types';
import type { LayoutEngine, LayoutOptions } from './types';
import { createAllNodes } from './nodes';
import { createAllEdges } from './edges';
import { ButterflyLayoutEngine } from './butterfly';

/**
 * Input for graph transformation
 */
export interface TransformInput {
  gatewayInfo: { name: string; version: string };
  mcpServers: MCPServerStatus[];
  resources: ResourceStatus[];
  sessions?: number;
  clients?: ClientStatus[];
  registryStatus?: RegistryStatus | null;
  codeMode?: string | null;
  skills?: AgentSkill[];
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
  /** Whether to use compact node dimensions */
  compact?: boolean;
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
  const { gatewayInfo, mcpServers, resources, sessions, clients = [], registryStatus, codeMode, skills = [] } = input;
  const { layoutEngine = defaultLayoutEngine, preservedPositions, compact } = options;

  // Create nodes
  const nodes = createAllNodes(
    gatewayInfo,
    mcpServers,
    resources,
    sessions,
    clients,
    registryStatus,
    codeMode,
    skills
  );

  // Create edges
  const edges = createAllEdges(mcpServers, resources, clients, skills);

  // Apply layout
  const layoutOptions: LayoutOptions = { preservedPositions, compact };
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
 * @param existingPositions - Optional map to preserve user-dragged positions
 */
export function transformToNodesAndEdges(
  gatewayInfo: { name: string; version: string },
  mcpServers: MCPServerStatus[],
  resources: ResourceStatus[] = [],
  existingPositions?: Map<string, { x: number; y: number }>,
  sessions?: number,
  clients?: ClientStatus[],
  registryStatus?: RegistryStatus | null,
  codeMode?: string | null,
  compact?: boolean,
  skills?: AgentSkill[]
): TransformOutput {
  return transformToGraph(
    { gatewayInfo, mcpServers, resources, sessions, clients, registryStatus, codeMode, skills },
    { preservedPositions: existingPositions, compact }
  );
}
