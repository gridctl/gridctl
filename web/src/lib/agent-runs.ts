/**
 * Agent runtime client. Wraps /api/agent/runs/* and exposes the shapes
 * the ApprovalBanner and Runs panels render against.
 */

const API_BASE = '';

const AUTH_STORAGE_KEY = 'gridctl-auth-token';

function buildHeaders(extra?: Record<string, string>): Record<string, string> {
  const headers: Record<string, string> = { ...extra };
  try {
    const token = localStorage.getItem(AUTH_STORAGE_KEY);
    if (token) {
      headers['Authorization'] = `Bearer ${token}`;
    }
  } catch {
    // localStorage may be unavailable
  }
  return headers;
}

export interface AgentRunSummary {
  run_id: string;
  skill?: string;
  flavor?: string;
  status: string;
  started_at?: string;
  completed_at?: string;
  event_count: number;
  parent_run_id?: string;
  trace_id?: string;
  pending_approval?: string;
  error?: string;
}

export interface AgentRunListResponse {
  runs: AgentRunSummary[];
}

export async function fetchAgentRuns(limit = 50): Promise<AgentRunSummary[]> {
  const response = await fetch(`${API_BASE}/api/agent/runs?limit=${limit}`, {
    headers: buildHeaders(),
  });
  if (response.status === 503) {
    return [];
  }
  if (!response.ok) {
    throw new Error(`agent runs API: ${response.status} ${response.statusText}`);
  }
  const body = (await response.json()) as AgentRunListResponse;
  return body.runs ?? [];
}

export interface ApprovalRequest {
  run_id: string;
  approval_id?: string;
  approved: boolean;
  reason?: string;
  source?: string;
}

export async function approveAgentRun(req: ApprovalRequest): Promise<void> {
  const response = await fetch(`${API_BASE}/api/agent/runs/${encodeURIComponent(req.run_id)}/approve`, {
    method: 'POST',
    headers: buildHeaders({ 'Content-Type': 'application/json' }),
    body: JSON.stringify({
      approval_id: req.approval_id,
      approved: req.approved,
      reason: req.reason,
      source: req.source ?? 'web',
    }),
  });
  if (!response.ok) {
    const text = await response.text().catch(() => '');
    throw new Error(`approve failed: ${response.status} ${text}`);
  }
}
