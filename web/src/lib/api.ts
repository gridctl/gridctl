import type { GatewayStatus, MCPServerStatus, ClientStatus, ToolsListResult, RegistryStatus, AgentSkill, SkillFile, SkillValidationResult, WorkflowDefinition, ExecutionResult } from '../types';

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
