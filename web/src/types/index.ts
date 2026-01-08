// Transport type matching backend pkg/mcp/types.go
export type Transport = 'http' | 'stdio';

// Server info matching api.ServerInfo
export interface ServerInfo {
  name: string;
  version: string;
}

// MCP Server status matching mcp.MCPServerStatus
export interface MCPServerStatus {
  name: string;
  transport: Transport;
  endpoint?: string;
  containerId?: string;
  initialized: boolean;
  toolCount: number;
  tools: string[];
}

// Resource status for non-MCP containers
export interface ResourceStatus {
  name: string;
  image: string;
  status: 'running' | 'stopped' | 'error';
  network?: string;
}

// Agent status for active agent containers
export interface AgentStatus {
  name: string;
  image: string;
  status: 'running' | 'stopped' | 'error';
  containerId?: string;
}

// A2A Agent status matching a2a.A2AAgentStatus
export interface A2AAgentStatus {
  name: string;
  role: 'local' | 'remote';
  url?: string;
  endpoint?: string;
  available: boolean;
  skillCount: number;
  skills: string[];
  description?: string;
}

// Gateway status response from GET /api/status
export interface GatewayStatus {
  gateway: ServerInfo;
  'mcp-servers': MCPServerStatus[];
  agents?: AgentStatus[];
  resources?: ResourceStatus[];
  'a2a-agents'?: A2AAgentStatus[];
}

// Tool definition matching mcp.Tool
export interface Tool {
  name: string;
  title?: string;
  description?: string;
  inputSchema: InputSchema;
}

export interface InputSchema {
  type: string;
  properties?: Record<string, Property>;
  required?: string[];
}

export interface Property {
  type: string;
  description?: string;
  enum?: string[];
  default?: unknown;
}

// Tools list response from GET /api/tools
export interface ToolsListResult {
  tools: Tool[];
  nextCursor?: string;
}

// Node status for UI display
export type NodeStatus = 'running' | 'stopped' | 'error' | 'initializing';

// Base type for React Flow compatibility (requires index signature)
interface NodeDataBase {
  [key: string]: unknown;
}

// React Flow node data types
export interface GatewayNodeData extends NodeDataBase {
  type: 'gateway';
  name: string;
  version: string;
  serverCount: number;
  resourceCount: number;
  agentCount: number;
  a2aAgentCount: number;
  totalToolCount: number;
}

export interface MCPServerNodeData extends NodeDataBase {
  type: 'mcp-server';
  name: string;
  transport: Transport;
  endpoint?: string;
  containerId?: string;
  initialized: boolean;
  toolCount: number;
  tools: string[];
  status: NodeStatus;
}

export interface ResourceNodeData extends NodeDataBase {
  type: 'resource';
  name: string;
  image: string;
  network?: string;
  status: NodeStatus;
}

export interface AgentNodeData extends NodeDataBase {
  type: 'agent';
  name: string;
  image: string;
  containerId?: string;
  status: NodeStatus;
}

export interface A2AAgentNodeData extends NodeDataBase {
  type: 'a2a-agent';
  name: string;
  role: 'local' | 'remote';
  url?: string;
  endpoint?: string;
  skillCount: number;
  skills: string[];
  description?: string;
  status: NodeStatus;
}

export type NodeData = GatewayNodeData | MCPServerNodeData | ResourceNodeData | AgentNodeData | A2AAgentNodeData;

// Connection status for real-time updates
export type ConnectionStatus = 'connected' | 'connecting' | 'disconnected' | 'error';
