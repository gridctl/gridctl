import type { GatewayStatus, MCPServerStatus, ClientStatus, ToolsListResult, RegistryStatus, AgentSkill, SkillFile, SkillValidationResult, WorkflowDefinition, ExecutionResult, TokenMetricsResponse, ValidationResult, PlanDiff, SpecHealth, StackSpec, SkillSourceStatus, SkillPreviewResponse, ImportResult, SourceUpdateCheck, UpdateSummary, SkillTestResult } from '../types';

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
 * Fetch detected/linked LLM clients
 * GET /api/clients
 */
export async function fetchClients(): Promise<ClientStatus[]> {
  return fetchJSON<ClientStatus[]>('/api/clients');
}

// === Agent Control Functions (require backend endpoints) ===

/**
 * Fetch logs for a specific agent
 * GET /api/agents/{name}/logs
 */
export async function fetchAgentLogs(name: string, lines = 100): Promise<string[]> {
  const response = await fetch(
    `${API_BASE}/api/agents/${encodeURIComponent(name)}/logs?lines=${lines}`,
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
 * Restart an agent's container
 * POST /api/agents/{name}/restart
 */
export async function restartAgent(name: string): Promise<void> {
  const response = await fetch(`${API_BASE}/api/agents/${encodeURIComponent(name)}/restart`, {
    method: 'POST',
    headers: buildHeaders(),
  });

  if (response.status === 401) {
    throw new AuthError('Authentication required');
  }

  if (!response.ok) {
    throw new Error(`Restart failed: ${response.status} ${response.statusText}`);
  }
}

/**
 * Stop an agent's container
 * POST /api/agents/{name}/stop
 */
export async function stopAgent(name: string): Promise<void> {
  const response = await fetch(`${API_BASE}/api/agents/${encodeURIComponent(name)}/stop`, {
    method: 'POST',
    headers: buildHeaders(),
  });

  if (response.status === 401) {
    throw new AuthError('Authentication required');
  }

  if (!response.ok) {
    throw new Error(`Stop failed: ${response.status} ${response.statusText}`);
  }
}

// === MCP Server Control Functions ===

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

// === Structured Log Entry (from gateway) ===

export interface LogEntry {
  level: string;     // "DEBUG", "INFO", "WARN", "ERROR"
  ts: string;        // RFC3339Nano timestamp
  msg: string;       // Log message
  component?: string; // Component name (e.g., "gateway", "router")
  trace_id?: string;  // Trace ID for correlation
  attrs?: Record<string, unknown>; // Additional attributes
}

/**
 * Fetch structured gateway logs
 * GET /api/logs
 */
export async function fetchGatewayLogs(lines = 100, level?: string): Promise<LogEntry[]> {
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
 * Clear all token metrics
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
    throw new Error(data.error || `${method} ${endpoint} failed: ${response.status}`);
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

export async function runSkillTest(name: string): Promise<SkillTestResult> {
  return mutateJSON<SkillTestResult>(`/api/registry/skills/${encodeURIComponent(name)}/test`, 'POST');
}

export async function getSkillTestResult(name: string): Promise<SkillTestResult> {
  return fetchJSON<SkillTestResult>(`/api/registry/skills/${encodeURIComponent(name)}/test`);
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

// --- Workflow API ---

export async function fetchWorkflowDefinition(skillName: string): Promise<WorkflowDefinition> {
  return fetchJSON<WorkflowDefinition>(`/api/registry/skills/${encodeURIComponent(skillName)}/workflow`);
}

export async function executeWorkflow(
  skillName: string,
  args: Record<string, unknown>
): Promise<ExecutionResult> {
  return mutateJSON<ExecutionResult>(
    `/api/registry/skills/${encodeURIComponent(skillName)}/execute`,
    'POST',
    { arguments: args },
  );
}

export async function validateWorkflow(
  skillName: string,
  args: Record<string, unknown>
): Promise<{ valid: boolean; errors: string[]; warnings: string[]; resolvedArgs?: Record<string, Record<string, unknown>> }> {
  return mutateJSON(
    `/api/registry/skills/${encodeURIComponent(skillName)}/validate-workflow`,
    'POST',
    { arguments: args },
  );
}

// === Vault API ===

export interface VaultSecret {
  key: string;
  set?: string;
}

export interface VaultSet {
  name: string;
  description?: string;
  count: number;
}

export async function fetchVaultSecrets(): Promise<VaultSecret[]> {
  return fetchJSON<VaultSecret[]>('/api/vault');
}

export async function createVaultSecret(key: string, value: string, set?: string): Promise<void> {
  await mutateJSON<unknown>('/api/vault', 'POST', { key, value, ...(set ? { set } : {}) });
}

export async function getVaultSecret(key: string): Promise<{ key: string; value: string }> {
  return fetchJSON<{ key: string; value: string }>(`/api/vault/${encodeURIComponent(key)}`);
}

export async function updateVaultSecret(key: string, value: string): Promise<void> {
  await mutateJSON<unknown>(`/api/vault/${encodeURIComponent(key)}`, 'PUT', { value });
}

export async function deleteVaultSecret(key: string): Promise<void> {
  return mutateJSON<void>(`/api/vault/${encodeURIComponent(key)}`, 'DELETE');
}

export async function fetchVaultSets(): Promise<VaultSet[]> {
  return fetchJSON<VaultSet[]>('/api/vault/sets');
}

export async function createVaultSet(name: string): Promise<void> {
  await mutateJSON<unknown>('/api/vault/sets', 'POST', { name });
}

export async function deleteVaultSet(name: string): Promise<void> {
  return mutateJSON<void>(`/api/vault/sets/${encodeURIComponent(name)}`, 'DELETE');
}

export async function assignSecretToSet(key: string, set: string): Promise<void> {
  await mutateJSON<unknown>(`/api/vault/${encodeURIComponent(key)}/set`, 'PUT', { set });
}

// === Vault Encryption API ===

export interface VaultStatus {
  locked: boolean;
  encrypted: boolean;
  secrets_count?: number;
  sets_count?: number;
}

export async function fetchVaultStatus(): Promise<VaultStatus> {
  return fetchJSON<VaultStatus>('/api/vault/status');
}

export async function unlockVault(passphrase: string): Promise<{ status: string }> {
  return mutateJSON<{ status: string }>('/api/vault/unlock', 'POST', { passphrase });
}

export async function lockVault(passphrase: string): Promise<{ status: string }> {
  return mutateJSON<{ status: string }>('/api/vault/lock', 'POST', { passphrase });
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
 * Get secret-to-node mapping for heatmap overlay
 * GET /api/stack/secrets-map
 */
export async function fetchSecretsMap(): Promise<{
  secrets: Record<string, string[]>;
  nodes: Record<string, string[]>;
}> {
  return fetchJSON('/api/stack/secrets-map');
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
 * Apply available updates for a source
 * POST /api/skills/sources/{name}/update
 */
export async function updateSkillSource(name: string): Promise<{ source: string; results: unknown[] }> {
  return mutateJSON<{ source: string; results: unknown[] }>(
    `/api/skills/sources/${encodeURIComponent(name)}/update`,
    'POST',
  );
}

/**
 * Preview skills in a source without importing
 * GET /api/skills/sources/{name}/preview
 */
export async function previewSkillSource(
  name: string,
  params?: { repo?: string; ref?: string; path?: string },
): Promise<SkillPreviewResponse> {
  const query = new URLSearchParams();
  if (params?.repo) query.set('repo', params.repo);
  if (params?.ref) query.set('ref', params.ref);
  if (params?.path) query.set('path', params.path);
  const qs = query.toString();
  return fetchJSON<SkillPreviewResponse>(
    `/api/skills/sources/${encodeURIComponent(name)}/preview${qs ? `?${qs}` : ''}`,
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
}

export interface SpanEvent {
  name: string;
  timestamp: string;
  attributes: Record<string, string>;
}

export interface Span {
  spanId: string;
  parentSpanId?: string;
  name: string;
  startTime: string;
  endTime: string;
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
  if (params?.minDuration != null) query.set('minDuration', String(params.minDuration));
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
