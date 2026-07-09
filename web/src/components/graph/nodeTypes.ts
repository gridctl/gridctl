import type { NodeTypes } from '@xyflow/react';
import CustomNode from './CustomNode';
import GatewayNode from './GatewayNode';
import ClientNode from './ClientNode';
import SkillNode from './SkillNode';
import SkillGroupNode from './SkillGroupNode';
import ToolNode from './ToolNode';
import ToolOverflowNode from './ToolOverflowNode';

// NodeTypes erases each component's concrete node-data generic, which is why
// the components can be registered here without casting.
export const nodeTypes: NodeTypes = {
  mcpServer: CustomNode,
  resource: CustomNode,
  gateway: GatewayNode,
  client: ClientNode,
  skill: SkillNode,
  skillGroup: SkillGroupNode,
  tool: ToolNode,
  toolOverflow: ToolOverflowNode,
};
