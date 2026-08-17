import type { GatewayStatus, MCPServerStatus, ServerAuthInfo, ServerAuthLogin, ClientStatus, ToolsListResult, ToolUsageResponse, SkillUsageResponse, RegistryStatus, AgentSkill, ItemState, SkillFile, SkillValidationResult, TokenMetricsResponse, OptimizeReport, ValidationResult, PlanDiff, SpecHealth, StackSpec, SkillSourceStatus, SkillPreviewResponse, ImportResult, SourceUpdateCheck, UpdateSummary, SourceSyncSummary, SkillSyncResult, SkillDiffResponse, InventoryRecord, TelemetryMutationResponse, TelemetryPersistDefaults, TelemetryRetention, SessionsResponse, RegistryAgent, AgentProjectionStatus, AgentSyncResult, AgentUnsyncResult, AgentAdoptResult, SecurityFinding, WiringRow, WiringAdoptResult } from '../types';

// Base URL for API calls - empty for same origin
const API_BASE = '';

// === Auth Token Management ===

const AUTH_STORAGE_KEY = 'gridctl-auth-token';

export class AuthError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'AuthError';
  }
}

/**
 * HTTPError carries the server response status alongside the error message
 * so callers can branch on classified statuses (401/404/400) — e.g. the
 * skill wizard auto-expanding its auth card on 401 or 404.
 */
export class HTTPError extends Error {
  status: number;
  /**
   * Stable machine code for failures a client has a distinct remedy for
   * (currently `ssh_agent_unavailable`). Branch on this, never on message
   * text, which is prose and changes.
   */
  code?: string;
  /**
   * For an ssh-agent failure on an SSH URL, the server's rewrite of that URL
   * to HTTPS. Absent when the input was not SSH-form.
   */
  httpsEquivalent?: string;
  constructor(
    status: number,
    message: string,
    extra?: { code?: string; httpsEquivalent?: string },
  ) {
    super(message);
    this.status = status;
    this.name = 'HTTPError';
    this.code = extra?.code;
    this.httpsEquivalent = extra?.httpsEquivalent;
  }
}

/** Pull the structured git-error fields off an error response body. */
function gitErrorExtra(data: unknown): { code?: string; httpsEquivalent?: string } {
  const body = (data ?? {}) as { code?: unknown; httpsEquivalent?: unknown };
  return {
    code: typeof body.code === 'string' ? body.code : undefined,
    httpsEquivalent:
      typeof body.httpsEquivalent === 'string' ? body.httpsEquivalent : undefined,
  };
}

/**
 * Auth payload accepted by all /api/skills/sources/* endpoints. The raw
 * Token is transient (used once, never persisted); CredentialRef is the
 * "${vault:KEY}" reference that the server resolves against the live
 * vault on every request and that gets recorded in lock/origin for
 * subsequent updates.
 */
export type SkillAuthMethod = 'token' | 'ssh-agent' | 'ssh-key' | '';
export interface SkillAuth {
  method?: SkillAuthMethod;
  token?: string;
  credentialRef?: string;
  sshUser?: string;
  sshKeyPath?: string;
}

export function getStoredToken(): string | null {
  try {
    return localStorage.getItem(AUTH_STORAGE_KEY);
  } catch {
    return null;
  }
}

export function storeToken(token: string): void {
  try {
    localStorage.setItem(AUTH_STORAGE_KEY, token);
  } catch {
    // localStorage may be unavailable
  }
}

export function clearToken(): void {
  try {
    localStorage.removeItem(AUTH_STORAGE_KEY);
  } catch {
    // localStorage may be unavailable
  }
}

function buildHeaders(extra?: Record<string, string>): Record<string, string> {
  const headers: Record<string, string> = { ...extra };
  const token = getStoredToken();
  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }
  return headers;
}

// === Generic Fetch Wrapper ===

async function fetchJSON<T>(endpoint: string): Promise<T> {
  const response = await fetch(`${API_BASE}${endpoint}`, {
    headers: buildHeaders(),
  });

  if (response.status === 401) {
    throw new AuthError('Authentication required');
  }

  if (!response.ok) {
    throw new Error(`API error: ${response.status} ${response.statusText}`);
  }

  return response.json();
}

// === API Functions ===

/**
 * Fetch gateway status including all MCP server statuses
 * GET /api/status
 */
export async function fetchStatus(): Promise<GatewayStatus> {
  return fetchJSON<GatewayStatus>('/api/status');
}

/**
 * Fetch list of registered MCP servers
 * GET /api/mcp-servers
 */
export async function fetchMCPServers(): Promise<MCPServerStatus[]> {
  return fetchJSON<MCPServerStatus[]>('/api/mcp-servers');
}

/**
 * Fetch all aggregated tools from all MCP servers
 * GET /api/tools
 */
export async function fetchTools(): Promise<ToolsListResult> {
  return fetchJSON<ToolsListResult>('/api/tools');
}

/**
 * Fetch the full downstream tool inventory with each tool's raw description and
 * input schema, regardless of code mode. Unlike /api/tools (which returns only
 * the meta-tools when code mode is on), this informational endpoint always
 * carries the real per-tool detail the Tools workspace renders.
 *
 * `include=all` bypasses the whitelist filter so whitelist-disabled tools keep
 * their descriptions, schemas, and annotations in the UI (the operator is
 * deciding whether to re-enable exactly those rows). Informational only; the
 * MCP-facing tool surface is unaffected.
 * GET /api/tools/catalog?include=all
 */
export async function fetchToolCatalog(): Promise<ToolsListResult> {
  return fetchJSON<ToolsListResult>('/api/tools/catalog?include=all');
}

/**
 * Fetch per-(server, tool) usage: cumulative call counts + last-called
 * timestamps observed by the gateway. Powers Tools workspace Audit Mode.
 * Survives gateway restarts for servers with metrics persistence enabled.
 * GET /api/tools/usage
 */
export async function fetchToolUsage(): Promise<ToolUsageResponse> {
  return fetchJSON<ToolUsageResponse>('/api/tools/usage');
}

/**
 * Fetch detected/linked LLM clients
 * GET /api/clients
 */
export async function fetchClients(): Promise<ClientStatus[]> {
  return fetchJSON<ClientStatus[]>('/api/clients');
}

/**
 * Fetch active MCP sessions with per-session protocol generation
 * GET /api/sessions
 */
export async function fetchSessions(): Promise<SessionsResponse> {
  return fetchJSON<SessionsResponse>('/api/sessions');
}

// === MCP Server Control Functions ===

/**
 * Fetch logs for a specific MCP server
 * GET /api/mcp-servers/{name}/logs
 */
export async function fetchServerLogs(name: string, lines = 100): Promise<string[]> {
  const response = await fetch(
    `${API_BASE}/api/mcp-servers/${encodeURIComponent(name)}/logs?lines=${lines}`,
    { headers: buildHeaders() },
  );

  if (response.status === 401) {
    throw new AuthError('Authentication required');
  }

  if (!response.ok) {
    let errorMessage = `Logs fetch failed: ${response.status} ${response.statusText}`;
    try {
      const errorData = await response.json();
      if (errorData.error) {
        errorMessage = errorData.error;
      }
    } catch {
      // JSON parsing failed, use default message
    }
    throw new Error(errorMessage);
  }

  return response.json();
}

/**
 * Restart an MCP server connection
 * POST /api/mcp-servers/{name}/restart
 */
export async function restartMCPServer(name: string): Promise<void> {
  const response = await fetch(
    `${API_BASE}/api/mcp-servers/${encodeURIComponent(name)}/restart`,
    {
      method: 'POST',
      headers: buildHeaders(),
    },
  );

  if (response.status === 401) {
    throw new AuthError('Authentication required');
  }

  if (!response.ok) {
    const data = await response.json().catch(() => ({}));
    throw new Error(data.error || `Restart failed: ${response.status} ${response.statusText}`);
  }
}

// Response payload for PUT /api/mcp-servers/{name}/tools on success.
export interface SetServerToolsResponse {
  server: string;
  tools: string[];
  reloaded: boolean;
  reloadedAt?: string; // RFC3339 timestamp, only present when reloaded is true
}

// SetServerToolsError is thrown when the backend returns a structured error
// envelope ({error: {code, message, hint?}}) for the tool-whitelist update
// endpoint. It lets the UI branch on `code` to show stable copy.
//
// Known codes:
//   - "stack_modified" (409): the YAML on disk changed since the handler read
//     it. The UI should offer a "Reload file" affordance and preserve the
//     user's pending selection on top of the refreshed state.
//   - "reload_failed" (502): the YAML write succeeded but the hot reload
//     returned an error. The save persisted; only the reload failed.
//   - "unknown_tool" (400): a tool name in the request is not advertised by
//     the server. Surface the message directly so the operator can fix it.
export class SetServerToolsError extends Error {
  code: string;
  hint?: string;
  httpStatus: number;

  constructor(code: string, message: string, hint: string | undefined, httpStatus: number) {
    super(message);
    this.name = 'SetServerToolsError';
    this.code = code;
    this.hint = hint;
    this.httpStatus = httpStatus;
  }
}

/**
 * Update the tool whitelist for an MCP server in the live stack YAML and
 * trigger a hot reload. An empty array clears the whitelist (exposing all
 * tools, matching stack YAML semantics).
 *
 * Rejects with SetServerToolsError on 400/409/502 (structured envelope),
 * AuthError on 401, or a plain Error for other failures.
 * PUT /api/mcp-servers/{name}/tools
 */
export async function setServerTools(
  name: string,
  tools: string[],
): Promise<SetServerToolsResponse> {
  const response = await fetch(
    `${API_BASE}/api/mcp-servers/${encodeURIComponent(name)}/tools`,
    {
      method: 'PUT',
      headers: buildHeaders({ 'Content-Type': 'application/json' }),
      body: JSON.stringify({ tools }),
    },
  );

  if (response.status === 401) throw new AuthError('Authentication required');

  const data = await response.json().catch(() => null);

  if (!response.ok) {
    const err = data?.error;
    if (err && typeof err === 'object' && typeof err.code === 'string') {
      throw new SetServerToolsError(
        err.code,
        err.message ?? 'Set tools failed',
        err.hint,
        response.status,
      );
    }
    // Plain {error: "..."} envelope — fall through to a generic Error.
    const msg =
      typeof err === 'string' ? err : `Set tools failed: ${response.status} ${response.statusText}`;
    throw new Error(msg);
  }

  return data as SetServerToolsResponse;
}

// One server's whitelist change in a batch request. tools = [] clears the
// whitelist (expose all), matching the single-server semantics.
export interface ServerToolsBatchEntry {
  name: string;
  tools: string[];
}

// One server's applied whitelist in a successful batch response.
export interface ServerToolsBatchResult {
  server: string;
  tools: string[];
}

// Response payload for PUT /api/mcp-servers/tools on success. The batch is
// atomic, so every listed server was applied and a single reload (when enabled)
// ran once for the whole batch.
export interface SetServerToolsBatchResponse {
  servers: ServerToolsBatchResult[];
  reloaded: boolean;
  reloadedAt?: string; // RFC3339, only present when reloaded is true
}

/**
 * Apply tool-whitelist changes to MULTIPLE servers in one atomic write that
 * triggers a SINGLE reload — the fleet-bulk path. Transaction semantics are
 * all-or-nothing: if any tool is unknown the whole batch is rejected and
 * nothing is written (SetServerToolsError code "unknown_tool", message names
 * the offending server). A concurrent external edit rejects with
 * "stack_modified" (409); a write that reloads-failed surfaces "reload_failed"
 * (502) with the changes persisted — mirroring the single-server endpoint.
 *
 * PUT /api/mcp-servers/tools
 */
