import type { ComponentType } from 'react';
import CustomNode from './CustomNode';
import GatewayNode from './GatewayNode';
import ClientNode from './ClientNode';
import SkillNode from './SkillNode';
import SkillGroupNode from './SkillGroupNode';
import ToolNode from './ToolNode';
import ToolOverflowNode from './ToolOverflowNode';

// Use 'any' to bypass React Flow's strict typing
// The components receive props correctly at runtime
export const nodeTypes: Record<string, ComponentType<any>> = {
  mcpServer: CustomNode,
  resource: CustomNode,
  gateway: GatewayNode,
  client: ClientNode,
  skill: SkillNode,
  skillGroup: SkillGroupNode,
  tool: ToolNode,
  toolOverflow: ToolOverflowNode,
};
