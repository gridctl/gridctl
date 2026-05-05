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

// Per-replica runtime status matching mcp.ReplicaStatus on the Go side.
// Present only when a server has a replica set; single-replica servers may
// still populate a single-element array.
export interface ReplicaStatus {
  replicaId: number;
  state: 'healthy' | 'unhealthy' | 'restarting' | string;
  healthy: boolean;
  inFlight: number;
  startedAt?: string; // RFC3339 timestamp
  lastCheck?: string;
  lastHealthy?: string;
  lastError?: string;
  restartAttempts?: number;
  nextRetryAt?: string;
  pid?: number;
  containerId?: string;
}

// Controller decision at the last autoscale tick.
export type AutoscaleDecisionKind = 'up' | 'down' | 'noop';

// Live autoscale snapshot matching mcp.AutoscaleStatus on the Go side.
// Present only when a server has autoscale configured.
export interface AutoscaleStatus {
  min: number;
  max: number;
  current: number;
  target: number;
  targetInFlight: number;
  medianInFlight: number;
  lastScaleUpAt?: string;   // RFC3339
  lastScaleDownAt?: string; // RFC3339
  lastDecision: AutoscaleDecisionKind;
  warmPool?: number;
  idleToZero?: boolean;
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
  // Tool whitelist from the stack YAML's tools: field. Empty/absent means "no
  // whitelist" (expose all tools the gateway loaded). Present and non-empty
  // means the operator has curated a subset.
  toolWhitelist?: string[];
  replicas?: ReplicaStatus[]; // Per-replica runtime status
  autoscale?: AutoscaleStatus; // Live autoscale snapshot (absent when not configured)
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
  stack_name?: string;     // Active stack name; omitted in stackless mode
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
export type NodeStatus = 'running' | 'stopped' | 'error' | 'initializing' | 'idle';

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
  replicaCount?: number; // Number of replicas (omitted or 1 = single-replica)
  // Tool whitelist from the stack YAML — present when the operator has
  // curated a subset of the server's tools. Drives the canvas "curated" badge.
  toolWhitelist?: string[];
  // Live autoscale snapshot — drives the ×current/target badge and decision ring
  // on the canvas node, and powers the Sidebar Scaling section.
  autoscale?: AutoscaleStatus;
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

// --- Telemetry Persistence Types (Phase 4) ---

// Three signal types persisted to disk. Lower-case wire shape matches the
// Go struct's YAML tags so request bodies round-trip without renaming.
export type TelemetrySignal = 'logs' | 'metrics' | 'traces';

// Stack-global persist defaults are plain bools (binary on/off). Per-server
// overrides use *bool semantics — see ServerPersistOverride below.
export interface TelemetryPersistDefaults {
  logs?: boolean;
  metrics?: boolean;
  traces?: boolean;
}

// One block per stack — per-signal retention is intentionally out of scope
// at MVP. Defaults filled by SetDefaults: 100MB / 5 backups / 7d.
export interface TelemetryRetention {
  max_size_mb?: number;
  max_backups?: number;
  max_age_days?: number;
}

export interface TelemetryConfig {
  persist?: TelemetryPersistDefaults;
  retention?: TelemetryRetention;
}

// Per-server overrides are tri-state in the YAML: absent (inherit),
// explicit true, explicit false. We keep null for "explicitly absent"
// after the parser drops the key, so the UI can distinguish a freshly
// cleared override from one that was never set.
export type OverrideValue = boolean | null;

export interface ServerPersistOverride {
  logs?: OverrideValue;
  metrics?: OverrideValue;
  traces?: OverrideValue;
}

export interface MCPServerTelemetryOverride {
  persist?: ServerPersistOverride;
}

// Inventory record from GET /api/telemetry/inventory. One entry per (server,
// signal) pair where at least one file exists. SizeBytes/FileCount aggregate
// the active jsonl plus rotated lumberjack siblings.
export interface InventoryRecord {
  server: string;
  signal: TelemetrySignal;
  path: string;
  sizeBytes: number;
  oldestTime: string; // RFC3339
  newestTime: string; // RFC3339
  fileCount: number;
}

// Standard envelope for PATCH/DELETE telemetry endpoints. The refreshed
// inventory snapshot lets callers update the store in-place without an
// extra round-trip.
export interface TelemetryMutationResponse {
  success: boolean;
  inventory: InventoryRecord[];
}

// Resolved view derived from the parsed stack YAML. global is the
// stack.telemetry block; servers maps server name → its (possibly empty)
// override block. retention is the stack-wide retention config.
export interface ResolvedTelemetry {
  global: TelemetryPersistDefaults;
  retention?: TelemetryRetention;
  servers: Record<string, ServerPersistOverride>;
}