export async function setServerToolsBatch(
  servers: ServerToolsBatchEntry[],
): Promise<SetServerToolsBatchResponse> {
  const response = await fetch(`${API_BASE}/api/mcp-servers/tools`, {
    method: 'PUT',
    headers: buildHeaders({ 'Content-Type': 'application/json' }),
    body: JSON.stringify({ servers }),
  });

  if (response.status === 401) throw new AuthError('Authentication required');

  const data = await response.json().catch(() => null);

  if (!response.ok) {
    const err = data?.error;
    if (err && typeof err === 'object' && typeof err.code === 'string') {
      throw new SetServerToolsError(
        err.code,
        err.message ?? 'Batch set tools failed',
        err.hint,
        response.status,
      );
    }
    const msg =
      typeof err === 'string'
        ? err
        : `Batch set tools failed: ${response.status} ${response.statusText}`;
    throw new Error(msg);
  }

  return data as SetServerToolsBatchResponse;
}

// ClientScopeError carries the structured envelope from the per-client scope
// write endpoint, mirroring SetServerToolsError:
//   - "stack_modified" (409): the stack file changed on disk since read.
//   - "unknown_server"/"unknown_tool" (422): the scope references a server or
//     tool the gateway does not know about (stale UI).
//   - "reload_failed" (502): the YAML write succeeded but the reload failed.
export class ClientScopeError extends Error {
  code: string;
  hint?: string;
  httpStatus: number;

  constructor(code: string, message: string, hint: string | undefined, httpStatus: number) {
    super(message);
    this.name = 'ClientScopeError';
    this.code = code;
    this.hint = hint;
    this.httpStatus = httpStatus;
  }
}

// ClientScopeUpdate is the allow-list written for one client profile. Each axis
// is independent: an omitted field leaves that axis untouched (so a server-only
// edit preserves an operator's tool list), while a present array replaces it.
export interface ClientScopeUpdate {
  servers?: string[];
  tools?: string[];
}

// UpdateClientScopeResponse is the success payload from the write endpoint.
export interface UpdateClientScopeResponse {
  client: string;
  profileKey: string;
  servers: string[];
  tools: string[];
  reloaded: boolean;
  reloadedAt?: string;
}

/**
 * Persist a client's access profile (allowed servers and/or tools) to the live
 * stack YAML's `clients:` block and trigger a hot reload. The slug is the
 * client identifier; the gateway normalizes it to the stable profile key.
 *
 * Rejects with ClientScopeError on 409/422/502 (structured envelope), AuthError
 * on 401, or a plain Error otherwise.
 * PUT /api/clients/{slug}/scope
 */
export async function updateClientScope(
  slug: string,
  update: ClientScopeUpdate,
): Promise<UpdateClientScopeResponse> {
  const response = await fetch(
    `${API_BASE}/api/clients/${encodeURIComponent(slug)}/scope`,
    {
      method: 'PUT',
      headers: buildHeaders({ 'Content-Type': 'application/json' }),
      body: JSON.stringify(update),
    },
  );

  if (response.status === 401) throw new AuthError('Authentication required');

  const data = await response.json().catch(() => null);

  if (!response.ok) {
    const err = data?.error;
    if (err && typeof err === 'object' && typeof err.code === 'string') {
      throw new ClientScopeError(
        err.code,
        err.message ?? 'Update client scope failed',
        err.hint,
        response.status,
      );
    }
    const msg =
      typeof err === 'string'
        ? err
        : `Update client scope failed: ${response.status} ${response.statusText}`;
    throw new Error(msg);
  }

  return data as UpdateClientScopeResponse;
}

// ClientLinkError mirrors ClientScopeError for the link endpoints:
//   - "unknown_client" (404): the slug matches no registered provisioner.
//   - "client_not_detected" (422): the client is not installed here.
//   - "link_conflict" (409): a foreign entry occupies the target name;
//     nothing was written.
//   - "stack_not_updated" (500): the client config WAS written but the stack
//     file was not — surface both facts; nothing is rolled back.
export class ClientLinkError extends Error {
  code: string;
  hint?: string;
  httpStatus: number;

  constructor(code: string, message: string, hint: string | undefined, httpStatus: number) {
    super(message);
    this.name = 'ClientLinkError';
    this.code = code;
    this.hint = hint;
    this.httpStatus = httpStatus;
  }
}

// LinkClientOptions carries the optional declared-entry fields (mirrors the
// stack.yaml link: object form).
export interface LinkClientOptions {
  group?: string;
  clientId?: string;
  name?: string;
  /** Overwrite a foreign or drifted entry (the engine backs it up first),
   *  mirroring `gridctl link --force`. Without it those states 409. */
  force?: boolean;
}

// LinkClientResponse echoes the applied state from POST/DELETE.
export interface LinkClientResponse {
  client: string;
  serverName: string;
  linked: boolean;
  declared: boolean;
  alreadyLinked?: boolean;
  configPath?: string;
}

// ClientLinkPreview is the dry-run payload: client config before/after plus
// the unified diff of the stack.yaml change. Nothing is written.
export interface ClientLinkPreview {
  client: string;
  serverName: string;
  configPath: string;
  before: string;
  after: string;
  stackDiff: string;
}

async function clientLinkFetch<T>(
  slug: string,
  sub: '' | '/preview',
  method: 'POST' | 'DELETE',
  body?: unknown,
): Promise<T> {
  const response = await fetch(
    `${API_BASE}/api/clients/${encodeURIComponent(slug)}/link${sub}`,
    {
      method,
      headers: buildHeaders(body !== undefined ? { 'Content-Type': 'application/json' } : {}),
      body: body !== undefined ? JSON.stringify(body) : undefined,
    },
  );

  if (response.status === 401) throw new AuthError('Authentication required');

  const data = await response.json().catch(() => null);
  if (!response.ok) {
    const err = data?.error;
    if (err && typeof err === 'object' && typeof err.code === 'string') {
      throw new ClientLinkError(
        err.code,
        err.message ?? 'Client link request failed',
        err.hint,
        response.status,
      );
    }
    const msg = typeof err === 'string' ? err : `Client link request failed: ${response.status}`;
    throw new Error(msg);
  }
  return data as T;
}

/**
 * Link a client to the gateway AND declare it in the stack's link: block.
 * POST /api/clients/{slug}/link
 */
export async function linkClient(
  slug: string,
  opts: LinkClientOptions = {},
): Promise<LinkClientResponse> {
  return clientLinkFetch<LinkClientResponse>(slug, '', 'POST', opts);
}

/**
 * Unlink a client and remove its link: declaration.
 * DELETE /api/clients/{slug}/link
 */
export async function unlinkClient(slug: string): Promise<LinkClientResponse> {
  return clientLinkFetch<LinkClientResponse>(slug, '', 'DELETE');
}

/**
 * Preview a link: client config before/after plus the stack.yaml diff.
 * Nothing is written. POST /api/clients/{slug}/link/preview
 */
export async function previewClientLink(
  slug: string,
  opts: LinkClientOptions = {},
): Promise<ClientLinkPreview> {
  return clientLinkFetch<ClientLinkPreview>(slug, '/preview', 'POST', opts);
}

// ClientScopeImpact is one client's before/after access delta in a scope
// preview. Mirrors api.clientScopeImpact.
export interface ClientScopeImpact {
  name: string;
  slug: string;
  beforeServers: number;
  afterServers: number;
  beforeTools: number;
  afterTools: number;
  lostServers: string[] | null;
  gainedServers: string[] | null;
}

// ClientScopePreview is the read-only result of POST /scope/preview: the exact
// stack.yaml patch a commit would write plus its per-client consequences.
// Mirrors api.scopePreviewResponse.
export interface ClientScopePreview {
  client: string;
  profileKey: string;
  createsBlock: boolean;
  lockout: boolean;
  totalServers: number;
  totalTools: number;
  diff: string;
  selected: ClientScopeImpact;
  affected: ClientScopeImpact[] | null;
}

/**
 * Preview committing a client access draft without writing. Returns the exact
 * YAML patch and the per-client impact computed server-side (the faithful
 * source the commit gate renders). Rejects with ClientScopeError on 422 (stale
 * server/tool reference), AuthError on 401, or a plain Error otherwise.
 * POST /api/clients/{slug}/scope/preview
 */
export async function previewClientScope(
  slug: string,
  update: ClientScopeUpdate,
): Promise<ClientScopePreview> {
  const response = await fetch(
    `${API_BASE}/api/clients/${encodeURIComponent(slug)}/scope/preview`,
    {
      method: 'POST',
      headers: buildHeaders({ 'Content-Type': 'application/json' }),
      body: JSON.stringify(update),
    },
  );

  if (response.status === 401) throw new AuthError('Authentication required');

  const data = await response.json().catch(() => null);

  if (!response.ok) {
    const err = data?.error;
    if (err && typeof err === 'object' && typeof err.code === 'string') {
      throw new ClientScopeError(
        err.code,
        err.message ?? 'Scope preview failed',
        err.hint,
        response.status,
      );
    }
    const msg =
      typeof err === 'string'
        ? err
        : `Scope preview failed: ${response.status} ${response.statusText}`;
    throw new Error(msg);
  }

  return data as ClientScopePreview;
}

// === Structured Log Entry (from gateway) ===

export interface LogEntry {
  level: string;     // "DEBUG", "INFO", "WARN", "ERROR"
  ts: string;        // RFC3339Nano timestamp
  msg: string;       // Log message
  component?: string; // Component name (e.g., "gateway", "router")
  trace_id?: string;  // Trace ID for correlation
  attrs?: Record<string, unknown>; // Additional attributes
}

// Envelope served by GET /api/logs: the windowed entries plus ring occupancy
// (total) and capacity, so the UI can label the window against retention.
export interface GatewayLogsResponse {
  logs: LogEntry[];
  total: number;
  bufferCapacity: number;
}

export async function fetchGatewayLogs(lines = 100, level?: string): Promise<GatewayLogsResponse> {
  let url = `${API_BASE}/api/logs?lines=${lines}`;
  if (level) {
    url += `&level=${encodeURIComponent(level)}`;
  }
  const response = await fetch(url, { headers: buildHeaders() });

  if (response.status === 401) {
    throw new AuthError('Authentication required');
  }

  if (!response.ok) {
    throw new Error(`Logs fetch failed: ${response.status} ${response.statusText}`);
  }

  return response.json();
}

// === Token Metrics API ===

/**
 * Fetch historical token metrics
 * GET /api/metrics/tokens?range=1h
 */
export async function fetchTokenMetrics(range: string = '1h'): Promise<TokenMetricsResponse> {
  return fetchJSON<TokenMetricsResponse>(`/api/metrics/tokens?range=${encodeURIComponent(range)}`);
}

/**
 * Clear all recorded metrics. The endpoint is a total wipe (Accumulator.Clear):
 * tokens, cost, tool usage, and model history all reset together.
 * DELETE /api/metrics/tokens
 */
export async function clearTokenMetrics(): Promise<void> {
  const response = await fetch(`${API_BASE}/api/metrics/tokens`, {
    method: 'DELETE',
    headers: buildHeaders(),
  });

  if (response.status === 401) {
    throw new AuthError('Authentication required');
  }

  if (!response.ok) {
    throw new Error(`Clear metrics failed: ${response.status} ${response.statusText}`);
  }
}

/**
 * Fetch the optimize report (unused servers, unused tools, etc.) for
 * the active stack. Mirrors fetchTokenMetrics so the sidebar panel can
 * poll on the same cadence as Token Usage.
 * GET /api/optimize?min_impact=1000&severity=warn,critical
 */
