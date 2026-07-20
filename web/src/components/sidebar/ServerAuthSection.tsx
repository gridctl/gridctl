import { useCallback, useEffect, useRef, useState } from 'react';
import { Check, Copy, KeyRound, LogOut, ShieldCheck } from 'lucide-react';
import { cn } from '../../lib/cn';
import {
  beginServerAuthorization,
  fetchAuthServers,
  logoutServerAuthorization,
  waitServerAuthorization,
} from '../../lib/api';
import type { ServerAuthInfo } from '../../types';

/**
 * ServerAuthSection is the Sidebar's downstream authorization panel for
 * external servers with OAuth brokering (auth: {type: oauth} in stack.yaml).
 * This is SERVER authorization (gridctl acting as OAuth client to the
 * downstream server), unrelated to the gateway's own inbound API auth.
 *
 * Authorize opens the provider's consent page in a popup and long-polls the
 * daemon's wait endpoint; the popup self-closes after the callback and the
 * 3s status poll flips the canvas node live. The waiting phase is always
 * escapable: a Cancel button closes the popup and aborts the wait, and
 * closing the provider window by hand resets to idle within about a second.
 * When the popup is blocked, the authorization URL is rendered as a plain
 * anchor (anchor clicks are never popup-blocked) with a copy button.
 */

type FlowPhase =
  | { kind: 'idle' }
  | { kind: 'starting' }
  | { kind: 'waiting'; authorizeUrl: string; popupBlocked: boolean }
  | { kind: 'done' }
  | { kind: 'failed'; message: string };

interface ServerAuthSectionProps {
  serverName: string;
  authStatus: 'authorized' | 'needs_auth';
  authIssuer?: string;
  authExpiry?: string;
}

