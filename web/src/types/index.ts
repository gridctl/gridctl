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
  // MCP protocol version the downstream server reported at initialize; absent
  // for lax servers that omit it and for OpenAPI adapters (no MCP handshake).
  protocolVersion?: string;
  // True for servers that never registered with the gateway (initialize
  // failure, unsupported protocol version, unreachable endpoint). Such
  // entries carry only name/healthy/healthError.
  registrationFailed?: boolean;
  openapi?: boolean; // True for OpenAPI-backed servers
  openapiSpec?: string; // OpenAPI spec URL or file path
  outputFormat?: string; // Configured output format (e.g. "toon", "csv")
  // Tool whitelist from the stack YAML's tools: field. Empty/absent means "no
  // whitelist" (expose all tools the gateway loaded). Present and non-empty
  // means the operator has curated a subset.
  toolWhitelist?: string[];
  // Pricing model DECLARED on this server in stack.yaml (model: field only;
  // a gateway default_model is not folded in). Absent when the server
  // inherits the default or has no attribution.
  model?: string;
  // Which model actually priced this server's recorded cost, with provenance.
  // Read-only; absent when the server has no observed traffic.
  effectiveModel?: EffectiveModel;
  replicas?: ReplicaStatus[]; // Per-replica runtime status
  autoscale?: AutoscaleStatus; // Live autoscale snapshot (absent when not configured)
  // Downstream OAuth authorization state for external servers with an
  // auth: {type: oauth} block: "authorized" or "needs_auth". Absent for
  // servers without tracked auth state. A needs_auth server is actionable
  // (authorize it), not failed.
  authStatus?: 'authorized' | 'needs_auth';
  authIssuer?: string; // Authorization server issuer, when known
  authExpiry?: string; // RFC3339 access token expiry, when known
}

// Per-server downstream authorization detail from GET /api/auth/servers.
export interface ServerAuthInfo {
  server: string;
  resource: string;
  status: 'authorized' | 'needs_auth';
  issuer?: string;
  scopes?: string[];
  expiry?: string; // RFC3339
}

// Response of POST /api/servers/{name}/auth/login.
export interface ServerAuthLogin {
  authorize_url: string;
  state: string;
}

// Resource status for non-MCP containers
export interface ResourceStatus {
  name: string;
  image: string;
  status: 'running' | 'stopped' | 'error';
  network?: string;
}

// Backend-computed per-client access scope (from the stack.yaml `clients:` block).
// Mirrors mcp.ClientScopeResult. When no clients: block is configured,
// `configured` is false and `unscoped` is true.
export interface ClientScopeResult {
  configured: boolean;  // A clients: block exists in the stack
  unscoped: boolean;    // This client reaches the full tool surface
  servers: string[];    // Reachable server names
  tools: string[];      // Reachable prefixed tool names (server__tool)
}

// LLM client status from GET /api/clients
export interface ClientStatus {
  name: string;       // Human-readable name (e.g., "Claude Desktop")
  slug: string;       // CLI identifier (e.g., "claude")
  detected: boolean;  // Whether client is installed on the system
  linked: boolean;    // Whether gridctl entry exists in client config
  transport: string;  // "native SSE", "native HTTP", or "mcp-remote bridge"
  configPath?: string; // Config file path (only if detected)
  model?: string;     // Declared pricing model from client_models (pricing only, not access)
  // Which model actually priced this client's recorded cost, with provenance.
  // Read-only; absent when the client has no observed traffic.
  effectiveModel?: EffectiveModel;
  effectiveScope?: ClientScopeResult; // Per-client access scope (when scoping is configured)
  // Desired state from the stack's link: block, distinct from linked
  // (actual config-file state). linkEntry carries the declared options.
  declared?: boolean;
  linkEntry?: { group?: string; clientId?: string; name?: string };
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
  per_replica?: Record<string, Record<string, TokenCounts>>;
  // per_client groups token usage by the originating MCP client. omitempty
  // on the wire; pre-attribution responses won't include it.
  per_client?: Record<string, TokenCounts>;
  format_savings: FormatSavings;
}

// Per-dimension USD cost snapshot mirroring TokenCounts
export interface CostCounts {
  input_usd: number;
  output_usd: number;
  cache_read_usd?: number;
  cache_write_usd?: number;
  total_usd: number;
}