export async function fetchOptimizeReport(opts?: {
  stack?: string;
  minImpact?: number;
  severity?: string[];
}): Promise<OptimizeReport> {
  const params = new URLSearchParams();
  if (opts?.stack) params.set('stack', opts.stack);
  if (opts?.minImpact && opts.minImpact > 0) params.set('min_impact', String(opts.minImpact));
  if (opts?.severity && opts.severity.length > 0) params.set('severity', opts.severity.join(','));
  const query = params.toString();
  return fetchJSON<OptimizeReport>(`/api/optimize${query ? `?${query}` : ''}`);
}

// === Reload API ===

export interface ReloadResult {
  success: boolean;
  message: string;
  added?: string[];
  removed?: string[];
  modified?: string[];
  errors?: string[];
}

/**
 * Trigger a configuration reload
 * POST /api/reload
 */
export async function triggerReload(): Promise<ReloadResult> {
  const response = await fetch(`${API_BASE}/api/reload`, {
    method: 'POST',
    headers: buildHeaders(),
  });

  if (response.status === 401) {
    throw new AuthError('Authentication required');
  }

  const data = await response.json();

  if (!response.ok) {
    throw new Error(data.error || `Reload failed: ${response.status}`);
  }

  return data;
}

// === Registry API ===

async function mutateJSON<T>(
  endpoint: string,
  method: 'POST' | 'PUT' | 'DELETE',
  body?: unknown,
): Promise<T> {
  const headers: Record<string, string> = { ...buildHeaders() };
  if (body !== undefined) {
    headers['Content-Type'] = 'application/json';
  }

  const response = await fetch(`${API_BASE}${endpoint}`, {
    method,
    headers,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });

  if (response.status === 401) throw new AuthError('Authentication required');

  if (!response.ok) {
    const data = await response.json().catch(() => ({}));
    throw new HTTPError(
      response.status,
      data.error || `${method} ${endpoint} failed: ${response.status}`,
      gitErrorExtra(data),
    );
  }

  // DELETE returns no body
  if (method === 'DELETE') return undefined as T;
  return response.json();
}

export async function fetchRegistryStatus(): Promise<RegistryStatus> {
  return fetchJSON<RegistryStatus>('/api/registry/status');
}

// --- Agent Skills ---

export async function fetchRegistrySkills(): Promise<AgentSkill[]> {
  return fetchJSON<AgentSkill[]>('/api/registry/skills');
}

/**
 * Per-skill prompts/get usage: cumulative call counts plus last-called
 * timestamps, keyed by skill name. Powers the Library "Last used" column,
 * the inspector usage line, and the "Never used" facet. Joined to skills by
 * name, so the registry list payload stays unchanged. Survives gateway
 * restarts when metrics persistence is enabled.
 * GET /api/skills/usage
 */
export async function fetchSkillUsage(): Promise<SkillUsageResponse> {
  return fetchJSON<SkillUsageResponse>('/api/skills/usage');
}

export async function fetchRegistrySkill(name: string): Promise<AgentSkill> {
  return fetchJSON<AgentSkill>(`/api/registry/skills/${encodeURIComponent(name)}`);
}

export async function createRegistrySkill(skill: AgentSkill): Promise<AgentSkill> {
  return mutateJSON<AgentSkill>('/api/registry/skills', 'POST', skill);
}

export async function updateRegistrySkill(name: string, skill: AgentSkill): Promise<AgentSkill> {
  return mutateJSON<AgentSkill>(`/api/registry/skills/${encodeURIComponent(name)}`, 'PUT', skill);
}

export async function deleteRegistrySkill(name: string): Promise<void> {
  return mutateJSON<void>(`/api/registry/skills/${encodeURIComponent(name)}`, 'DELETE');
}

export async function activateRegistrySkill(name: string): Promise<AgentSkill> {
  return mutateJSON<AgentSkill>(`/api/registry/skills/${encodeURIComponent(name)}/activate`, 'POST');
}

export async function disableRegistrySkill(name: string): Promise<AgentSkill> {
  return mutateJSON<AgentSkill>(`/api/registry/skills/${encodeURIComponent(name)}/disable`, 'POST');
}

export interface RegistrySkillBatchEntry {
  name: string;
  // Bulk actions enable or disable; they never set draft.
  state: Extract<ItemState, 'active' | 'disabled'>;
}

export interface SetRegistrySkillsBatchResponse {
  skills: { name: string; state: ItemState }[];
}

// PUT /api/registry/skills/batch: set the state of multiple skills in one
// all-or-nothing request (the whole batch is validated before any write).
export async function setRegistrySkillsBatch(
  skills: RegistrySkillBatchEntry[],
): Promise<SetRegistrySkillsBatchResponse> {
  return mutateJSON<SetRegistrySkillsBatchResponse>('/api/registry/skills/batch', 'PUT', { skills });
}

// --- Agents (imported agent definitions) ---

export async function fetchRegistryAgents(): Promise<RegistryAgent[]> {
  return fetchJSON<RegistryAgent[]>('/api/registry/agents');
}

export async function fetchRegistryAgent(name: string): Promise<RegistryAgent> {
  return fetchJSON<RegistryAgent>(`/api/registry/agents/${encodeURIComponent(name)}`);
}

/**
 * AgentScanError carries the blocking security-scan findings from a 409 on
 * PUT /api/registry/agents/{name}, so the editor can render the actual
 * findings instead of a generic error string.
 */
export class AgentScanError extends HTTPError {
  findings: SecurityFinding[];
  constructor(message: string, findings: SecurityFinding[]) {
    super(409, message);
    this.name = 'AgentScanError';
    this.findings = findings;
  }
}

/**
 * PUT the whole AGENT.md back. The server re-parses, refuses renames, runs
 * the blocking security scan, and writes the bytes verbatim. A scan 409
 * throws AgentScanError with the findings attached.
 */
export async function updateRegistryAgent(name: string, raw: string): Promise<RegistryAgent> {
  const response = await fetch(`${API_BASE}/api/registry/agents/${encodeURIComponent(name)}`, {
    method: 'PUT',
    headers: buildHeaders({ 'Content-Type': 'application/json' }),
    body: JSON.stringify({ raw }),
  });
  if (response.status === 401) throw new AuthError('Authentication required');
  if (!response.ok) {
    const data = await response.json().catch(() => ({}));
    if (response.status === 409 && Array.isArray(data.findings)) {
      throw new AgentScanError(data.error ?? 'Security scan blocked the save', data.findings);
    }
    throw new HTTPError(response.status, data.error || `PUT agent failed: ${response.status}`);
  }
  return response.json();
}

export async function deleteRegistryAgent(name: string): Promise<void> {
  return mutateJSON<void>(`/api/registry/agents/${encodeURIComponent(name)}`, 'DELETE');
}

// --- Agent projection (REST face of `gridctl skill project --kind agent`) ---

export async function fetchAgentProjectionStatus(): Promise<AgentProjectionStatus[]> {
  return fetchJSON<AgentProjectionStatus[]>('/api/project/agents/status');
}

export async function syncAgentProjections(body?: {
  agents?: string[];
  clients?: string[];
  force?: boolean;
  dry_run?: boolean;
}): Promise<AgentSyncResult[]> {
  return mutateJSON<AgentSyncResult[]>('/api/project/agents/sync', 'POST', body ?? {});
}

export async function unsyncAgentProjections(body: {
  agents?: string[];
  clients?: string[];
  all?: boolean;
  dry_run?: boolean;
}): Promise<AgentUnsyncResult[]> {
  return mutateJSON<AgentUnsyncResult[]>('/api/project/agents/unsync', 'POST', body);
}

export async function adoptAgentProjection(agent: string, client: string): Promise<AgentAdoptResult> {
  return mutateJSON<AgentAdoptResult>('/api/project/agents/adopt', 'POST', { agent, client });
}

// --- Wiring ownership (REST face of `gridctl project --kind wiring`) ---

export async function fetchWiringStatus(): Promise<WiringRow[]> {
  return fetchJSON<WiringRow[]>('/api/project/wiring/status');
}

/**
 * Adopt records ownership of the entry's current value without rewriting
 * it. Refusals arrive as 409 with the engine's reason; callers render
 * that message verbatim.
 */
export async function adoptWiringEntry(client: string, name?: string): Promise<WiringAdoptResult> {
  return mutateJSON<WiringAdoptResult>('/api/project/wiring/adopt', 'POST', {
    client,
    ...(name ? { name } : {}),
  });
}

// --- Skill File Management ---

export async function fetchSkillFiles(skillName: string): Promise<SkillFile[]> {
  return fetchJSON<SkillFile[]>(`/api/registry/skills/${encodeURIComponent(skillName)}/files`);
}

export async function fetchSkillFile(skillName: string, filePath: string): Promise<string> {
  const response = await fetch(
    `${API_BASE}/api/registry/skills/${encodeURIComponent(skillName)}/files/${filePath}`,
    { headers: buildHeaders() }
  );
  if (response.status === 401) throw new AuthError('Authentication required');
  if (!response.ok) {
    throw new Error(`Failed to read file: ${response.status} ${response.statusText}`);
  }
  return response.text();
}

export async function writeSkillFile(skillName: string, filePath: string, content: string): Promise<void> {
  const response = await fetch(
    `${API_BASE}/api/registry/skills/${encodeURIComponent(skillName)}/files/${filePath}`,
    {
      method: 'PUT',
      headers: buildHeaders({ 'Content-Type': 'application/octet-stream' }),
      body: content,
    }
  );
  if (response.status === 401) throw new AuthError('Authentication required');
  if (!response.ok) {
    throw new Error(`Failed to write file: ${response.status} ${response.statusText}`);
  }
}

export async function deleteSkillFile(skillName: string, filePath: string): Promise<void> {
  const response = await fetch(
    `${API_BASE}/api/registry/skills/${encodeURIComponent(skillName)}/files/${filePath}`,
    {
      method: 'DELETE',
      headers: buildHeaders(),
    }
  );
  if (response.status === 401) throw new AuthError('Authentication required');
  if (!response.ok) {
    throw new Error(`Failed to delete file: ${response.status} ${response.statusText}`);
  }
}

// --- Skill Validation ---

export async function validateSkillContent(content: string): Promise<SkillValidationResult> {
  return mutateJSON<SkillValidationResult>('/api/registry/skills/validate', 'POST', { content });
}


// === Variable Store API ===

// Variable types accepted by `gridctl var set --type`. PR 1 records the type
// as metadata only — expansion still treats every value as a string.
export type VariableType = 'string' | 'json' | 'list' | 'number' | 'bool';

export interface Variable {
  key: string;
  type: VariableType;
  is_secret: boolean;
  set?: string;
  // RFC3339 stamp of the last value change (rotation or manual edit). Absent
  // means unknown, not never: variables untouched since the field shipped
  // carry no stamp, so render the absence as unknown rather than a date.
  last_rotated?: string;
}

export interface VariableSet {
  name: string;
  description?: string;
  count: number;
}

export async function fetchVariables(): Promise<Variable[]> {
  return fetchJSON<Variable[]>('/api/var');
}

// ConsumerKind mirrors the backend config.ReferenceKind: where in the active
// stack a ${var:KEY} reference appears. 'mcp-server' and 'resource' map to
// canvas nodes by `name`; 'gateway', 'network', and 'stack' are stack-level
// sites that belong to no node.
//
// 'secrets-set' is a synthetic consumer for variables bulk-injected via
// secrets.sets. It has no YAML site of its own, so `name` is the set rather
// than a workload. When that set is scoped, `target`/`targetKind` name the one
// workload the entry injects into, which does map to a node: use
// navigationTarget and consumerReachesWorkload rather than reading `kind` or
// `name` directly, or scoped injections silently drop out of workload queries.
export type ConsumerKind =
  | 'mcp-server'
  | 'resource'
  | 'gateway'
  | 'network'
  | 'stack'
  | 'secrets-set';

