// Transport type matching backend pkg/mcp/types.go
export type Transport = 'http' | 'stdio' | 'sse';

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
  external?: boolean; // True for external URL servers
}

// Resource status for non-MCP containers
export interface ResourceStatus {
  name: string;
  image: string;
  status: 'running' | 'stopped' | 'error';
  network?: string;
}

// Agent variant discriminator
export type AgentVariant = 'local' | 'remote';

// Unified agent status - merges container and A2A protocol state
export interface AgentStatus {
  // Core identification
  name: string;
  status: 'running' | 'stopped' | 'error' | 'unavailable';

  // Variant: "local" (container-based) or "remote" (A2A only)
  variant: AgentVariant;

  // Container fields (populated for local/container-based agents)
  image?: string;
  containerId?: string;
  uses?: string[];

  // A2A fields (populated when hasA2A is true)
  hasA2A: boolean;
  role?: 'local' | 'remote';
  url?: string;
  endpoint?: string;
  skillCount?: number;
  skills?: string[];
  description?: string;
}

// Gateway status response from GET /api/status
export interface GatewayStatus {
  gateway: ServerInfo;
  'mcp-servers': MCPServerStatus[];
  agents?: AgentStatus[];
  resources?: ResourceStatus[];
}

// Tool definition matching mcp.Tool
export interface Tool {
  name: string;
  title?: string;
  description?: string;
  // InputSchema is now a raw JSON object to preserve full JSON Schema
  // from MCP servers without loss (supports JSON Schema draft 2020-12)
  inputSchema: Record<string, unknown>;
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
  external?: boolean; // True for external URL servers
}

export interface ResourceNodeData extends NodeDataBase {
  type: 'resource';
  name: string;
  image: string;
  network?: string;
  status: NodeStatus;
}

// Unified agent node data - handles both local (container) and remote (A2A) agents
export interface AgentNodeData extends NodeDataBase {
  type: 'agent';
  name: string;
  status: NodeStatus;

  // Variant determines primary behavior and visual style
  variant: AgentVariant;

  // Container fields (local variant only)
  image?: string;
  containerId?: string;
  uses?: string[];

  // A2A fields (when hasA2A is true)
  hasA2A: boolean;
  role?: 'local' | 'remote';
  url?: string;
  endpoint?: string;
  skillCount?: number;
  skills?: string[];
  description?: string;
}

export type NodeData = GatewayNodeData | MCPServerNodeData | ResourceNodeData | AgentNodeData;

// Connection status for real-time updates
export type ConnectionStatus = 'connected' | 'connecting' | 'disconnected' | 'error';