// Cost usage summary from GET /api/status (cost field)
export interface CostUsage {
  session: CostCounts;
  per_server: Record<string, CostCounts>;
  per_replica?: Record<string, Record<string, CostCounts>>;
  per_client?: Record<string, CostCounts>;
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

// Single bucket of cost-over-time data (USD per minute-aligned bucket)
export interface CostDataPoint {
  timestamp: string;
  usd: number;
}

// Response from GET /api/metrics/cost
export interface CostMetricsResponse {
  range: string;
  interval: string;
  data_points: CostDataPoint[];
  per_server: Record<string, CostDataPoint[]>;
  per_client?: Record<string, CostDataPoint[]>;
}

// Severity classifies optimize findings. Mirrors pkg/optimize.Severity
// on the Go side; "info" findings are advisory and never trigger a
// non-zero CLI exit.
export type OptimizeSeverity = 'info' | 'warn' | 'critical';

// Single recommendation produced by pkg/optimize.Analyze.
export interface OptimizeFinding {
  id: string;
  heuristic: string;
  severity: OptimizeSeverity;
  title: string;
  summary: string;
  server?: string;
  tool?: string;
  impact_usd_per_week: number;
  remediation: string;
  detected_at: string;
}

// Response from GET /api/optimize.
export interface OptimizeReport {
  findings: OptimizeFinding[];
  health_score: number;
  generated_at: string;
}

// Model provenance for an effective-model attribution. Mirrors the
// pkg/optimize / internal/api vocabulary. `declared` = one model priced all
// recorded cost; `mixed` = multiple models priced the traffic; `none` =
// traffic observed but nothing priced (cost $0). The set is open so a future
// `reported` (a wire-level model signal) can extend it.
export type ModelProvenance = 'declared' | 'mixed' | 'none';

// One model's slice of an entity's recorded cost.
export interface ModelShare {
  model: string;
  cost_usd: number;
  share: number; // 0–1 of the entity's total recorded cost
}

// Which model(s) priced an entity's traffic, with provenance. Describes which
// declaration gridctl applied when pricing — NOT what the upstream client ran.
export interface EffectiveModel {
  model?: string;             // dominant model (omitted for `none`)
  provenance: ModelProvenance;
  share?: number;             // dominant model's cost share (omitted for `none`)
  models?: ModelShare[];      // full breakdown, descending by cost
}

// Gateway status response from GET /api/status
export interface GatewayStatus {
  gateway: ServerInfo;
  'mcp-servers': MCPServerStatus[];
  resources?: ResourceStatus[];
  sessions?: number;       // Active MCP session count
  code_mode?: string;      // "on" when code mode is active (omitted when off)
  token_usage?: TokenUsage; // Token usage metrics (omitted if no accumulator)
  cost?: CostUsage;        // Cost snapshot (omitted until any cost is recorded)
  cost_attribution?: boolean; // True when any client or server has a pricing model configured
  client_models?: Record<string, string>; // Declared client -> model pricing map (client_models)
  server_models?: Record<string, string>; // EFFECTIVE server -> model map (model: with default_model folded in)
  default_model?: string;  // Gateway-level default_model (omitted when not configured)
  // Effective model + provenance per client / server, derived read-only from
  // observed cost. Omitted until traffic is observed.
  effective_client_models?: Record<string, EffectiveModel>;
  effective_server_models?: Record<string, EffectiveModel>;
  stack_name?: string;     // Active stack name; omitted in stackless mode
}

// Response from GET /api/pricing/models
export interface PricingModelsResponse {
  source: string;   // Active pricing source name (e.g. "litellm")
  models: string[]; // Sorted canonical model IDs the source can price
}

// Response from PUT /api/clients/{slug}/model
export interface UpdateClientModelResponse {
  client: string;
  profileKey: string;
  model: string;
  reloaded: boolean;
  reloadedAt?: string;
}

// Response from PUT /api/mcp-servers/{name}/model
export interface UpdateServerModelResponse {
  server: string;
  model: string;
  reloaded: boolean;
  reloadedAt?: string;
}

// Response from PUT /api/gateway/default-model
export interface UpdateDefaultModelResponse {
  model: string;
  reloaded: boolean;
  reloadedAt?: string;
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

// One tool's observed usage from GET /api/tools/usage.
export interface ToolUsageStat {
  calls: number;
  // RFC3339; absent when the tool has a count but no recorded timestamp,
  // or (cross-referenced from the status list) has never been called.
  lastCalledAt?: string;
  // Cumulative tokens of the tool's own calls; absent (zero) on responses
  // from gateways predating per-tool cost attribution.
  inputTokens?: number;
  outputTokens?: number;
  // Cumulative estimated cost of the tool's priced calls. Absent when no
  // call was priced — the em-dash rule, never $0.
  costUsd?: number;
}

// GET /api/tools/usage — per-(server, tool) call counts + last-called times,
// keyed by server name then unprefixed tool name. Powers Tools Audit Mode.
// observedSince is when this gateway process began recording; with metrics
// persistence enabled, restored counts may predate it, so tools missing from
// `servers` mean "no recorded calls" — not a guaranteed longer disuse window.
export interface ToolUsageResponse {
  observedSince?: string;
  servers: Record<string, Record<string, ToolUsageStat>>;
}

// One skill's observed usage from GET /api/skills/usage. lastCalledAt is
// explicitly null (not omitted) when a skill has a count but no recorded
// timestamp, matching the endpoint's documented shape.
export interface SkillUsageStat {
  calls: number;
  lastCalledAt: string | null;
}

// GET /api/skills/usage: per-skill prompts/get call counts + last-called
// times, keyed by skill name. Joined to the registry list by name on the
// frontend (the registry payload is unchanged). observedSince is when this
// gateway process began recording; with metrics persistence enabled the
// restored counts may predate it, so a skill missing from `skills` means
// "no recorded calls", not a guaranteed longer disuse window.
export interface SkillUsageResponse {
  observedSince: string | null;
  skills: Record<string, SkillUsageStat>;
}

// Node status for UI display
export type NodeStatus = 'running' | 'stopped' | 'error' | 'initializing' | 'idle' | 'needs-auth';

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
  protocolVersion?: string; // MCP protocol version reported at initialize
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
  // Downstream OAuth authorization state; drives the needs-auth node state,
  // the canvas key indicator, and the Sidebar Authorization section.
  authStatus?: 'authorized' | 'needs_auth';
  authIssuer?: string;
  authExpiry?: string;
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
  effectiveScope?: ClientScopeResult; // Per-client access scope (mirrors ClientStatus.effectiveScope)
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

// Tool fan-out node data — one node per visible tool of an expanded server.
export interface ToolNodeData extends NodeDataBase {
  type: 'tool';
  name: string;          // Unprefixed tool name (e.g. "search-repos")
  serverName: string;    // Owning MCP server name
  serverNodeId: string;  // Parent server node id (e.g. "mcp-github")
}

// Aggregate "+N more" node shown when an expanded server exceeds the fan-out
// cap. Carries the hidden tool names so the node can list them in a popover
// rather than mounting more canvas nodes.
export interface ToolOverflowNodeData extends NodeDataBase {
  type: 'tool-overflow';
  serverName: string;
  serverNodeId: string;
  overflowCount: number; // Number of tools beyond the cap (the N in "+N more")
  hiddenTools: string[]; // The unprefixed names of the capped-out tools
}

export type NodeData = GatewayNodeData | MCPServerNodeData | ResourceNodeData | ClientNodeData | SkillNodeData | SkillGroupNodeData | ToolNodeData | ToolOverflowNodeData;

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
  /** True when the on-disk SKILL.md diverges from the last installed snapshot. */
  hasLocalEdits?: boolean;
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
  /** Names of skills in this source with local edits a sync would overwrite. */
  driftedSkills?: string[];
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

/** A SKILL.md that was found in the repository but could not be parsed. */
export interface MalformedSkillFile {
  path: string;
  error: string;
}

export interface SkillPreviewResponse {
  repo: string;
  ref: string;
  commitSha: string;
  skills: SkillPreview[];
  malformed?: MalformedSkillFile[];
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

export interface SkillSyncResult {
  skill: string;
  imported?: number;
  warnings?: string[];
  error?: string;
  /** Reason a drifted skill was left untouched (e.g. "local edits"). */
  skipped?: string;
  /** File name of the pre-overwrite backup written when force-overwritten. */
  backup?: string;
}

/** Local vs upstream comparison for a single tracked skill (no writes). */
export interface SkillDiffResponse {
  skill: string;
  local: string;
  upstream: string;
  unifiedDiff?: string;
  drifted: boolean;
}

export interface SourceSyncResult {
  name: string;
  repo: string;
  pinned?: boolean;
  skills?: SkillSyncResult[];
  error?: string;
}

export interface SourceSyncSummary {
  sources: SourceSyncResult[];
  syncedSources: number;
  updatedSkills: number;
  skippedSkills?: number;
  failedSources: number;
  pinnedSources: number;
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