// Consumer is a single site that references a variable. `field` is the YAML key
// path the user wrote (e.g. "env.GITHUB_TOKEN", "image", "openapi.baseUrl").
export interface Consumer {
  kind: ConsumerKind;
  name?: string;
  field: string;
  // Set only on 'secrets-set' consumers built from a *scoped* set: the
  // workload that entry injects into, and whether it is a server or a
  // resource. One such consumer exists per receiving workload. An unscoped
  // set fans out and leaves both empty.
  //
  // `name` keeps holding the set name in every case, so callers asking "is
  // this variable's own set injected" still compare against `name`.
  target?: string;
  targetKind?: ConsumerKind;
}

// DriftEntry is a ${var:KEY} reference in the stack that no stored variable
// satisfies. References carrying a default (${var:KEY:-fallback}) are valid
// config and never appear here. The backend decides, so the UI cannot invent
// a second, looser definition of "missing".
export interface DriftEntry {
  key: string;
  consumers: Consumer[];
}

// fetchVariableDrift lists stack references to variables that do not exist.
// Keys and reference sites only, never values. A locked vault returns an empty
// list (membership is uncheckable while locked) rather than flagging everything.
export async function fetchVariableDrift(): Promise<DriftEntry[]> {
  return fetchJSON<DriftEntry[]>('/api/var/drift');
}

// fetchVariableUsage returns the usage index for the active stack: each variable
// key mapped to the consumers that reference it. Returns {} when no stack is
// loaded. Derived from the stack file (never the vault), so it carries no values
// and is safe to call while the vault is locked.
export async function fetchVariableUsage(): Promise<Record<string, Consumer[]>> {
  return fetchJSON<Record<string, Consumer[]>>('/api/var/usage');
}

export interface CreateVariableInput {
  key: string;
  value: string;
  type?: VariableType;
  isSecret?: boolean;
  set?: string;
}

export async function createVariable(input: CreateVariableInput): Promise<void> {
  const body: Record<string, unknown> = { key: input.key, value: input.value };
  if (input.type !== undefined) body.type = input.type;
  if (input.isSecret !== undefined) body.is_secret = input.isSecret;
  if (input.set) body.set = input.set;
  await mutateJSON<unknown>('/api/var', 'POST', body);
}

export interface VariableDetail extends Variable {
  value: string;
}

export async function getVariable(key: string): Promise<VariableDetail> {
  return fetchJSON<VariableDetail>(`/api/var/${encodeURIComponent(key)}`);
}

export interface UpdateVariableInput {
  value?: string;
  type?: VariableType;
  isSecret?: boolean;
  set?: string;
}

export async function updateVariable(key: string, input: UpdateVariableInput): Promise<void> {
  const body: Record<string, unknown> = {};
  if (input.value !== undefined) body.value = input.value;
  if (input.type !== undefined) body.type = input.type;
  if (input.isSecret !== undefined) body.is_secret = input.isSecret;
  if (input.set !== undefined) body.set = input.set;
  await mutateJSON<unknown>(`/api/var/${encodeURIComponent(key)}`, 'PUT', body);
}

export async function deleteVariable(key: string): Promise<void> {
  return mutateJSON<void>(`/api/var/${encodeURIComponent(key)}`, 'DELETE');
}

export async function fetchVariableSets(): Promise<VariableSet[]> {
  return fetchJSON<VariableSet[]>('/api/var/sets');
}

export async function createVariableSet(name: string): Promise<void> {
  await mutateJSON<unknown>('/api/var/sets', 'POST', { name });
}

export async function deleteVariableSet(name: string): Promise<void> {
  return mutateJSON<void>(`/api/var/sets/${encodeURIComponent(name)}`, 'DELETE');
}

export async function assignVariableToSet(key: string, set: string): Promise<void> {
  await mutateJSON<unknown>(`/api/var/${encodeURIComponent(key)}/set`, 'PUT', { set });
}

// === Variable Store Encryption API ===

export interface VariableStoreStatus {
  locked: boolean;
  encrypted: boolean;
  variables_count?: number;
  sets_count?: number;
}

export async function fetchVariableStoreStatus(): Promise<VariableStoreStatus> {
  return fetchJSON<VariableStoreStatus>('/api/var/status');
}

export async function unlockVariableStore(passphrase: string): Promise<{ status: string }> {
  return mutateJSON<{ status: string }>('/api/var/unlock', 'POST', { passphrase });
}

export async function lockVariableStore(passphrase: string): Promise<{ status: string }> {
  return mutateJSON<{ status: string }>('/api/var/lock', 'POST', { passphrase });
}

export interface ImportVariableInput {
  key: string;
  value: string;
  type: VariableType;
  isSecret: boolean;
  set?: string;
}

export interface ImportVariablesResult {
  imported: number;
}

// importVariables bulk-imports entries via POST /api/var/import using the
// modern `{ variables: [...] }` shape. The server overwrites by key —
// callers must filter out conflicts they want to preserve before calling.
export async function importVariables(
  vars: ImportVariableInput[],
): Promise<ImportVariablesResult> {
  const body = {
    variables: vars.map((v) => ({
      key: v.key,
      value: v.value,
      type: v.type,
      is_secret: v.isSecret,
      ...(v.set ? { set: v.set } : {}),
    })),
  };
  return mutateJSON<ImportVariablesResult>('/api/var/import', 'POST', body);
}

// === Stack Spec API ===

/**
 * Validate a stack YAML body
 * POST /api/stack/validate
 */
export async function validateStackSpec(yamlContent: string): Promise<ValidationResult> {
  const response = await fetch(`${API_BASE}/api/stack/validate`, {
    method: 'POST',
    headers: buildHeaders({ 'Content-Type': 'application/x-yaml' }),
    body: yamlContent,
  });

  if (response.status === 401) throw new AuthError('Authentication required');

  return response.json();
}

/**
 * Append a resource to the current stack.yaml
 * POST /api/stack/append
 */
export async function appendToStack(yaml: string, resourceType: string): Promise<{ success: boolean; resourceType: string; resourceName: string }> {
  const response = await fetch(`${API_BASE}/api/stack/append`, {
    method: 'POST',
    headers: buildHeaders({ 'Content-Type': 'application/json' }),
    body: JSON.stringify({ yaml, resourceType }),
  });

  if (response.status === 401) throw new AuthError('Authentication required');

  const data = await response.json();
  if (!response.ok) {
    throw new Error(data.error || `Deploy failed: ${response.status}`);
  }

  return data;
}

/**
 * Save a stack spec to the library (~/.gridctl/stacks/<name>.yaml)
 * POST /api/stacks
 */
export async function saveStack(yaml: string, name: string): Promise<{ success: boolean; path: string; name: string }> {
  const response = await fetch(`${API_BASE}/api/stacks`, {
    method: 'POST',
    headers: buildHeaders({ 'Content-Type': 'application/json' }),
    body: JSON.stringify({ yaml, name }),
  });

  if (response.status === 401) throw new AuthError('Authentication required');

  const data = await response.json();
  if (!response.ok) {
    throw new Error(data.error || `Save failed: ${response.status}`);
  }

  return data;
}

/**
 * Cold-load a saved stack into the running daemon.
 * Returns 409 if a stack is already active — callers must check for this.
 * POST /api/stack/initialize
 */
export class StackAlreadyActiveError extends Error {
  constructor() {
    super('Stack already active');
    this.name = 'StackAlreadyActiveError';
  }
}

export async function initializeStack(name: string): Promise<{ success: boolean; name: string }> {
  const response = await fetch(`${API_BASE}/api/stack/initialize`, {
    method: 'POST',
    headers: buildHeaders({ 'Content-Type': 'application/json' }),
    body: JSON.stringify({ name }),
  });

  if (response.status === 401) throw new AuthError('Authentication required');
  if (response.status === 409) throw new StackAlreadyActiveError();

  const data = await response.json();
  if (!response.ok) {
    throw new Error(data.error || `Initialize failed: ${response.status}`);
  }

  return data;
}

/**
 * Get spec plan diff (spec vs running state)
 * GET /api/stack/plan
 */
export async function fetchStackPlan(): Promise<PlanDiff> {
  return fetchJSON<PlanDiff>('/api/stack/plan');
}

/**
 * Get aggregate spec health
 * GET /api/stack/health
 */
export async function fetchStackHealth(): Promise<SpecHealth> {
  return fetchJSON<SpecHealth>('/api/stack/health');
}

/**
 * Get current stack.yaml content
 * GET /api/stack/spec
 */
export async function fetchStackSpec(): Promise<StackSpec> {
  return fetchJSON<StackSpec>('/api/stack/spec');
}

// === Stack Export & Canvas APIs ===

/**
 * Export stack spec from running state
 * GET /api/stack/export
 */
export async function fetchStackExport(): Promise<{ content: string; format: string }> {
  return fetchJSON<{ content: string; format: string }>('/api/stack/export');
}

/**
 * Get available stack recipes
 * GET /api/stack/recipes
 */
export interface StackRecipe {
  id: string;
  name: string;
  description: string;
  category: string;
  spec: string;
}

export async function fetchStackRecipes(): Promise<StackRecipe[]> {
  return fetchJSON<StackRecipe[]>('/api/stack/recipes');
}

// === Limits API ===

/** One rate limit's configuration snapshot. Mirrors pkg/limits RateStatus. */
export interface LimitRateStatus {
  calls_per_minute: number;
  burst: number;
}

export type LimitState = 'ok' | 'warn' | 'exceeded';

/**
 * One limit's snapshot. Mirrors pkg/limits EntryStatus. The UI surfaces rate
 * entries only; `kind` remains in the payload so budget entries from an older
 * backend are filtered out rather than misread.
 */
export interface LimitEntry {
  kind: 'budget' | 'rate';
  scope: 'client' | 'server' | 'tool';
  key: string;
  state: LimitState;
  rate?: LimitRateStatus;
}

export interface LimitsReport {
  configured: boolean;
  entries: LimitEntry[];
}

/**
 * Get the state of every configured rate limit.
 * GET /api/limits
 */
export async function fetchLimits(): Promise<LimitsReport> {
  return fetchJSON<LimitsReport>('/api/limits');
}

// === Tool Groups API ===

/**
 * One member tool as exposed by a group: post-rewrite name, description,
 * and merged annotation hints, with the canonical origin. Mirrors
 * pkg/mcp GroupToolStatus.
 */
export interface GroupToolStatus {
  name: string;
  canonical: string;
  description?: string;
  annotations?: {
    title?: string;
    readOnlyHint?: boolean;
    destructiveHint?: boolean;
    idempotentHint?: boolean;
    openWorldHint?: boolean;
  };
  renamed?: boolean;
  rewritten?: boolean;
}

/** One group's resolved snapshot. Mirrors pkg/mcp GroupStatus. */
export interface GroupStatus {
  name: string;
  description?: string;
  endpoint: string;
  member_count: number;
  tools: string[];
  members: GroupToolStatus[];
  overrides?: Record<string, string>;
}

export interface GroupsReport {
  configured: boolean;
  groups: GroupStatus[];
}

/**
 * Get every tool group resolved against the live tool surface.
 * GET /api/groups
 */
export async function fetchGroups(): Promise<GroupsReport> {
  return fetchJSON<GroupsReport>('/api/groups');
}

// === Server Catalog API ===

/**
 * One installable server from the catalog: the embedded curated set plus
 * MCP Registry search results. Mirrors pkg/catalog.Entry.
 */
export interface CatalogInput {
  name: string;
  description?: string;
  required?: boolean;
  secret?: boolean;
  arg?: boolean;
  auth?: boolean;
  default?: string;
  placeholder?: string;
  choices?: string[];
  format?: string;
}

