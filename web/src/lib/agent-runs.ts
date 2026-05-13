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

export interface LaunchRunRequest {
  skill_name: string;
  input: Record<string, unknown>;
}

export interface LaunchRunResponse {
  run_id: string;
  started_at: string;
}

// LaunchRunError carries the structured server-side rejection so the
// modal can render the operator-facing message verbatim (skill not
// found, wrong handler language, invalid input, runtime not configured).
export class LaunchRunError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.name = 'LaunchRunError';
    this.status = status;
  }
}

export interface AgentRunEvent {
  run_id: string;
  seq: number;
  time: string;
  type: string;
  payload?: Record<string, unknown>;
}

export interface AgentRunDetail {
  run: AgentRunSummary;
  events: AgentRunEvent[];
}

/**
 * Fetch a single run's typed event timeline. The first event is
 * guaranteed to be `run_started` with the input payload — the Run
 * Launcher uses this to pre-fill the editor when the operator selects
 * a previous run from the "Run like…" picker.
 */
export async function fetchAgentRun(runID: string): Promise<AgentRunDetail | null> {
  const response = await fetch(
    `${API_BASE}/api/agent/runs/${encodeURIComponent(runID)}`,
    { headers: buildHeaders() },
  );
  if (response.status === 404 || response.status === 503) return null;
  if (!response.ok) {
    throw new Error(`fetchAgentRun(${runID}): ${response.status} ${response.statusText}`);
  }
  return (await response.json()) as AgentRunDetail;
}

/**
 * Extract the input object from a run's run_started event. Returns
 * {} when the event is missing or the input is empty.
 */
export function inputFromRunDetail(detail: AgentRunDetail | null): Record<string, unknown> {
  if (!detail) return {};
  const started = detail.events.find((e) => e.type === 'run_started');
  const raw = started?.payload?.input;
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return {};
  return raw as Record<string, unknown>;
}

/**
 * Start a new agent run for the given skill via POST /api/agent/runs.
 * Returns the run id and start timestamp; the run continues async and
 * its events stream through the existing /api/agent/runs/{id}/events
 * SSE endpoint.
 */
export async function launchRun(req: LaunchRunRequest): Promise<LaunchRunResponse> {
  const response = await fetch(`${API_BASE}/api/agent/runs`, {
    method: 'POST',
    headers: buildHeaders({ 'Content-Type': 'application/json' }),
    body: JSON.stringify({ skill_name: req.skill_name, input: req.input }),
  });
  if (!response.ok) {
    let message = `${response.status} ${response.statusText}`;
    try {
      const body = (await response.json()) as { error?: string };
      if (body.error) message = body.error;
    } catch {
      const text = await response.text().catch(() => '');
      if (text) message = text;
    }
    throw new LaunchRunError(response.status, message);
  }
  return (await response.json()) as LaunchRunResponse;
}
