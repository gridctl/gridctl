/**
 * Graph module - Layout engines and transformation
 *
 * This module provides:
 * - Layout engine interface and implementations (Dagre, Butterfly)
 * - Node and edge creation functions
 * - Graph transformation from backend data to React Flow
 */

// Types
export type {
  LayoutEngine,
  LayoutInput,
  LayoutOutput,
  LayoutOptions,
  ButterflyZone,
  ZoneConfig,
  EdgeRelationType,
  EdgeMetadata,
} from './types';

// Layout engines
export { DagreLayoutEngine } from './dagre';
export type { DagreLayoutOptions, LayoutDirection } from './dagre';

export { ButterflyLayoutEngine, createButterflyEngine } from './butterfly';
export type { ButterflyLayoutOptions } from './butterfly';

// Utilities
export { getNodeDimensions } from './utils';

// Node creation
export {
  createGatewayNode,
  createMCPServerNodes,
  createAgentNodes,
  createResourceNodes,
  createClientNodes,
  createRegistryNode,
  createAllNodes,
} from './nodes';

// Edge creation
export {
  GATEWAY_NODE_ID,
  createClientToGatewayEdges,
  createAgentToGatewayEdges,
  createGatewayToServerEdges,
  createGatewayToResourceEdges,
  createGatewayToRegistryEdge,
  createAgentUsesEdges,
  createAllEdges,
  buildNodeTypeSets,
} from './edges';
export type { EnhancedEdge } from './edges';

// Transformation
export {
  transformToGraph,
  transformToNodesAndEdges,
} from './transform';
export type { TransformInput, TransformOutput, TransformOptions } from './transform';