export interface CatalogInstall {
  type: 'image' | 'command' | 'url';
  transport: string;
  image?: string;
  port?: number;
  command?: string[];
  url?: string;
  auth_type?: string;
  auth_header?: string;
}

export interface CatalogEntry {
  name: string;
  title?: string;
  description: string;
  tier?: 'curated' | 'registry';
  namespace?: string;
  homepage?: string;
  repository?: string;
  status?: string;
  install: CatalogInstall;
  inputs?: CatalogInput[];
  unsupported?: string;
}

export interface CatalogResponse {
  query: string;
  source: string;
  stale?: boolean;
  registry_error?: string;
  servers: CatalogEntry[];
}

/**
 * Search the server catalog. An empty query lists the curated set only;
 * with a query the MCP Registry is merged in after curated results.
 * GET /api/catalog
 */
export async function fetchCatalog(query = '', source = 'all'): Promise<CatalogResponse> {
  const params = new URLSearchParams();
  if (query) params.set('q', query);
  if (source !== 'all') params.set('source', source);
  const qs = params.toString();
  return fetchJSON<CatalogResponse>(`/api/catalog${qs ? `?${qs}` : ''}`);
}

// === Wizard Draft API ===

export interface WizardDraft {
  id: string;
  name: string;
  resourceType: string;
  formData: Record<string, unknown>;
  createdAt: string;
  updatedAt: string;
}

/**
 * List saved wizard drafts
 * GET /api/wizard/drafts
 */
export async function fetchWizardDrafts(): Promise<WizardDraft[]> {
  return fetchJSON<WizardDraft[]>('/api/wizard/drafts');
}

/**
 * Save a new wizard draft
 * POST /api/wizard/drafts
 */
export async function saveWizardDraft(draft: {
  name: string;
  resourceType: string;
  formData: Record<string, unknown>;
}): Promise<WizardDraft> {
  return mutateJSON<WizardDraft>('/api/wizard/drafts', 'POST', draft);
}

/**
 * Delete a wizard draft
 * DELETE /api/wizard/drafts/{id}
 */
export async function deleteWizardDraft(id: string): Promise<void> {
  return mutateJSON<void>(`/api/wizard/drafts/${encodeURIComponent(id)}`, 'DELETE');
}

// === Skills Source API ===

/**
 * List all configured skill sources with update status
 * GET /api/skills/sources
 */
export async function fetchSkillSources(): Promise<SkillSourceStatus[]> {
  return fetchJSON<SkillSourceStatus[]>('/api/skills/sources');
}

/**
 * Add a new skill source (triggers clone + import)
 * POST /api/skills/sources
 */
export async function addSkillSource(source: {
  repo: string;
  ref?: string;
  path?: string;
  trust?: boolean;
  noActivate?: boolean;
  selected?: string[];
  /** Agent names to import. Required alongside `selected`: a skill selection
   *  alone skips agents (legacy importer contract). */
  selectedAgents?: string[];
  auth?: SkillAuth;
}): Promise<ImportResult> {
  return mutateJSON<ImportResult>('/api/skills/sources', 'POST', source);
}

/**
 * Remove a skill source and its imported skills
 * DELETE /api/skills/sources/{name}
 */
export async function removeSkillSource(name: string): Promise<{ removed: string[]; source: string }> {
  return mutateJSON<{ removed: string[]; source: string }>(
    `/api/skills/sources/${encodeURIComponent(name)}`,
    'DELETE',
  );
}

/**
 * Trigger update check for a source
 * POST /api/skills/sources/{name}/check
 */
export async function checkSkillSource(name: string): Promise<SourceUpdateCheck> {
  return mutateJSON<SourceUpdateCheck>(
    `/api/skills/sources/${encodeURIComponent(name)}/check`,
    'POST',
  );
}

/**
 * Apply available updates for a source. Without `force`, locally-edited
 * (drifted) skills are skipped and reported as `skipped: "local edits"`; with
 * `force` they are overwritten (the server writes a SKILL.md.pre-<sha> backup
 * first). An optional `skills` filter restricts the operation to named skills.
 * POST /api/skills/sources/{name}/update
 */
export async function updateSkillSource(
  name: string,
  opts?: { force?: boolean; skills?: string[] },
): Promise<{ source: string; results: SkillSyncResult[] }> {
  return mutateJSON<{ source: string; results: SkillSyncResult[] }>(
    `/api/skills/sources/${encodeURIComponent(name)}/update`,
    'POST',
    opts && (opts.force || opts.skills?.length) ? opts : undefined,
  );
}

/**
 * Sync every imported source in one server-side fan-out. Pinned sources
 * (refs shaped like v1.0.0 or full commit SHAs) are silently skipped. Without
 * `force`, drifted skills are skipped; with `force` they are overwritten. The
 * response carries per-source results plus aggregate counters.
 *
 * POST /api/skills/sources/update
 */
export async function syncAllSources(opts?: { force?: boolean }): Promise<SourceSyncSummary> {
  return mutateJSON<SourceSyncSummary>(
    '/api/skills/sources/update',
    'POST',
    opts?.force ? opts : undefined,
  );
}

/**
 * Compare a tracked skill's on-disk SKILL.md against the latest upstream
 * content. Read-only: nothing is written to disk and no SHAs change.
 * GET /api/skills/sources/{name}/skills/{skill}/diff
 */
export async function fetchSkillDiff(source: string, skill: string): Promise<SkillDiffResponse> {
  return fetchJSON<SkillDiffResponse>(
    `/api/skills/sources/${encodeURIComponent(source)}/skills/${encodeURIComponent(skill)}/diff`,
  );
}

/**
 * Detach a skill from its source: removes the origin sidecar and lock entry so
 * the skill becomes local-only and is no longer touched by sync.
 * POST /api/skills/sources/{name}/skills/{skill}/detach
 */
export async function detachSkill(source: string, skill: string): Promise<{ detached: string }> {
  return mutateJSON<{ detached: string }>(
    `/api/skills/sources/${encodeURIComponent(source)}/skills/${encodeURIComponent(skill)}/detach`,
    'POST',
  );
}

/**
 * Reset a single skill to its upstream content. The server backs up the
 * current (possibly edited) SKILL.md before force-restoring it.
 * POST /api/skills/sources/{name}/skills/{skill}/reset
 */
export async function resetSkill(source: string, skill: string): Promise<SkillSyncResult> {
  return mutateJSON<SkillSyncResult>(
    `/api/skills/sources/${encodeURIComponent(source)}/skills/${encodeURIComponent(skill)}/reset`,
    'POST',
  );
}

/**
 * Preview skills in a source without importing.
 *
 * Posts the request body (rather than query params) so optional auth
 * credentials never surface in URLs, logs, or browser history.
 * POST /api/skills/sources/{name}/preview
 */
export async function previewSkillSource(
  name: string,
  params?: { repo?: string; ref?: string; path?: string; auth?: SkillAuth },
): Promise<SkillPreviewResponse> {
  return mutateJSON<SkillPreviewResponse>(
    `/api/skills/sources/${encodeURIComponent(name)}/preview`,
    'POST',
    params ?? {},
  );
}

/**
 * Get pending update summary across all sources
 * GET /api/skills/updates
 */
export async function fetchSkillUpdates(): Promise<UpdateSummary> {
  return fetchJSON<UpdateSummary>('/api/skills/updates');
}

// === Traces API ===

export interface TraceSummary {
  traceId: string;
  rootSpanId: string;
  operation: string;
  /** Bare tool name (client-requested until routing resolves it); empty for
   *  non-tool-call traces. */
  tool: string;
  /** Connecting client name; empty when the client did not identify itself. */
  client: string;
  server: string;
  startTime: string;
  duration: number;
  spanCount: number;
  hasError: boolean;
  status: 'ok' | 'error';
}

export interface TraceListResponse {
  traces: TraceSummary[];
  total: number;
  /** False when the gateway has no trace buffer (gateway.tracing disabled). */
  tracingEnabled: boolean;
  /** Traces currently in the ring buffer. */
  bufferSize: number;
  /** Ring buffer capacity (gateway.tracing.max_traces). */
  bufferCapacity: number;
}

export interface SpanEvent {
  name: string;
  timestamp: string;
  attributes: Record<string, string>;
}

export interface Span {
  spanId: string;
  /** Empty string for root spans. */
  parentSpanId: string;
  name: string;
  startTime: string;
  /** Absent for spans persisted before endTime was serialized; derive from startTime + duration. */
  endTime?: string;
  /** Milliseconds. */
  duration: number;
  status: 'ok' | 'error';
  attributes: Record<string, string>;
  events: SpanEvent[];
}

export interface TraceDetail {
  traceId: string;
  spans: Span[];
}

/**
 * Fetch list of recent traces with optional filters
 * GET /api/traces
 */
export async function fetchTraces(params?: {
  server?: string;
  errors?: boolean;
  minDuration?: number;
  limit?: number;
}): Promise<TraceListResponse> {
  const query = new URLSearchParams();
  if (params?.server) query.set('server', params.server);
  if (params?.errors) query.set('errors', 'true');
  // The API expects a unit; the store keeps a bare millisecond number.
  if (params?.minDuration != null) query.set('minDuration', `${params.minDuration}ms`);
  if (params?.limit != null) query.set('limit', String(params.limit));
  const qs = query.toString();
  return fetchJSON<TraceListResponse>(`/api/traces${qs ? `?${qs}` : ''}`);
}

/**
 * Fetch full trace detail including all spans
 * GET /api/traces/{traceId}
 */
export async function fetchTraceDetail(traceId: string): Promise<TraceDetail> {
  return fetchJSON<TraceDetail>(`/api/traces/${encodeURIComponent(traceId)}`);
}

/**
 * Fetch a single trace as an OTLP/JSON TracesData document
 * GET /api/traces/{traceId}/otlp
 */
export async function fetchTraceOTLP(traceId: string): Promise<unknown> {
  return fetchJSON<unknown>(`/api/traces/${encodeURIComponent(traceId)}/otlp`);
}

// === Playground API ===

export interface PlaygroundProviderAuth {
  apiKey: boolean;
  keyName: string | null;
  cliPath: string | null;
}

export interface PlaygroundAuthResponse {
  providers: Record<string, PlaygroundProviderAuth>;
  ollama: { reachable: boolean; endpoint: string };
}

export interface PlaygroundChatRequest {
  agentId?: string;
  message: string;
  sessionId: string;
  authMode: string;
  model?: string;
  ollamaUrl?: string;
}

export interface PlaygroundChatResponse {
  sessionId: string;
  status: string;
}

/**
 * Detect available auth methods for each LLM provider
 * POST /api/playground/auth
 */
export async function fetchPlaygroundAuth(): Promise<PlaygroundAuthResponse> {
  const response = await fetch(`${API_BASE}/api/playground/auth`, {
    method: 'POST',
    headers: buildHeaders(),
  });
  if (response.status === 401) throw new AuthError('Authentication required');
  if (!response.ok) throw new Error(`Auth check failed: ${response.status} ${response.statusText}`);
  return response.json();
}

/**
 * Start a playground inference session
 * POST /api/playground/chat
 */
export async function sendPlaygroundChat(req: PlaygroundChatRequest): Promise<PlaygroundChatResponse> {
  const response = await fetch(`${API_BASE}/api/playground/chat`, {
    method: 'POST',
    headers: buildHeaders({ 'Content-Type': 'application/json' }),
    body: JSON.stringify(req),
  });
  if (response.status === 401) throw new AuthError('Authentication required');
  if (!response.ok) {
    const data = await response.json().catch(() => ({}));
    throw new Error((data as { error?: string }).error || `Chat failed: ${response.status}`);
  }
  return response.json();
}

/**
 * Returns headers needed for streaming fetch (SSE with auth)
 */