export function ServerAuthSection({ serverName, authStatus, authIssuer, authExpiry }: ServerAuthSectionProps) {
  const [phase, setPhase] = useState<FlowPhase>({ kind: 'idle' });
  const [detail, setDetail] = useState<ServerAuthInfo | null>(null);
  const [copied, setCopied] = useState(false);
  const mounted = useRef(true);
  const popupRef = useRef<Window | null>(null);
  const abortRef = useRef<AbortController | null>(null);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      // Abort the wait long-poll so an unmounted section never leaves a
      // request hanging. The popup stays open — the user may still be
      // mid-consent, and the daemon completes the flow without us.
      abortRef.current?.abort();
    };
  }, []);

  // The Sidebar reuses one instance across node selections, so a retarget to
  // another server must not inherit the previous server's flow: abort the
  // in-flight wait (the popup stays open; the daemon completes the flow and
  // the status poll reflects it) and start the new server at idle.
  const prevServerName = useRef(serverName);
  const prevAuthStatus = useRef(authStatus);
  useEffect(() => {
    if (prevServerName.current === serverName) return;
    prevServerName.current = serverName;
    prevAuthStatus.current = authStatus;
    abortRef.current?.abort();
    popupRef.current = null;
    setPhase({ kind: 'idle' });
    setCopied(false);
  }, [serverName, authStatus]);

  // The parent's authStatus arrives via the 3s status poll; a stale local
  // done/failed message beside a fresh status badge reads as a contradiction.
  // Reset terminal phases to idle whenever the status transitions.
  useEffect(() => {
    if (prevAuthStatus.current === authStatus) return;
    prevAuthStatus.current = authStatus;
    setPhase((p) => (p.kind === 'done' || p.kind === 'failed' ? { kind: 'idle' } : p));
  }, [authStatus]);

  // While waiting, poll the retained popup handle so closing the provider
  // window by hand cancels the flow instead of leaving the section stuck.
  useEffect(() => {
    if (phase.kind !== 'waiting') return;
    const popup = popupRef.current;
    if (!popup) return;
    const interval = window.setInterval(() => {
      if (!popup.closed) return;
      window.clearInterval(interval);
      popupRef.current = null;
      abortRef.current?.abort();
    }, 1000);
    return () => window.clearInterval(interval);
  }, [phase.kind]);

  // Scopes are only in the auth detail endpoint, not the status payload.
  // Best effort: a failure leaves the section on status-payload data alone.
  useEffect(() => {
    let cancelled = false;
    fetchAuthServers()
      .then((infos) => {
        if (cancelled) return;
        setDetail(infos.find((i) => i.server === serverName) ?? null);
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [serverName, authStatus]);

  const handleAuthorize = useCallback(async () => {
    setPhase({ kind: 'starting' });
    setCopied(false);
    const controller = new AbortController();
    abortRef.current = controller;
    try {
      const login = await beginServerAuthorization(serverName);
      if (!mounted.current) return;

      // No 'noopener' here: window.open would then return null even on
      // success, and the retained handle is what powers Cancel and the
      // closed-popup detection. The blocked-state fallback anchor keeps
      // rel="noopener" instead.
      const popup = window.open(login.authorize_url, '_blank', 'width=560,height=720');
      // Sever the popup's back-reference so the provider page cannot
      // navigate this tab (reverse tabnabbing); our own handle keeps
      // working for close() and the closed-poll.
      if (popup) popup.opener = null;
      popupRef.current = popup;
      setPhase({ kind: 'waiting', authorizeUrl: login.authorize_url, popupBlocked: popup == null });

      await waitServerAuthorization(serverName, login.state, controller.signal);
      if (!mounted.current) return;
      popupRef.current = null;
      setPhase({ kind: 'done' });
    } catch (err) {
      if (!mounted.current) return;
      // An aborted wait (Cancel, closed popup, unmount) returns silently to
      // idle — never to the failure box.
      if (err instanceof DOMException && err.name === 'AbortError') {
        setPhase({ kind: 'idle' });
        return;
      }
      setPhase({ kind: 'failed', message: err instanceof Error ? err.message : String(err) });
    }
  }, [serverName]);

  const handleCancel = useCallback(() => {
    popupRef.current?.close();
    popupRef.current = null;
    abortRef.current?.abort();
    setPhase({ kind: 'idle' });
  }, []);

  const handleSignOut = useCallback(async () => {
    try {
      await logoutServerAuthorization(serverName);
      if (!mounted.current) return;
      setPhase({ kind: 'idle' });
      setDetail(null);
    } catch (err) {
      if (!mounted.current) return;
      setPhase({ kind: 'failed', message: err instanceof Error ? err.message : String(err) });
    }
  }, [serverName]);

  const handleCopyUrl = useCallback((url: string) => {
    navigator.clipboard
      .writeText(url)
      .then(() => setCopied(true))
      .catch(() => {});
  }, []);

  const authorized = authStatus === 'authorized';
  const busy = phase.kind === 'starting' || phase.kind === 'waiting';
  const issuer = authIssuer ?? detail?.issuer;
  const expiry = authExpiry ?? detail?.expiry;
  const scopes = detail?.scopes ?? [];

  return (
    <div className="space-y-3" aria-label={`Authorization for ${serverName}`}>
      <div className="flex justify-between items-center">
        <span className="text-sm text-text-muted">State</span>
        <span
          className={cn(
            'text-xs px-2 py-0.5 rounded-md font-medium flex items-center gap-1',
            authorized
              ? 'bg-status-running/10 text-status-running'
              : 'bg-status-pending/10 text-status-pending',
          )}
        >
          {authorized ? <ShieldCheck size={10} /> : <KeyRound size={10} />}
          {authorized ? 'Authorized' : 'Needs authorization'}
        </span>
      </div>

      {issuer && (
        <div className="flex justify-between items-center gap-4">
          <span className="text-sm text-text-muted">Issuer</span>
          <span
            className="text-xs text-text-secondary font-mono truncate max-w-[180px] bg-background/50 px-2 py-1 rounded-md"
            title={issuer}
          >
            {issuer}
          </span>
        </div>
      )}

      {expiry && (
        <div className="flex justify-between items-center">
          <span className="text-sm text-text-muted">Token expires</span>
          <span className="text-xs text-text-secondary font-mono">
            {formatExpiry(expiry)}
          </span>
        </div>
      )}

      {scopes.length > 0 && (
        <div className="space-y-1">
          <span className="text-sm text-text-muted">Scopes</span>
          <div className="flex flex-wrap gap-1">
            {scopes.map((scope) => (
              <span
                key={scope}
                className="text-[10px] px-1.5 py-0.5 rounded font-mono bg-surface-highlight text-text-secondary"
              >
                {scope}
              </span>
            ))}
          </div>
        </div>
      )}

      <div className="flex items-center gap-2 pt-1">
        <button
          type="button"
          onClick={handleAuthorize}
          disabled={busy}
          className={cn(
            'inline-flex items-center gap-1.5 rounded-md px-3 py-1.5 text-[11px] font-medium transition-colors',
            'bg-status-pending/15 text-status-pending border border-status-pending/30 hover:bg-status-pending/25',
            'disabled:opacity-60 disabled:cursor-not-allowed',
          )}
        >
          <KeyRound size={11} />
          {busy ? 'Waiting for provider…' : authorized ? 'Re-authorize' : 'Authorize'}
        </button>
        {phase.kind === 'waiting' && (
          <button
            type="button"
            onClick={handleCancel}
            className={cn(
              'inline-flex items-center gap-1.5 rounded-md px-3 py-1.5 text-[11px] font-medium transition-colors',
              'text-text-muted border border-border/50 hover:text-text-primary hover:border-border',
            )}
          >
            Cancel
          </button>
        )}
        {authorized && (
          <button
            type="button"
            onClick={handleSignOut}
            disabled={busy}
            className={cn(
              'inline-flex items-center gap-1.5 rounded-md px-3 py-1.5 text-[11px] font-medium transition-colors',
              'text-text-muted border border-border/50 hover:text-text-primary hover:border-border',
              'disabled:opacity-60 disabled:cursor-not-allowed',
            )}
          >
            <LogOut size={11} />
            Sign out
          </button>
        )}
      </div>

      {phase.kind === 'waiting' && phase.popupBlocked && (
        <div
          role="status"
          className="p-2 rounded-md bg-status-pending/5 border border-status-pending/20 space-y-1.5"
        >
          <p className="text-[11px] text-status-pending font-medium">
            Popup blocked. Open the authorization page yourself:
          </p>
          <div className="flex items-center gap-1.5">
            <a
              href={phase.authorizeUrl}
              target="_blank"
              rel="noopener"
              title={phase.authorizeUrl}
              className="flex-1 text-[11px] text-secondary hover:text-secondary-light underline underline-offset-2 truncate transition-colors"
            >
              Open authorization page
            </a>
            <button
              type="button"
              onClick={() => handleCopyUrl(phase.authorizeUrl)}
              aria-label="Copy authorization URL"
              className="p-1.5 rounded-md border border-border/50 text-text-muted hover:text-text-primary transition-colors"
            >
              {copied ? <Check size={11} className="text-status-running" /> : <Copy size={11} />}
            </button>
          </div>
        </div>
      )}

      {phase.kind === 'waiting' && (
        <p className="text-[10px] text-text-muted">
          Remote daemon? Authorize from the machine running gridctl with
          'gridctl auth login {serverName}', or use --manual over SSH.
        </p>
      )}

      {phase.kind === 'done' && (
        <p className="text-[11px] text-status-running flex items-center gap-1.5">
          <ShieldCheck size={11} />
          Authorized. The server reconnects automatically.
        </p>
      )}

      {phase.kind === 'failed' && (
        <div role="alert" className="p-2 rounded-md bg-status-error/5 border border-status-error/15">
          <p className="text-[11px] text-status-error break-words">{phase.message}</p>
        </div>
      )}
    </div>
  );
}

function formatExpiry(iso: string): string {
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return iso;
  return new Date(t).toLocaleString();
}
