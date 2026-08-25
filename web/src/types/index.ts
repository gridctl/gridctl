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
  // Resolved MCP protocol generation ("handshake" or "stateless"); absent
  // for OpenAPI adapters (no MCP wire protocol) and unresolved servers.
  protocolGeneration?: string;
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
  transport: string;  // "native HTTP" or "mcp-remote bridge"
  configPath?: string; // Config file path (only if detected)
  effectiveScope?: ClientScopeResult; // Per-client access scope (when scoping is configured)
  // Desired state from the stack's link: block, distinct from linked
  // (actual config-file state). linkEntry carries the declared options.
  declared?: boolean;
  linkEntry?: { group?: string; clientId?: string; name?: string };
  // A recorded gridctl entry in this client's config was edited since
  // gridctl wrote it (wiring ownership drift).
  drifted?: boolean;
  // Client-specific post-link guidance from the backend provisioner;
  // absent for clients without caveats. Rendered verbatim, never
  // hardcoded per client in the frontend.
  notes?: string[];
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
  // Projected weekly token savings from applying the remediation. Schema
  // heuristics assume ~500 prompts/week; format_savings_shortfall
  // normalizes measured savings over the observation window. 0 or absent
  // means no provable impact.
  impact_tokens_per_week?: number;
  remediation: string;
  detected_at: string;
}

// Response from GET /api/optimize.
export interface OptimizeReport {
  findings: OptimizeFinding[];
  health_score: number;
  generated_at: string;
}

// One enabled experimental flag from /api/status feature_details.
// Read-only display metadata: flags are configured in stack.yaml (or via
// GRIDCTL_EXPERIMENTAL_* env vars) and cannot be toggled from the UI.
export interface FeatureDetail {
  name: string;        // snake_case flag key from the registry
  stage: string;       // lifecycle stage, e.g. "experimental"
  description: string; // one-line summary from the flag registry
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
  features?: Record<string, boolean>; // Enabled experimental flags, name -> true (omitted when none)
  feature_details?: FeatureDetail[];  // Display metadata for the same flags (omitted when none)
}

// MCP tool annotations, matching mcp.ToolAnnotations. All fields are
// optional hints; absent means "undeclared", which the spec treats as
// worst-case (potentially destructive, open-world). Server-reported and
// unverified; UI surfaces must present them as claims, not guarantees.
export interface ToolAnnotations {
  title?: string;
  readOnlyHint?: boolean;
  destructiveHint?: boolean;
  idempotentHint?: boolean;
  openWorldHint?: boolean;
}

// Tool definition matching mcp.Tool
export interface Tool {
  name: string;
  title?: string;
  description?: string;
  // InputSchema is now a raw JSON object to preserve full JSON Schema
  // from MCP servers without loss (supports JSON Schema draft 2020-12)
  inputSchema: Record<string, unknown>;
  annotations?: ToolAnnotations;
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
  // from gateways predating per-tool attribution.
  inputTokens?: number;
  outputTokens?: number;
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
  protocolGeneration?: string; // Resolved MCP protocol generation ("handshake" or "stateless")
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
  // Frontmatter keys gridctl does not model (e.g. argument-hint), preserved
  // verbatim by the backend. The editor carries them through saves untouched.
  extra?: Record<string, unknown>;
  state: ItemState;
  // Markdown content after the frontmatter. Optional because the list endpoint
  // (GET /api/registry/skills) omits it — the bodies dominated a payload that
  // is polled every few seconds. `undefined` means "not loaded", which is
  // distinct from `''` ("this skill has no instructions"); fetch the single
  // skill (or use useSkillBody) before rendering or saving it.
  body?: string;
  fileCount: number;     // Supporting files count
  dir?: string;          // Relative path from skills/ root (e.g., "git-workflow/branch-fork")
  // Pin/provenance/policy summary. Absent when the skill has neither a pin
  // record nor a policy verdict. Every field is optional on the wire
  // (omitempty), so zero counts and false flags simply do not appear.
  governance?: SkillGovernance;
  // Declared/resolved model preference view; absent when nothing declares
  // or resolves one.
  modelPreference?: ModelPreference;
}