export function buildStreamHeaders(): Record<string, string> {
  return buildHeaders();
}

// === Pins API ===

/**
 * One poisoning-scan signal on a tool definition. Advisory only: findings
 * inform the approve decision and never block anything. snippet and decoded
 * quote attacker-controlled text and MUST be rendered through
 * escapeNonPrintable.
 */
export interface PinFinding {
  code: string;
  severity: 'info' | 'warn' | 'critical';
  confidence: 'high' | 'medium' | 'low';
  field: string;
  snippet?: string;
  message: string;
  decoded?: string;
}

export interface PinRecord {
  hash: string;
  name: string;
  description?: string;
  pinned_at: string;
  findings?: PinFinding[];
}

export interface ServerPins {
  server_hash: string;
  pinned_at: string;
  last_verified_at: string;
  tool_count: number;
  status: 'pinned' | 'drift' | 'approved_pending_redeploy';
  tools: Record<string, PinRecord>;
}

/**
 * Fetch pin state for all servers
 * GET /api/pins
 */
export async function fetchServerPins(): Promise<Record<string, ServerPins>> {
  return fetchJSON<Record<string, ServerPins>>('/api/pins');
}

/**
 * Parts of a tool definition that changed, as named in change_kinds.
 * schema_uncaptured marks pins recorded before schema capture: the old
 * schemas are unrecoverable, so the hash move may include a schema change
 * that cannot be shown. It appears alongside 'description' when the prose
 * also moved.
 */
export type PinsChangeKind =
  | 'description'
  | 'input_schema'
  | 'output_schema'
  | 'schema_uncaptured';

export interface PinsToolDiff {
  name: string;
  old_hash: string;
  new_hash: string;
  old_description: string;
  new_description: string;
  // Optional so the UI tolerates a daemon predating the poisoning scanner.
  findings?: PinFinding[];
  // Canonical schema serializations and change kinds. All optional (matching
  // the findings precedent) so the UI tolerates an older daemon, which
  // degrades to the description-only view. old_* are absent for pins
  // recorded before schema capture.
  old_input_schema?: string;
  new_input_schema?: string;
  old_output_schema?: string;
  new_output_schema?: string;
  change_kinds?: PinsChangeKind[];
  // Tool groups whose overrides rewrite this tool's description; those
  // rewrites were written against the old upstream definition.
  groups_rewriting?: string[];
}

export interface PinsDiff {
  server: string;
  status: string;
  // Fingerprint of the live definitions this diff was computed from; pass to
  // approveServerPins to bind the approval to the reviewed snapshot.
  live_server_hash: string;
  modified_tools: PinsToolDiff[];
  new_tools: string[];
  removed_tools: string[];
}

/**
 * Fetch the per-tool delta between pinned and live tool definitions.
 * Computed on demand server-side; never mutates pin state.
 * GET /api/pins/{server}/diff
 */
export async function fetchPinsDiff(serverName: string): Promise<PinsDiff> {
  return fetchJSON<PinsDiff>(`/api/pins/${encodeURIComponent(serverName)}/diff`);
}

/**
 * Approve current tool definitions for a server, clearing drift.
 * When expectedServerHash (from PinsDiff.live_server_hash) is provided, the
 * gateway rejects the approval with 409 if the live definitions changed after
 * the diff was reviewed, so nothing unreviewed can be pinned.
 * POST /api/pins/{server}/approve
 */
export async function approveServerPins(
  serverName: string,
  expectedServerHash?: string,
): Promise<void> {
  const response = await fetch(`${API_BASE}/api/pins/${encodeURIComponent(serverName)}/approve`, {
    method: 'POST',
    headers: buildHeaders(),
    ...(expectedServerHash
      ? { body: JSON.stringify({ expected_server_hash: expectedServerHash }) }
      : {}),
  });
  if (response.status === 401) throw new AuthError('Authentication required');
  if (!response.ok) {
    let message = `API error: ${response.status} ${response.statusText}`;
    try {
      const body = (await response.json()) as { error?: string };
      if (body?.error) message = body.error;
    } catch {
      // Non-JSON error body; keep the status-line message.
    }
    throw new Error(message);
  }
}

/**
 * Delete all pins for a server; it re-pins from scratch on the next verify.
 * Returns 204 with no body, so this cannot use fetchJSON.
 * DELETE /api/pins/{server}
 */
export async function resetServerPins(serverName: string): Promise<void> {
  const response = await fetch(`${API_BASE}/api/pins/${encodeURIComponent(serverName)}`, {
    method: 'DELETE',
    headers: buildHeaders(),
  });
  if (response.status === 401) throw new AuthError('Authentication required');
  if (!response.ok) {
    let message = `API error: ${response.status} ${response.statusText}`;
    try {
      const body = (await response.json()) as { error?: string };
      if (body?.error) message = body.error;
    } catch {
      // Non-JSON error body; keep the status-line message.
    }
    throw new Error(message);
  }
}

// === Skill Pins API ===

/** One supporting file's content digest inside a skill pin. */
export interface SkillFileDigest {
  path: string;
  digest: string;
}

/**
 * TOFU content pin for one registry skill: per-file digests over the whole
 * document set. status has only two values (no approved_pending_redeploy:
 * skill approvals take effect immediately, there is no redeploy step).
 * Findings are advisory, same contract as PinFinding.
 */
export interface SkillPin {
  skill_hash: string;
  files?: SkillFileDigest[];
  document?: string;
  source?: 'local' | 'git';
  // commitSha is camelCase by design: the field mirrors the .origin.json
  // sidecar record byte-for-byte.
  origin?: { repo?: string; ref?: string; commitSha?: string };
  approved_reason?: string;
  pinned_at: string;
  last_verified_at: string;
  status: 'pinned' | 'drift';
  findings?: PinFinding[];
}

/**
 * What changed since a skill was pinned. composite_hash fingerprints the
 * CURRENT content this diff was computed from; pass it to approveSkillPin so
 * the approval is bound to the reviewed snapshot. Documents are the pinned
 * and current canonical SKILL.md renderings (absent when unchanged).
 */
export interface SkillPinsDiff {
  skill: string;
  status: 'pinned' | 'drift';
  composite_hash: string;
  old_document?: string;
  new_document?: string;
  added_files: string[];
  removed_files: string[];
  modified_files: string[];
  findings: PinFinding[];
}

/**
 * Fetch pin state for all skills, keyed by skill name.
 * GET /api/skill-pins
 */
export async function fetchSkillPins(): Promise<Record<string, SkillPin>> {
  return fetchJSON<Record<string, SkillPin>>('/api/skill-pins');
}

/**
 * Fetch what changed since a skill was pinned. Never mutates pin state.
 * GET /api/skill-pins/{name}/diff
 */
export async function fetchSkillPinDiff(name: string): Promise<SkillPinsDiff> {
  return fetchJSON<SkillPinsDiff>(`/api/skill-pins/${encodeURIComponent(name)}/diff`);
}

/**
 * Approve a skill's current content, clearing pin drift. The gateway rejects
 * with 409 when the content changed after the reviewed diff (stale
 * expectedHash) and with 400 when the content carries unresolved advisory
 * findings and no reason is given — the UI branches on those two statuses,
 * so this throws HTTPError rather than a bare Error.
 * POST /api/skill-pins/{name}/approve
 */
export async function approveSkillPin(
  name: string,
  expectedHash: string,
  reason?: string,
): Promise<void> {
  const response = await fetch(`${API_BASE}/api/skill-pins/${encodeURIComponent(name)}/approve`, {
    method: 'POST',
    headers: buildHeaders(),
    body: JSON.stringify({ expected_hash: expectedHash, reason: reason ?? '' }),
  });
  if (response.status === 401) throw new AuthError('Authentication required');
  if (!response.ok) {
    let message = `API error: ${response.status} ${response.statusText}`;
    try {
      const body = (await response.json()) as { error?: string };
      if (body?.error) message = body.error;
    } catch {
      // Non-JSON error body; keep the status-line message.
    }
    throw new HTTPError(response.status, message);
  }
}

/**
 * Delete a skill's pin record; it re-pins fresh on the next registry refresh.
 * Returns 204 with no body, so this cannot use fetchJSON.
 * DELETE /api/skill-pins/{name}
 */
export async function resetSkillPin(name: string): Promise<void> {
  const response = await fetch(`${API_BASE}/api/skill-pins/${encodeURIComponent(name)}`, {
    method: 'DELETE',
    headers: buildHeaders(),
  });
  if (response.status === 401) throw new AuthError('Authentication required');
  if (!response.ok) {
    let message = `API error: ${response.status} ${response.statusText}`;
    try {
      const body = (await response.json()) as { error?: string };
      if (body?.error) message = body.error;
    } catch {
      // Non-JSON error body; keep the status-line message.
    }
    throw new Error(message);
  }
}

// === JSON-RPC Helper (for MCP protocol calls) ===

interface JSONRPCRequest {
  jsonrpc: '2.0';
  id: number | string;
  method: string;
  params?: unknown;
}

interface JSONRPCResponse<T = unknown> {
  jsonrpc: '2.0';
  id: number | string;
  result?: T;
  error?: {
    code: number;
    message: string;
    data?: unknown;
  };
}

// === Server Probe API ===

// Wire shape accepted by POST /api/servers/probe. Mirrors the subset of
// config.MCPServer relevant to tool discovery — snake_case fields match the
// stack YAML schema.
export interface ProbeServerConfig {
  name?: string;
  image?: string;
  url?: string;
  port?: number;
  transport?: string;
  command?: string[];
  env?: Record<string, string>;
  build_args?: Record<string, string>;
  ssh?: { host: string; user: string; port?: number; identity_file?: string };
  openapi?: { spec: string };
  ready_timeout?: string;
  auth?: ProbeServerAuth;
}

// Downstream auth block for external URL probes. Snake_case matches the
// stack YAML schema (config.ServerAuth) like the rest of the probe wire.
export interface ProbeServerAuth {
  type: string;
  token?: string;
  header?: string;
  value?: string;
  scopes?: string[];
  client_id?: string;
  client_secret?: string;
}

export interface ProbedTool {
  name: string;
  description?: string;
  inputSchema: unknown;
  outputSchema?: unknown;
  findings?: PinFinding[];
}

export interface ProbeSuccess {
  tools: ProbedTool[];
  probedAt: string;
  cached: boolean;
}

// ProbeError exposes the structured error payload returned by the backend so
// the UI can render stable copy per `code`.
export class ProbeError extends Error {
  code: string;
  hint?: string;
  httpStatus: number;

  constructor(code: string, message: string, hint: string | undefined, httpStatus: number) {
    super(message);
    this.name = 'ProbeError';
    this.code = code;
    this.hint = hint;
    this.httpStatus = httpStatus;
  }
}

/**
 * Ephemerally probe an MCP server to enumerate its tools before deploying it.
 * The backend spawns the server (when applicable), runs the MCP initialize +
 * tools/list handshake, tears down, and caches the result for 5 minutes.
 *
 * Rejects with ProbeError on structured failures (422 / 400), AuthError on
 * 401, or a plain Error for transport issues.
 * POST /api/servers/probe
 */
export async function probeServer(config: ProbeServerConfig, sessionId?: string): Promise<ProbeSuccess> {
  const headers = buildHeaders({ 'Content-Type': 'application/json' });
  if (sessionId) headers['X-Session-ID'] = sessionId;
  const response = await fetch(`${API_BASE}/api/servers/probe`, {
    method: 'POST',
    headers,
    body: JSON.stringify(config),
  });

  if (response.status === 401) throw new AuthError('Authentication required');

  const data = await response.json().catch(() => null);

  if (!response.ok) {
    const err = data?.error;
    if (err && typeof err.code === 'string') {
      throw new ProbeError(err.code, err.message ?? 'Probe failed', err.hint, response.status);
    }
    throw new Error(`Probe failed: ${response.status} ${response.statusText}`);
  }

  return data as ProbeSuccess;
}

