// Transport type matching backend pkg/mcp/types.go
export type Transport = 'http' | 'stdio' | 'sse';

// Tool selector matching pkg/config/types.go ToolSelector
// Supports agent-level tool filtering
export interface ToolSelector {
  server: string;
  tools?: string[]; // Empty/undefined implies all tools from server
}

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
  localProcess?: boolean; // True for local process servers
  ssh?: boolean; // True for SSH servers
  sshHost?: string; // SSH hostname
  healthy?: boolean; // Health check result (undefined if not yet checked)
  lastCheck?: string; // RFC3339 timestamp of last health check
  healthError?: string; // Error message if unhealthy
  openapi?: boolean; // True for OpenAPI-backed servers
  openapiSpec?: string; // OpenAPI spec URL or file path
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
  uses?: ToolSelector[];

  // A2A fields (populated when hasA2A is true)
  hasA2A: boolean;
  role?: 'local' | 'remote';
  url?: string;
  endpoint?: string;
  skillCount?: number;
  skills?: string[];
  description?: string;
}

// LLM client status from GET /api/clients
export interface ClientStatus {
  name: string;       // Human-readable name (e.g., "Claude Desktop")
  slug: string;       // CLI identifier (e.g., "claude")
  detected: boolean;  // Whether client is installed on the system
  linked: boolean;    // Whether gridctl entry exists in client config
  transport: string;  // "native SSE", "native HTTP", or "mcp-remote bridge"
  configPath?: string; // Config file path (only if detected)
}

// Gateway status response from GET /api/status
export interface GatewayStatus {
  gateway: ServerInfo;
  'mcp-servers': MCPServerStatus[];
  agents?: AgentStatus[];
  resources?: ResourceStatus[];
  sessions?: number;       // Active MCP session count
  a2a_tasks?: number;      // Active A2A task count (omitted if no A2A gateway)
  code_mode?: string;      // "on" when code mode is active (omitted when off)
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
  clientCount: number;
  totalToolCount: number;
  sessions: number;
  a2aTasks: number | null;
  codeMode: string | null;
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
  localProcess?: boolean; // True for local process servers
  ssh?: boolean; // True for SSH servers
  sshHost?: string; // SSH hostname
  healthy?: boolean; // Health check result
  lastCheck?: string; // RFC3339 timestamp of last health check
  healthError?: string; // Error message if unhealthy
  openapi?: boolean; // True for OpenAPI-backed servers
  openapiSpec?: string; // OpenAPI spec URL or file path
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
  uses?: ToolSelector[];

  // A2A fields (when hasA2A is true)
  hasA2A: boolean;
  role?: 'local' | 'remote';
  url?: string;
  endpoint?: string;
  skillCount?: number;
  skills?: string[];
  description?: string;
}

// Linked LLM client node data
export interface ClientNodeData extends NodeDataBase {
  type: 'client';
  name: string;
  slug: string;
  transport: string;
  configPath?: string;
  status: NodeStatus;
}

// --- Agent Skills Registry Types ---

export type ItemState = 'draft' | 'active' | 'disabled';

// AgentSkill represents a SKILL.md file following the agentskills.io spec
export interface AgentSkill {
  name: string;
  description: string;
  license?: string;
  compatibility?: string;
  metadata?: Record<string, string>;
  allowedTools?: string;
  state: ItemState;
  body: string;          // Markdown content (after frontmatter)
  fileCount: number;     // Supporting files count
}

// SkillFile represents a file within a skill directory
export interface SkillFile {
  path: string;          // Relative path (e.g., "scripts/lint.sh")
  size: number;          // File size in bytes
  isDir: boolean;        // True for directories
}

// Validation result from POST /api/registry/skills/validate
export interface SkillValidationResult {
  valid: boolean;
  errors: string[];
  warnings: string[];
  parsed?: AgentSkill;   // Parsed skill from content (when parseable)
}

export interface RegistryStatus {
  totalSkills: number;
  activeSkills: number;
}

export interface RegistryNodeData extends NodeDataBase {
  type: 'registry';
  name: string;
  totalSkills: number;
  activeSkills: number;
}

export type NodeData = GatewayNodeData | MCPServerNodeData | ResourceNodeData | AgentNodeData | ClientNodeData | RegistryNodeData;

// --- Workflow Types ---

export interface SkillInput {
  type: string;
  description?: string;
  required?: boolean;
  default?: unknown;
  enum?: string[];
}

export interface WorkflowStep {
  id: string;
  tool: string;
  args?: Record<string, unknown>;
  dependsOn?: string[];
  condition?: string;
  onError?: string;
  timeout?: string;
  retry?: { maxAttempts: number; backoff?: string };
}

export interface WorkflowOutput {
  format?: string;
  include?: string[];
  template?: string;
}

export interface WorkflowDefinition {
  name: string;
  inputs?: Record<string, SkillInput>;
  workflow: WorkflowStep[];
  output?: WorkflowOutput;
  dag: {
    levels: WorkflowStep[][];
  };
}

export interface StepExecutionResult {
  id: string;
  tool: string;
  status: 'success' | 'failed' | 'skipped' | 'running' | 'pending';
  startedAt?: string;
  durationMs?: number;
  error?: string;
  attempts?: number;
  skipReason?: string;
  level: number;
}

export interface ExecutionResult {
  skill: string;
  status: 'completed' | 'failed' | 'partial';
  startedAt: string;
  finishedAt: string;
  durationMs: number;
  steps: StepExecutionResult[];
  error?: string;
}

// Connection status for real-time updates
export type ConnectionStatus = 'connected' | 'connecting' | 'disconnected' | 'error';