// ModelPreference mirrors the backend's modelPreference object on registry
// skill and agent responses. Declared is the author's frontmatter
// declaration; resolved appears only when a loaded stack policy default or
// override decides the value (an author declaration a policy leaves
// untouched stays declared-only); honor maps projection target slugs to
// what each target does with the key. The whole object is absent when
// nothing is declared and no policy resolves — older backends simply never
// send it.
export interface ModelPreference {
  declared?: { value: string; sourceKey: string };
  resolved?: { value: string; resolution: 'default' | 'override' };
  honor?: Record<string, string>;
}

// SkillGovernance mirrors the backend's governance object on registry skill
// responses: factual provenance (never a trust judgment), pin state
// ("pin drift" in UI copy — a different fact from the Library's sync drift),
// advisory finding counts, and the skills-policy verdict with the matching
// rule. Severity vocabulary matches lib/severity.ts ('warn', not 'warning').
export interface SkillGovernance {
  source?: 'local' | 'git';
  origin?: { repo?: string; ref?: string; commitSha?: string };
  pinStatus?: 'pinned' | 'drift';
  findingsCount?: number;
  maxFindingSeverity?: 'info' | 'warn' | 'critical';
  policyDenied?: boolean;
  policyRule?: string;
}

// --- Agents (imported agent definitions) ---

// One passthrough frontmatter key on an agent, in document order. An ordered
// array rather than an object: key order is part of the verbatim-projection
// contract, and JSON objects do not guarantee it.
export interface AgentExtraField {
  key: string;
  value: unknown;
}

// RegistryAgent mirrors GET /api/registry/agents[/{name}]. The list endpoint
// omits body and raw; the single-agent GET (and ?full=1) includes them. Raw is
// the verbatim file and the editing surface — PUT sends it back whole.
export interface RegistryAgent {
  name: string;
  description: string;
  /** Imported source's name from the lock file; absent for unsourced agents. */
  source?: string;
  extra?: AgentExtraField[];
  dir?: string;
  body?: string;
  raw?: string;
  /** Declared/resolved model preference view; absent when nothing declares
   *  or resolves one. */
  modelPreference?: ModelPreference;
}

// Shared projection-state vocabulary from the pkg/project engine. The CLI and
// every REST surface speak these exact strings; the UI must not invent synonyms.
export type AgentProjectionState = 'in-sync' | 'stale' | 'drifted' | 'target-missing';

// One (agent, client) row from GET /api/project/agents/status.
export interface AgentProjectionStatus {
  agent: string;
  client: string;
  channel: string;
  target: string;
  /** identity = canonical bytes copied verbatim; lossy = client-dialect render. */
  render: 'identity' | 'lossy';
  /** Pack that applied this projection; absent outside packs. */
  pack?: string;
  state: AgentProjectionState;
  /** Lossy-render report (dropped frontmatter keys) or drift detail. */
  detail?: string;
  /** Model preference a stack policy rewrite wrote into the projected
   *  file; absent for pass-through projections (wire: model_value). */
  model_value?: string;
  synced_at?: string;
}

// One row from POST /api/project/agents/sync.
export interface AgentSyncResult {
  agent: string;
  client: string;
  channel?: string;
  target?: string;
  action: string;
  detail?: string;
  backup_path?: string;
  error?: string;
}

// One row from POST /api/project/agents/unsync.
export interface AgentUnsyncResult {
  agent: string;
  client: string;
  target: string;
  action: string;
  backup_path?: string;
}