// === OpenAPI Operations Preview ===

// Wire shape accepted by POST /api/openapi/operations. There is deliberately no
// auth block: specs are fetched unauthenticated on the deployed path too, so
// sending credentials here would imply a capability the gateway does not have.
export interface OpenAPIPreviewRequest {
  spec: string;
  tls?: {
    certFile?: string;
    keyFile?: string;
    caFile?: string;
    insecureSkipVerify?: boolean;
  };
}

// One operation row from the preview endpoint.
//
// operation_id and tool_name are both present on purpose: the
// openapi.operations include/exclude filter matches operation_id (the raw spec
// value), while tool_name is the sanitized identifier the model actually sees.
// They differ whenever an operationId contains characters outside
// [a-zA-Z0-9_-], so anything persisted into stack.yaml must use operation_id.
export interface OpenAPIOperation {
  operation_id: string;
  tool_name: string;
  method: string;
  path: string;
  summary?: string;
  description?: string;
  tags?: string[];
  deprecated?: boolean;
  // Skipped operations cannot become tools at all (no operationId, or a
  // sanitized name that comes out empty). They are reported rather than
  // dropped so the picker's counts match what deploy will actually produce.
  skipped?: boolean;
  skip_reason?: string;
}

export interface OpenAPIPreviewSuccess {
  title?: string;
  version?: string;
  operations: OpenAPIOperation[];
  skipped_count: number;
  loaded_at: string;
  cached: boolean;
}

/**
 * Parse an OpenAPI spec and enumerate its operations without deploying
 * anything. Backs the wizard's "Load operations" button.
 *
 * Shares the probe's error envelope, so failures reject with ProbeError
 * carrying a stable `code` (invalid_request, needs_auth, fetch_failed,
 * parse_failed, rate_limited, internal), AuthError on 401, or a plain Error
 * for transport issues.
 * POST /api/openapi/operations
 */
export async function previewOpenAPIOperations(
  request: OpenAPIPreviewRequest,
  sessionId?: string,
  signal?: AbortSignal,
): Promise<OpenAPIPreviewSuccess> {
  const headers = buildHeaders({ 'Content-Type': 'application/json' });
  if (sessionId) headers['X-Session-ID'] = sessionId;
  const response = await fetch(`${API_BASE}/api/openapi/operations`, {
    method: 'POST',
    headers,
    body: JSON.stringify(request),
    signal,
  });

  if (response.status === 401) throw new AuthError('Authentication required');

  const data = await response.json().catch(() => null);

  if (!response.ok) {
    const err = data?.error;
    if (err && typeof err.code === 'string') {
      throw new ProbeError(err.code, err.message ?? 'Loading operations failed', err.hint, response.status);
    }
    throw new Error(`Loading operations failed: ${response.status} ${response.statusText}`);
  }

  return data as OpenAPIPreviewSuccess;
}

// === Downstream Server Authorization (OAuth brokering) ===

/**
 * Per-server downstream authorization state for every OAuth-configured
 * server. GET /api/auth/servers
 */
export async function fetchAuthServers(): Promise<ServerAuthInfo[]> {
  return fetchJSON<ServerAuthInfo[]>('/api/auth/servers');
}

/**
 * Start the OAuth authorization flow for a server. Returns the URL the
 * browser must open plus the single-use state token that keys the flow.
 * POST /api/servers/{name}/auth/login
 */
export async function beginServerAuthorization(server: string): Promise<ServerAuthLogin> {
  const response = await fetch(`${API_BASE}/api/servers/${encodeURIComponent(server)}/auth/login`, {
    method: 'POST',
    headers: buildHeaders({ 'Content-Type': 'application/json' }),
    body: '{}',
  });
  if (response.status === 401) throw new AuthError('Authentication required');
  const data = await response.json().catch(() => null);
  if (!response.ok) {
    throw new Error(data?.error ?? `Authorization start failed: ${response.status}`);
  }
  return data as ServerAuthLogin;
}

/**
 * Block until the authorization flow keyed by state completes, fails, or
 * times out. The backend long-polls; resolve means authorized. An optional
 * AbortSignal cancels the long-poll (rejects with an AbortError DOMException)
 * when the user cancels or the component unmounts.
 * GET /api/servers/{name}/auth/wait?state=...
 */
export async function waitServerAuthorization(
  server: string,
  state: string,
  signal?: AbortSignal,
): Promise<void> {
  const response = await fetch(
    `${API_BASE}/api/servers/${encodeURIComponent(server)}/auth/wait?state=${encodeURIComponent(state)}`,
    { headers: buildHeaders(), signal },
  );
  if (response.status === 401) throw new AuthError('Authentication required');
  if (!response.ok) {
    const data = await response.json().catch(() => null);
    throw new Error(data?.error ?? `Authorization failed: ${response.status}`);
  }
}

/**
 * Revoke (best effort) and delete a server's stored authorization.
 * POST /api/servers/{name}/auth/logout
 */
export async function logoutServerAuthorization(server: string): Promise<void> {
  const response = await fetch(`${API_BASE}/api/servers/${encodeURIComponent(server)}/auth/logout`, {
    method: 'POST',
    headers: buildHeaders(),
  });
  if (response.status === 401) throw new AuthError('Authentication required');
  if (!response.ok) {
    const data = await response.json().catch(() => null);
    throw new Error(data?.error ?? `Sign out failed: ${response.status}`);
  }
}

let requestId = 0;

export async function mcpRequest<T>(
  method: string,
  params?: unknown
): Promise<T> {
  const request: JSONRPCRequest = {
    jsonrpc: '2.0',
    id: ++requestId,
    method,
    params,
  };

  const response = await fetch(`${API_BASE}/mcp`, {
    method: 'POST',
    headers: buildHeaders({ 'Content-Type': 'application/json' }),
    body: JSON.stringify(request),
  });

  if (response.status === 401) {
    throw new AuthError('Authentication required');
  }

  const result = await response.json() as JSONRPCResponse<T>;

  if (result.error) {
    throw new Error(`MCP error ${result.error.code}: ${result.error.message}`);
  }

  return result.result as T;
}

// === Telemetry Persistence (Phase 4) ===

/**
 * StackModifiedError surfaces the structured 409 envelope from the
 * telemetry PATCH endpoints when the on-disk YAML changed between the
 * handler reading it and the atomic write. Callers should toast the hint
 * ("Reload the file to see the latest contents") and offer a refresh.
 */
export class StackModifiedError extends Error {
  code: string;
  hint?: string;
  constructor(message: string, hint?: string) {
    super(message);
    this.name = 'StackModifiedError';
    this.code = 'stack_modified';
    this.hint = hint;
  }
}

export interface UpdateStackTelemetryBody {
  persist?: {
    logs?: boolean | null;
    metrics?: boolean | null;
    traces?: boolean | null;
  };
  retention?: {
    max_size_mb?: number;
    max_backups?: number;
    max_age_days?: number;
  };
}

// Per-server PATCH body. Values are: undefined = no change, null = clear
// override (revert to inherit), bool = set explicit override. The whole
// `persist` field set to null deletes the entire telemetry block from the
// server entry — matching the "clear all overrides" idiom.
export interface UpdateServerTelemetryBody {
  persist?: {
    logs?: boolean | null;
    metrics?: boolean | null;
    traces?: boolean | null;
  } | null;
}

export interface WipeTelemetryOpts {
  server?: string;
  signal?: 'logs' | 'metrics' | 'traces';
}

async function telemetryMutate<T>(
  url: string,
  init: RequestInit,
): Promise<T> {
  const response = await fetch(`${API_BASE}${url}`, {
    ...init,
    headers: { ...buildHeaders({ 'Content-Type': 'application/json' }), ...(init.headers || {}) },
  });
  if (response.status === 401) throw new AuthError('Authentication required');

  // The body is JSON for both success and structured-error responses.
  // For 409 the server returns {error: {code, message, hint}}; for 422
  // it returns {error, validation}; otherwise plain {error: "..."}.
  const data = await response.json().catch(() => null);
  if (!response.ok) {
    const err = (data as { error?: unknown } | null)?.error;
    if (response.status === 409 && err && typeof err === 'object' && (err as { code?: string }).code === 'stack_modified') {
      const e = err as { message?: string; hint?: string };
      throw new StackModifiedError(
        e.message ?? 'The stack file was modified outside the canvas.',
        e.hint ?? 'Reload the file to see the latest contents, then re-apply your changes.',
      );
    }
    const msg = typeof err === 'string' ? err : `Telemetry request failed: ${response.status}`;
    throw new HTTPError(response.status, msg);
  }
  return data as T;
}

/**
 * Update the stack-global telemetry block. Returns the refreshed inventory
 * snapshot alongside the success flag so callers can update the store
 * without a follow-up GET.
 * PATCH /api/stack/telemetry
 */
export async function updateStackTelemetry(
  body: UpdateStackTelemetryBody,
): Promise<TelemetryMutationResponse> {
  return telemetryMutate<TelemetryMutationResponse>('/api/stack/telemetry', {
    method: 'PATCH',
    body: JSON.stringify(body),
  });
}

/**
 * Update per-server telemetry overrides. `body.persist === null` clears
 * the entire per-server telemetry block; per-signal `null` clears that
 * single override; bool sets an explicit override.
 * PATCH /api/mcp-servers/{name}/telemetry
 */
export async function updateServerTelemetry(
  name: string,
  body: UpdateServerTelemetryBody,
): Promise<TelemetryMutationResponse> {
  return telemetryMutate<TelemetryMutationResponse>(
    `/api/mcp-servers/${encodeURIComponent(name)}/telemetry`,
    {
      method: 'PATCH',
      body: JSON.stringify(body),
    },
  );
}

/**
 * Fetch the current on-disk telemetry inventory. Returns one record per
 * (server, signal) pair where at least one file exists.
 * GET /api/telemetry/inventory
 */
export async function getTelemetryInventory(): Promise<InventoryRecord[]> {
  return fetchJSON<InventoryRecord[]>('/api/telemetry/inventory');
}

/**
 * Wipe persisted telemetry files. Empty server/signal acts as a wildcard;
 * passing neither wipes everything for the active stack.
 * DELETE /api/telemetry?server=&signal=
 */
export async function wipeTelemetry(
  opts: WipeTelemetryOpts = {},
): Promise<TelemetryMutationResponse> {
  const params = new URLSearchParams();
  if (opts.server) params.set('server', opts.server);
  if (opts.signal) params.set('signal', opts.signal);
  const query = params.toString();
  const url = query ? `/api/telemetry?${query}` : '/api/telemetry';
  return telemetryMutate<TelemetryMutationResponse>(url, { method: 'DELETE' });
}

// Re-export the types used in mutation arguments so callers do not need to
// reach into the types module separately.
export type { TelemetryPersistDefaults, TelemetryRetention };

// === Global Context API ===
// Wire format is snake_case, mirroring pkg/contexts JSON tags.

export type ContextState =
  | 'unsupported'
  | 'never-synced'
  | 'in-sync'
  | 'stale'
  | 'drifted'
  | 'target-missing';

/** How a client receives the context; absent while fragments mode is off. */
export type ContextMode = 'single-file' | 'multi-file' | 'compiled';

/** One out-of-sync fragment on a multi-file client. */
export interface ContextFragmentStatus {
  name: string;
  state: ContextState;
  /** Pack that applied this fragment projection; absent outside packs. */
  pack?: string;
}

