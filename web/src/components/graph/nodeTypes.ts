import type { ComponentType } from 'react';
import CustomNode from './CustomNode';
import GatewayNode from './GatewayNode';
import AgentNode from './AgentNode';

// React Flow requires a generic node type map but our custom nodes have specific data types.
// This type mismatch is a known limitation of React Flow's typing - the components work correctly at runtime.
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export const nodeTypes: Record<string, ComponentType<any>> = {
  mcpServer: CustomNode,
  resource: CustomNode,
  gateway: GatewayNode,
  agent: AgentNode,
};