// Response of POST /api/project/agents/adopt.
export interface AgentAdoptResult {
  agent: string;
  client: string;
  target: string;
  canonical_file: string;
  backup_file?: string;
  changed: boolean;
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

/** A previewed agent definition from a repo (not yet imported). */
export interface AgentPreview {
  name: string;
  description: string;
  body: string;
  valid: boolean;
  errors?: string[];
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
  agents?: AgentPreview[];
  malformedAgents?: MalformedSkillFile[];
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
  importedAgents?: ImportedSkillResult[];
  skippedAgents?: SkippedSkillResult[];
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

// One active MCP session as reported by /api/sessions entries. Sessions
// exist only on the handshake generation; the stateless generation is
// sessionless by design, so live stateless traffic never appears here.
export interface SessionEntry {
  id: string;
  generation: string;
  protocolVersion?: string;
  /** Client-supplied clientInfo from initialize. */
  clientName?: string;
  clientVersion?: string;
  /** Normalized identifier; string-matches provisioner client slugs. */
  accessId?: string;
}

// --- Wiring ownership (GET /api/project/wiring/status) ---

// The wiring ownership vocabulary: the engine's shared projection states
// plus the two wiring extensions. The full form of the fact ClientStatus
// collapses into its single `drifted` boolean.
export type WiringState =
  | 'in-sync'
  | 'stale'
  | 'drifted'
  | 'target-missing'
  | 'foreign'
  | 'missing';

// One (client, entry) ownership row from GET /api/project/wiring/status.
export interface WiringRow {
  client: string;
  name: string;
  channel: string;
  pack?: string;
  target?: string;
  state: WiringState;
  detail?: string;
  remediation?: string;
  synced_at?: string;
}

// Response of POST /api/project/wiring/adopt.
export interface WiringAdoptResult {
  client: string;
  name: string;
  target?: string;
  action: string;
  detail?: string;
  error?: string;
}

// Model routing projection states: the engine vocabulary plus the
// never-synced extension for declared-but-never-synced targets.
export type ModelsTargetState =
  | 'in-sync'
  | 'stale'
  | 'drifted'
  | 'target-missing'
  | 'never-synced';

// One target row from GET /api/project/models/status. Targets is
// variable-length: the fragment row always exists; the include and
// OpenCode rows appear only when declared in the policy or recorded in
// the lockfile.
export interface ModelsTargetStatus {
  target: 'litellm-fragment' | 'litellm-include' | 'opencode';
  client: string;
  state: ModelsTargetState;
  /** Annotation on the fragment row, never a drift state: the file on
   *  disk is newer than what the running LiteLLM proxy is serving. */
  restart_pending?: boolean;
  path?: string;
  detail?: string;
  synced_at?: string;
}

// Read-only routing summary projected from the parsed policy.
export interface ModelsRoutingSummary {
  entry_model: string;
  default_tier: string;
  backends: string[];
  tiers: Record<string, string>;
}

// Response of GET /api/project/models/status.
export interface ModelsStatusDoc {
  policy_path: string;
  policy_exists: boolean;
  needs_attention: boolean;
  /** Parse failure carried in the document; status never 500s on it. */
  policy_error?: string;
  routing?: ModelsRoutingSummary;
  targets: ModelsTargetStatus[];
}

// One row from POST /api/project/models/sync.
export interface ModelsSyncResult {
  target: string;
  client: string;
  path: string;
  action: string;
  detail?: string;
  backup_path?: string;
  diff?: string;
  error?: string;
}

// One row from POST /api/project/models/adopt.
export interface ModelsAdoptResult {
  target: string;
  client: string;
  path: string;
  action: string;
  detail?: string;
}

// One finding from GET /api/project/models/validate.
export interface ModelsIssue {
  severity: 'error' | 'warning';
  field: string;
  message: string;
}

// Response of GET /api/project/models/validate.
export interface ModelsValidateDoc {
  policy_path: string;
  valid: boolean;
  issues: ModelsIssue[];
}

// Response shape of GET /api/sessions. entries rides alongside the
// legacy bare ID list and is absent from pre-dual-stack daemons, whose
// sessions are all handshake-generation by definition.
export interface SessionsResponse {
  count: number;
  sessions: string[];
  entries?: SessionEntry[];
}
