import { AuthError, HTTPError } from './api';

/**
 * SCP-style git URL: `user@host:path`, any user. Mirrors the server's pattern
 * in `pkg/git/auth.go` deliberately — a narrower client rule (matching only
 * `git@`) shows token fields for something like `deploy@git.internal:acme/p.git`,
 * and the token then fails with a protocol mismatch it could never satisfy.
 */
const SCP_SYNTAX = /^[a-zA-Z0-9._-]+@[a-zA-Z0-9._-]+:/;

/** SSH-form git URL: SCP syntax or an explicit ssh:// scheme. */
export function isSSHUrl(url: string): boolean {
  const trimmed = url.trim();
  return trimmed.startsWith('ssh://') || SCP_SYNTAX.test(trimmed);
}

/**
 * The server's machine code for "this process has no reachable ssh-agent".
 * Matched exactly; the accompanying message is prose and must not be parsed.
 */
export const SSH_AGENT_UNAVAILABLE = 'ssh_agent_unavailable';

/**
 * True when a failure is the ssh-agent-unavailable case. Credentials cannot
 * fix it, so callers must NOT respond by opening a token field — that would
 * imply a remedy that does not exist.
 */
export function isSSHAgentError(err: unknown): boolean {
  return err instanceof HTTPError && err.code === SSH_AGENT_UNAVAILABLE;
}

/**
 * The HTTPS URL for the same repository, computed by the server, when it
 * could derive one. Present only for an SSH input URL.
 */
export function httpsEquivalentOf(err: unknown): string | undefined {
  return err instanceof HTTPError ? err.httpsEquivalent : undefined;
}

/**
 * Classify a failure to decide whether to auto-open the auth card.
 *
 * The status check alone is not enough. The fetch layer converts every 401
 * into an AuthError, which carries no status, so a private repository's
 * "authentication required" arrives as a bare message and would otherwise
 * slip past — leaving the card shut on the single case it exists for. The
 * message fallback is what actually catches 401 today; keep both.
 */
export function shouldOpenAuthCard(err: unknown): boolean {
  // A missing agent is auth-shaped but not token-fixable.
  if (isSSHAgentError(err)) return false;
  if (err instanceof HTTPError && (err.status === 401 || err.status === 404)) return true;
  if (err instanceof AuthError) return true;
  const msg = err instanceof Error ? err.message.toLowerCase() : '';
  return (
    msg.includes('authentication required') ||
    msg.includes('authentication failed') ||
    msg.includes('repository not found') ||
    msg.includes('credentials were rejected')
  );
}

/** The banner text that matches how the failure actually reads to a user. */
export function authBannerFor(err: unknown): string {
  if (err instanceof HTTPError && err.status === 404) {
    return 'Not found. If this is a private repository, add credentials below and try again.';
  }
  return 'This repository requires authentication.';
}
