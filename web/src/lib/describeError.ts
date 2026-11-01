import {
  AgentScanError,
  ClientLinkError,
  ClientScopeError,
  HTTPError,
  PackFindingsError,
  ProbeError,
  RestartRequiredError,
  SetServerToolsError,
  StackAlreadyActiveError,
  StackModifiedError,
} from './api';
import { AuthError, GatewayRequestError } from './gatewayRequest';

/** What failed, in product terms, plus a bounded reason and a next step. */
export interface ErrorDescription {
  /** What failed, in product terms: `${context} failed` unless a known class supplies better. */
  summary: string;
  /** Why, trimmed: first line, whitespace-collapsed, at most 160 characters with an ellipsis. Never "[object Object]". */
  detail?: string;
  /** What to do. */
  hint?: string;
  /** Untrimmed detail for a title attribute or expandable region. Omitted when identical to detail. */
  fullDetail?: string;
  /** Composed one-line text. Summary, then ": " and detail when present. A sentence boundary precedes the hint. Ends with a period. */
  text: string;
}

const DETAIL_LIMIT = 160;
const RELOAD_FAILED_HINT = 'The file was saved. Use Reload Config or run gridctl reload.';

/** Safe message extraction. Never "[object Object]". */
export function messageOf(err: unknown): string {
  let message = '';
  if (typeof err === 'string') message = err;
  else if (err instanceof Error) message = err.message;
  else if (isMessageObject(err)) message = err.message;
  if (!message || message === '[object Object]') return '';
  return message;
}

/** Normalize to Error without stringifying the value. */
export function asError(err: unknown): Error {
  if (err instanceof Error) return err;
  return new Error(messageOf(err));
}

/** True for user or generation cancellations that must not be toasted. */
export function isCancellation(err: unknown): boolean {
  if (isAbortError(err)) return true;
  return err instanceof GatewayRequestError && (err.kind === 'abort' || err.kind === 'stale');
}

function isAbortError(err: unknown): boolean {
  if (!err || (typeof err !== 'object' && typeof err !== 'function')) return false;
  const name = 'name' in err ? err.name : undefined;
  if (name === 'AbortError') return true;
  return typeof DOMException !== 'undefined' && err instanceof DOMException && err.code === DOMException.ABORT_ERR;
}

export function describeError(err: unknown, context: string): ErrorDescription {
  if (err instanceof AuthError) {
    return finish(`${context} paused`, undefined, 'Verify your gateway credential to resume.');
  }
  if (err instanceof GatewayRequestError) {
    if (err.kind === 'abort' || err.kind === 'stale') return finish(`${context} cancelled`);
    const bounded = boundDetail(messageOf(err));
    if (err.kind === 'connection') {
      return finish(`${context} failed`, bounded.detail, 'Check that the gateway is running and reachable.', bounded.fullDetail);
    }
    if (err.kind === 'redirect' || err.kind === 'origin') {
      return finish(`${context} failed`, bounded.detail, 'Open the UI directly on the gateway origin.', bounded.fullDetail);
    }
    if (err.kind === 'malformed') {
      return finish(`${context} failed`, bounded.detail, 'Refresh and retry. If it persists, check gridctl logs.', bounded.fullDetail);
    }
  }
  // AgentScanError extends HTTPError with status 409. It must win over the generic conflict hint.
  if (err instanceof AgentScanError) {
    const bounded = boundDetail(messageOf(err));
    return finish(`${context} blocked by findings`, bounded.detail, 'Review the security scan findings before saving again.', bounded.fullDetail);
  }
  if (err instanceof HTTPError) {
    const bounded = boundDetail(messageOf(err));
    if (err.code === 'ssh_agent_unavailable') {
      return finish(`${context} failed`, bounded.detail, 'No ssh-agent is reachable. Use the HTTPS URL with a credential.', bounded.fullDetail);
    }
    return finish(`${context} failed`, bounded.detail, httpStatusHint(err.status), bounded.fullDetail);
  }
  if (
    err instanceof ClientLinkError ||
    err instanceof ClientScopeError ||
    err instanceof SetServerToolsError ||
    err instanceof ProbeError ||
    err instanceof StackModifiedError
  ) {
    const bounded = boundDetail(messageOf(err));
    const hint = err.code === 'reload_failed' ? RELOAD_FAILED_HINT : err.hint;
    return finish(`${context} failed`, bounded.detail, hint, bounded.fullDetail);
  }
  if (err instanceof RestartRequiredError) {
    const bounded = boundDetail(messageOf(err));
    return finish(`${context} requires a restart`, bounded.detail, 'Restart the gateway with its original startup options.', bounded.fullDetail);
  }
  if (err instanceof PackFindingsError) {
    const bounded = boundDetail(messageOf(err));
    return finish(`${context} blocked by findings`, bounded.detail, 'Review the findings and acknowledge trust to continue.', bounded.fullDetail);
  }
  if (err instanceof StackAlreadyActiveError) {
    return finish(`${context} failed`, 'A stack is already active.', 'Stop it before initializing another.');
  }
  if (err instanceof Error) {
    const raw = messageOf(err);
    const bounded = boundDetail(raw);
    const status = apiErrorStatus(raw);
    return finish(`${context} failed`, bounded.detail, status === undefined ? undefined : httpStatusHint(status), bounded.fullDetail);
  }
  if (typeof err === 'string') {
    const bounded = boundDetail(err);
    return finish(`${context} failed`, bounded.detail, undefined, bounded.fullDetail);
  }
  if (isMessageObject(err)) {
    const bounded = boundDetail(err.message);
    return finish(`${context} failed`, bounded.detail, undefined, bounded.fullDetail);
  }
  return finish(`${context} failed`);
}

function isMessageObject(err: unknown): err is { message: string } {
  return !!err && typeof err === 'object' && 'message' in err && typeof err.message === 'string';
}

function apiErrorStatus(message: string): number | undefined {
  const match = /^API error: (\d{3})/.exec(message);
  return match ? Number(match[1]) : undefined;
}

function httpStatusHint(status: number): string | undefined {
  if (status === 404) return 'It may have been removed. Refresh and retry.';
  if (status === 409) return 'Something changed underneath. Refresh and retry.';
  if (status >= 500 && status <= 599) return 'The gateway reported an error. Check gridctl logs.';
  return undefined;
}

function boundDetail(raw: string): { detail?: string; fullDetail?: string } {
  const stripped = raw.replace(/^Error: /, '');
  if (!stripped.trim() || stripped.trim() === '[object Object]') return {};
  const full = stripped.trim();
  const first = full.split(/\r?\n/, 1)[0] ?? '';
  const collapsed = first.replace(/\s+/g, ' ').trim();
  if (!collapsed || collapsed === '[object Object]') return {};
  const detail = collapsed.length > DETAIL_LIMIT ? `${collapsed.slice(0, DETAIL_LIMIT - 1)}…` : collapsed;
  return full === detail ? { detail } : { detail, fullDetail: full };
}

function finish(summary: string, detail?: string, hint?: string, fullDetail?: string): ErrorDescription {
  let text = summary;
  if (detail) text += `: ${detail}`;
  if (hint) {
    // Server messages usually have no trailing period. Close that sentence
    // before the hint so the two do not run together.
    if (!/[.!?]$/.test(text)) text += '.';
    text += ` ${hint}`;
  }
  if (!text.endsWith('.')) text += '.';
  const described: ErrorDescription = { summary, text };
  if (detail) described.detail = detail;
  if (hint) described.hint = hint;
  if (fullDetail && fullDetail !== detail) described.fullDetail = fullDetail;
  return described;
}
