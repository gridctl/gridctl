import type { Node, Edge } from '@xyflow/react';

/**
 * Input for layout engines - raw nodes and edges before positioning
 */
export interface LayoutInput {
  nodes: Node[];
  edges: Edge[];
}

/**
 * Output from layout engines - positioned nodes with same edges
 */
export interface LayoutOutput {
  nodes: Node[];
  edges: Edge[];
}

/**
 * Options for layout computation
 */
export interface LayoutOptions {
  /** Map of node IDs to preserved positions (for user-dragged nodes) */
  preservedPositions?: Map<string, { x: number; y: number }>;
}

/**
 * Layout engine interface - standardizes layout algorithm implementations
 *
 * All layout engines receive nodes and edges, apply positioning logic,
 * and return nodes with calculated positions.
 */
export interface LayoutEngine {
  /** Engine identifier */
  readonly name: string;

  /**
   * Apply layout to nodes and edges
   * @param input - Nodes and edges to layout
   * @param options - Optional configuration including preserved positions
   * @returns Nodes with calculated positions and unchanged edges
   */
  layout(input: LayoutInput, options?: LayoutOptions): LayoutOutput;
}

/**
 * Butterfly layout zone definitions
 *
 * Zone 4 (Far Left):  Clients - linked LLM clients
 * Zone 0 (Left):      Agents - consumers/drivers that initiate requests
 * Zone 1 (Center):    Gateway - central hub/router
 * Zone 2 (Right):     MCP Servers & A2A Agents - providers/tools
 * Zone 3 (Far Right): Resources - infrastructure/databases
 */
export const ButterflyZone = {
  CLIENTS: 4,
  AGENTS: 0,
  GATEWAY: 1,
  SERVERS: 2,
  RESOURCES: 3,
} as const;

export type ButterflyZone = (typeof ButterflyZone)[keyof typeof ButterflyZone];

/**
 * Configuration for a butterfly layout zone
 */
export interface ZoneConfig {
  /** Zone identifier */
  zone: ButterflyZone;
  /** Base X position for nodes in this zone */
  baseX: number;
  /** Vertical spacing between nodes in this zone */
  nodeSpacing: number;
}

/**
 * Edge relationship types for path highlighting
 */
export type EdgeRelationType =
  | 'client-to-gateway'    // LLM client connects to gateway
  | 'agent-to-gateway'     // Agent initiates connection to gateway
  | 'gateway-to-server'    // Gateway exposes MCP server
  | 'gateway-to-resource'  // Gateway manages resource
  | 'gateway-to-registry'  // Gateway connects to registry
  | 'agent-uses-server'    // Agent uses specific MCP server (via uses field)
  | 'agent-uses-agent';    // Agent delegates to another agent (A2A)

/**
 * Metadata attached to edges for highlighting and path tracing
 */
export interface EdgeMetadata {
  /** Type of relationship this edge represents */
  relationType: EdgeRelationType;
  /** Source agent name (for agent-originated edges) */
  sourceAgent?: string;
  /** Whether this edge can be part of a highlight path */
  isHighlightable: boolean;
}
