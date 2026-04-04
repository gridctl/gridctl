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
  tokenizer?: string; // active tokenizer mode: "embedded" or "api"
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
  outputFormat?: string; // Configured output format (e.g. "toon", "csv")
}

// Resource status for non-MCP containers
export interface ResourceStatus {
  name: string;
  image: string;
  status: 'running' | 'stopped' | 'error';
  network?: string;
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

// Token counts for a session or server
export interface TokenCounts {
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
}

// Format savings from output formatting (e.g., TOON/CSV)
export interface FormatSavings {
  original_tokens: number;
  formatted_tokens: number;
  saved_tokens: number;
  savings_percent: number;
}

// Token usage summary from GET /api/status
export interface TokenUsage {
  session: TokenCounts;
  per_server: Record<string, TokenCounts>;
  format_savings: FormatSavings;
}

// Historical time-series data point
export interface TokenDataPoint {
  timestamp: string;
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
}

// Response from GET /api/metrics/tokens
export interface TokenMetricsResponse {
  range: string;
  interval: string;
  data_points: TokenDataPoint[];
  per_server: Record<string, TokenDataPoint[]>;
}

// Gateway status response from GET /api/status
export interface GatewayStatus {
  gateway: ServerInfo;
  'mcp-servers': MCPServerStatus[];
  resources?: ResourceStatus[];
  sessions?: number;       // Active MCP session count
  code_mode?: string;      // "on" when code mode is active (omitted when off)
  token_usage?: TokenUsage; // Token usage metrics (omitted if no accumulator)
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
  clientCount: number;
  totalToolCount: number;
  sessions: number;
  codeMode: string | null;
  totalSkills: number;
  activeSkills: number;
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
  outputFormat?: string; // Configured output format (e.g. "toon", "csv")
  isProcessing?: boolean; // Playground: true when this server has an active tool call
  pinStatus?: 'pinned' | 'drift' | 'blocked' | 'approved_pending_redeploy';
  pinDriftCount?: number;
}

export interface ResourceNodeData extends NodeDataBase {
  type: 'resource';
  name: string;
  image: string;
  network?: string;
  status: NodeStatus;
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
  acceptanceCriteria?: string[]; // Given/When/Then scenarios (gridctl extension)
  state: ItemState;
  body: string;          // Markdown content (after frontmatter)
  fileCount: number;     // Supporting files count
  dir?: string;          // Relative path from skills/ root (e.g., "git-workflow/branch-fork")
}

// CriterionResult holds the outcome of a single acceptance criterion test
export interface CriterionResult {
  criterion: string;
  passed: boolean;
  actual?: string;       // Set when failed
  skipped?: boolean;
  skipReason?: string;   // Set when skipped
}

// SkillTestResult holds the full result of running a skill's acceptance criteria
export interface SkillTestResult {
  skill: string;
  passed: number;
  failed: number;
  skipped?: number;
  results: CriterionResult[];
  status?: 'untested'; // Set when no test has been run yet
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

// Skill canvas node data
export type SkillTestStatus = 'passed' | 'failing' | 'untested';

export interface SkillNodeData extends NodeDataBase {
  type: 'skill';
  name: string;
  description: string;
  state: ItemState;
  testStatus: SkillTestStatus;
  criteriaCount: number;
}

export interface SkillGroupNodeData extends NodeDataBase {
  type: 'skill-group';
  groupName: string;
  totalSkills: number;
  activeSkills: number;
  failingSkills: number;
  untestedSkills: number;
}

export type NodeData = GatewayNodeData | MCPServerNodeData | ResourceNodeData | ClientNodeData | SkillNodeData | SkillGroupNodeData;

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

// --- Spec Types (from Phase 1 backend) ---

export type IssueSeverity = 'error' | 'warning' | 'info';

export interface ValidationIssue {
  field: string;
  message: string;
  severity: IssueSeverity;
}

export interface ValidationResult {
  valid: boolean;
  errorCount: number;
  warningCount: number;
  issues: ValidationIssue[];
}

export type DiffAction = 'add' | 'remove' | 'change';

export interface DiffItem {
  action: DiffAction;
  kind: string;
  name: string;
  details?: string[];
}

export interface PlanDiff {
  hasChanges: boolean;
  items: DiffItem[];
  summary: string;
}

export interface ValidationStatus {
  status: 'valid' | 'warnings' | 'errors' | 'unknown';
  errorCount: number;
  warningCount: number;
}

export interface DriftStatus {
  status: 'in-sync' | 'drifted' | 'unknown';
  added?: string[];
  removed?: string[];
  changed?: string[];
}

export interface DependencyStatus {
  status: 'resolved' | 'missing';
  missing?: string[];
}

export interface SpecHealth {
  validation: ValidationStatus;
  drift: DriftStatus;
  dependencies: DependencyStatus;
}

export interface StackSpec {
  path: string;
  content: string;
}

// --- Skill Source Types (from Phase 7 backend) ---

export interface SecurityFinding {
  stepId: string;
  pattern: string;
  description: string;
  severity: 'warning' | 'danger';
}

export interface SkillSourceEntry {
  name: string;
  description: string;
  state: string;
  isRemote: boolean;
  contentHash?: string;
}

export interface SkillSourceStatus {
  name: string;
  repo: string;
  ref?: string;
  path?: string;
  autoUpdate: boolean;
  updateInterval: string;
  skills: SkillSourceEntry[];
  lastFetched?: string;
  commitSha?: string;
  updateAvailable: boolean;
}

export interface SkillPreview {
  name: string;
  description: string;
  body: string;
  valid: boolean;
  errors?: string[];
  warnings?: string[];
  findings?: SecurityFinding[];
  exists: boolean;
}

export interface SkillPreviewResponse {
  repo: string;
  ref: string;
  commitSha: string;
  skills: SkillPreview[];
}

export interface ImportedSkillResult {
  name: string;
  path: string;
}

export interface SkippedSkillResult {
  name: string;
  reason: string;
}

export interface ImportResult {
  imported?: ImportedSkillResult[];
  skipped?: SkippedSkillResult[];
  warnings?: string[];
}

export interface SourceUpdateCheck {
  source: string;
  currentSha: string;
  latestSha: string;
  hasUpdate: boolean;
}

export interface SourceUpdateSummary {
  name: string;
  repo: string;
  currentSha: string;
  latestSha?: string;
  hasUpdate: boolean;
  error?: string;
}

export interface UpdateSummary {
  available: number;
  sources: SourceUpdateSummary[];
}