export interface ContextClientStatus {
  slug: string;
  name: string;
  supported: boolean;
  available: boolean;
  /** Path rests on unofficial sourcing rather than published client docs. */
  unofficial?: boolean;
  strategy?: string;
  mode?: ContextMode;
  target_path?: string;
  state: ContextState;
  detail?: string;
  synced_at?: string;
  /** Every non-synced fragment with its own state; multi-file clients
   *  only, omitted when all fragments are in sync. */
  fragments?: ContextFragmentStatus[];
}

export interface ContextDoc {
  canonical: { path: string; exists: boolean; content: string };
  fragments_active?: boolean;
  needs_sync: boolean;
  clients: ContextClientStatus[];
}

/** One rule fragment in composition (filename-lexicographic) order. */
export interface ContextFragment {
  name: string;
  description?: string;
  paths?: string[];
  content: string;
  bytes: number;
  position: number;
}

export interface ContextScanEntry {
  slug: string;
  name: string;
  path: string;
  exists: boolean;
  size: number;
}

export interface ContextSyncResult {
  slug: string;
  name: string;
  strategy: string;
  /** Set on per-fragment rows from multi-file targets in fragments mode. */
  fragment?: string;
  target_path: string;
  action: string;
  backup_path?: string;
  diff?: string;
  error?: string;
}

export interface ContextSyncResponse {
  dry_run: boolean;
  has_failures: boolean;
  results: ContextSyncResult[];
}

export interface ContextInitRequest {
  source: 'template' | 'client' | 'file';
  client?: string;
  path?: string;
  force?: boolean;
}

/** Canonical content plus per-client sync state. GET /api/context */
export async function fetchGlobalContext(): Promise<ContextDoc> {
  return fetchJSON<ContextDoc>('/api/context');
}

/** Save the canonical content. PUT /api/context */
export async function saveGlobalContext(content: string): Promise<ContextDoc> {
  return mutateJSON<ContextDoc>('/api/context', 'PUT', { content });
}

/** What exists at each client's likely global context path. GET /api/context/scan */
export async function scanGlobalContext(): Promise<ContextScanEntry[]> {
  const body = await fetchJSON<{ entries: ContextScanEntry[] | null }>('/api/context/scan');
  return body.entries ?? [];
}

/** Bootstrap the canonical file from a chosen source. POST /api/context/init */
export async function initGlobalContext(req: ContextInitRequest): Promise<ContextDoc> {
  return mutateJSON<ContextDoc>('/api/context/init', 'POST', req);
}

/** Sync the canonical context to clients (all when clients is omitted). POST /api/context/sync */
export async function syncGlobalContext(opts?: {
  clients?: string[];
  force?: boolean;
  dryRun?: boolean;
}): Promise<ContextSyncResponse> {
  return mutateJSON<ContextSyncResponse>('/api/context/sync', 'POST', {
    clients: opts?.clients,
    force: opts?.force,
    dry_run: opts?.dryRun,
  });
}

/**
 * Pull a client's managed content back into the canon.
 * POST /api/context/adopt/{slug}. In fragments mode, `fragment` adopts one
 * projected file on an identity multi-file target, and `into` captures a
 * compiled target's edited body into the named fragment. No opts keeps the
 * whole-client behavior.
 */
export async function adoptGlobalContext(
  slug: string,
  opts?: { fragment?: string; into?: string },
): Promise<ContextDoc> {
  const url = `/api/context/adopt/${encodeURIComponent(slug)}`;
  if (opts?.fragment) return mutateJSON<ContextDoc>(url, 'POST', { fragment: opts.fragment });
  if (opts?.into) return mutateJSON<ContextDoc>(url, 'POST', { into: opts.into });
  return mutateJSON<ContextDoc>(url, 'POST');
}

/** Remove a client's managed artifact. POST /api/context/unsync/{slug} */
export async function unsyncGlobalContext(slug: string): Promise<void> {
  await mutateJSON<unknown>(`/api/context/unsync/${encodeURIComponent(slug)}`, 'POST');
}

/**
 * Canonical-vs-target unified diff. GET /api/context/diff/{slug}.
 * With `fragment`, diffs one projected fragment file on a multi-file client.
 */
export async function fetchGlobalContextDiff(slug: string, fragment?: string): Promise<string> {
  const query = fragment ? `?fragment=${encodeURIComponent(fragment)}` : '';
  const body = await fetchJSON<{ diff: string }>(
    `/api/context/diff/${encodeURIComponent(slug)}${query}`,
  );
  return body.diff;
}

/** Rule fragments in composition order. GET /api/context/fragments */
export async function fetchContextFragments(): Promise<{
  active: boolean;
  fragments: ContextFragment[];
}> {
  return fetchJSON<{ active: boolean; fragments: ContextFragment[] }>('/api/context/fragments');
}

/**
 * Create or update one fragment. PUT /api/context/fragments/{name}.
 * Empty content scaffolds a new fragment; the first ever fragment
 * activates fragments mode (the backend migrates AGENTS.md to
 * fragments/00-default.md with a backup and reports it via `migrated`).
 */
export async function saveContextFragment(
  name: string,
  content: string,
): Promise<{ name: string; migrated?: boolean }> {
  return mutateJSON<{ name: string; migrated?: boolean }>(
    `/api/context/fragments/${encodeURIComponent(name)}`,
    'PUT',
    { content },
  );
}

/** Delete one fragment (backed up out of tree first). DELETE /api/context/fragments/{name} */
export async function deleteContextFragment(name: string): Promise<{ name: string; backup: string }> {
  return mutateJSON<{ name: string; backup: string }>(
    `/api/context/fragments/${encodeURIComponent(name)}`,
    'DELETE',
  );
}

// === Packs API ===
// Wire format mirrors pkg/packops JSON tags.

/** One resource line in a pack document (status, apply, or remove). */
export interface PackRow {
  kind: 'skill' | 'agent' | 'rule' | 'wiring' | 'unresolved';
  name: string;
  client?: string;
  action?: string;
  state?: string;
  detail?: string;
  remediation?: string;
}

export interface PackOrigin {
  source: string;
  repo: string;
  ref?: string;
  commit_sha?: string;
  fetched_at?: string;
}

export interface PackCounts {
  skills: number;
  agents: number;
  rules: number;
  wiring: boolean;
}

/** Identity half of a pack: the list item shape. */
export interface PackInfo {
  name: string;
  version?: string;
  description?: string;
  author?: string;
  origin: PackOrigin;
  counts: PackCounts;
  unresolved?: string[];
  applied: boolean;
  collision?: boolean;
  collision_repos?: string[];
}

export interface PackListItem extends PackInfo {
  needs_attention: boolean;
}

export interface PackDetail {
  info: PackInfo;
  rows: PackRow[];
  needs_attention: boolean;
}

export interface PackAddDoc {
  pack: string;
  dry_run?: boolean;
  skills: string[];
  agents: string[];
  rules?: string[];
  wiring: boolean;
  unresolved?: string[];
  skipped?: string[];
  warnings?: string[];
}

export interface PackApplyDoc {
  pack: string;
  dry_run?: boolean;
  applied: number;
  total: number;
  rows: PackRow[];
}

export interface PackRemoveDoc {
  pack: string;
  dry_run?: boolean;
  rows: PackRow[];
  kept?: string[];
}

export interface PackPreviewResource {
  kind: string;
  name: string;
  findings?: { description?: string; pattern?: string; line?: number; severity?: string }[];
  /** True when the findings would block an untrusted import (the exact
   *  importer gate); non-blocking findings stay visible without forcing
   *  a trust grant. */
  blocking?: boolean;
}

export interface PackPreview {
  pack: string;
  version?: string;
  description?: string;
  author?: string;
  wiring: boolean;
  clients?: string[];
  skills: PackPreviewResource[];
  agents: PackPreviewResource[];
  rules: PackPreviewResource[];
  unresolved?: string[];
  warnings?: string[];
}

/** The 409 body a blocked-on-findings pack import carries. */
export class PackFindingsError extends Error {
  pack: string;
  findings: PackPreviewResource[];
  constructor(message: string, pack: string, findings: PackPreviewResource[]) {
    super(message);
    this.name = 'PackFindingsError';
    this.pack = pack;
    this.findings = findings;
  }
}

/** Shared fetch for pack endpoints that need response bodies on DELETE
 *  and typed 409 bodies on POST. */
async function packFetch<T>(endpoint: string, method: 'GET' | 'POST' | 'DELETE', body?: unknown): Promise<T> {
  const headers: Record<string, string> = { ...buildHeaders() };
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  const response = await fetch(`${API_BASE}${endpoint}`, {
    method,
    headers,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  if (response.status === 401) throw new AuthError('Authentication required');
  if (!response.ok) {
    const data = await response.json().catch(() => ({}));
    if (response.status === 409 && Array.isArray(data.findings)) {
      throw new PackFindingsError(data.error ?? 'Security findings', data.pack ?? '', data.findings);
    }
    throw new HTTPError(
      response.status,
      data.error || `${method} ${endpoint} failed: ${response.status}`,
      gitErrorExtra(data),
    );
  }
  return response.json();
}

/** Installed packs with identity, origin, counts, and attention. GET /api/packs */
export async function fetchPacks(): Promise<PackListItem[]> {
  const body = await packFetch<{ packs: PackListItem[] | null }>('/api/packs', 'GET');
  return body.packs ?? [];
}

/**
 * One pack's identity plus per-resource state rows. GET /api/packs/{name}.
 * A name claimed by two sources throws an HTTPError with status 409 whose
 * message names both repos (rendered as a detail banner, never a toast).
 */
export async function fetchPackDetail(name: string): Promise<PackDetail> {
  return packFetch<PackDetail>(`/api/packs/${encodeURIComponent(name)}`, 'GET');
}

/**
 * Import a pack from git (also the update path against a known origin).
 * Security findings without trust throw PackFindingsError before any
 * write. POST /api/packs
 */
export async function addPack(req: {
  repo: string;
  ref?: string;
  path?: string;
  trust?: boolean;
  dryRun?: boolean;
  auth?: SkillAuth;
}): Promise<{ doc: PackAddDoc; notes: string[] }> {
  return packFetch<{ doc: PackAddDoc; notes: string[] }>('/api/packs', 'POST', req);
}

/**
 * Resolve a pack manifest read-only. POST /api/packs/preview
 *
 * Omitting `auth` on a repository already imported with a credential
 * reference makes the server resolve that stored reference, which is how an
 * update previews a private pack with no user input. Passing an empty object
 * is an explicit request to use no credentials.
 */
export async function previewPack(req: {
  repo: string;
  ref?: string;
  path?: string;
  auth?: SkillAuth;
}): Promise<PackPreview> {
  return packFetch<PackPreview>('/api/packs/preview', 'POST', req);
}

/** Project one pack (CLI flag parity). POST /api/packs/{name}/apply */
export async function applyPack(
  name: string,
  opts?: { clients?: string[]; force?: boolean; dryRun?: boolean },
): Promise<PackApplyDoc> {
  const body: Record<string, unknown> = {};
  if (opts?.clients?.length) body.clients = opts.clients;
  if (opts?.force) body.force = true;
  if (opts?.dryRun) body.dry_run = true;
  return packFetch<PackApplyDoc>(`/api/packs/${encodeURIComponent(name)}/apply`, 'POST', body);
}

/** Cascade-remove one pack. DELETE /api/packs/{name}?dry_run=1&force=1 */
export async function removePack(
  name: string,
  opts?: { dryRun?: boolean; force?: boolean },
): Promise<PackRemoveDoc> {
  const params = new URLSearchParams();
  if (opts?.dryRun) params.set('dry_run', '1');
  if (opts?.force) params.set('force', '1');
  const query = params.toString();
  return packFetch<PackRemoveDoc>(
    `/api/packs/${encodeURIComponent(name)}${query ? `?${query}` : ''}`,
    'DELETE',
  );
}
