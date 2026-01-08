import type { ComponentType } from 'react';
import CustomNode from './CustomNode';
import GatewayNode from './GatewayNode';
import AgentNode from './AgentNode';

// Use 'any' to bypass React Flow's strict typing
// The components receive props correctly at runtime
export const nodeTypes: Record<string, ComponentType<any>> = {
  mcpServer: CustomNode,
  resource: CustomNode,
  gateway: GatewayNode,
  agent: AgentNode,
};
