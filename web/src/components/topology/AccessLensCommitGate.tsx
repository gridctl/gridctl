import { useCallback, useEffect, useState } from 'react';
import { AlertTriangle, Check, Copy, Loader2, ShieldAlert } from 'lucide-react';
import { cn } from '../../lib/cn';
import { Modal } from '../ui/Modal';
import { useAccessLensStore, canonical } from '../../stores/useAccessLensStore';
import {
  AuthError,
  ClientScopeError,
  previewClientScope,
  updateClientScope,
  type ClientScopePreview,
} from '../../lib/api';
import { showToast } from '../ui/Toast';

interface AccessLensCommitGateProps {
  isOpen: boolean;
  onClose: () => void;
  /** Called after the scope is written and state refreshed. */
  onCommitted: () => void;
}

// AccessLensCommitGate is the single write boundary for the Access Lens draft.
// It is a focus-trapped Modal (the gate IS modal, unlike the slide-over) that
// fetches the server-computed preview — the exact stack.yaml patch and the
// per-client impact — and only writes via PUT /scope when the operator confirms.
// A draft that would lock the client out of everything blocks the commit.
export function AccessLensCommitGate({ isOpen, onClose, onCommitted }: AccessLensCommitGateProps) {
  const clientSlug = useAccessLensStore((s) => s.clientSlug);
  const clientName = useAccessLensStore((s) => s.clientName);
  const draft = useAccessLensStore((s) => s.draft);
  const markSaved = useAccessLensStore((s) => s.markSaved);
  const setConflict = useAccessLensStore((s) => s.setConflict);

  const [preview, setPreview] = useState<ClientScopePreview | null>(null);
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [committing, setCommitting] = useState(false);
  const [copied, setCopied] = useState(false);

  const body = clientSlug ? { servers: canonical(draft) } : null;

  const loadPreview = useCallback(async () => {
    if (!clientSlug || !body) return;
    setLoading(true);
    setLoadError(null);
    try {
      const result = await previewClientScope(clientSlug, body);
      setPreview(result);
    } catch (err) {
      if (err instanceof AuthError) {
        setLoadError('Authentication required.');
      } else if (err instanceof ClientScopeError) {
        setLoadError(err.hint ? `${err.message} — ${err.hint}` : err.message);
      } else {
        setLoadError(err instanceof Error ? err.message : 'Preview failed');
      }
    } finally {
      setLoading(false);
    }
    // body is derived from clientSlug + draft; depending on those is sufficient.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [clientSlug, draft]);

  useEffect(() => {
    // Defer out of the synchronous effect body (the fetch sets loading state, and
    // close resets it) so we don't trip the no-setState-in-effect rule.
    const t = setTimeout(() => {
      if (isOpen) {
        void loadPreview();
      } else {
        setPreview(null);
        setLoadError(null);
        setCopied(false);
      }
    }, 0);
    return () => clearTimeout(t);
  }, [isOpen, loadPreview]);

  async function copyDiff() {
    if (!preview?.diff) return;
    try {
      await navigator.clipboard.writeText(preview.diff);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      /* clipboard may be unavailable; ignore */
    }
  }

  async function confirm() {
    if (!clientSlug || preview?.lockout) return;
    setCommitting(true);
    try {
      const resp = await updateClientScope(clientSlug, { servers: canonical(draft) });
      if (resp.reloaded === false) {
        showToast('warning', 'Stack updated. Run "gridctl reload" or restart with --watch to apply.');
      } else {
        showToast('success', `Access saved for ${clientName ?? clientSlug}`);
      }
      markSaved();
      onCommitted();
    } catch (err) {
      if (err instanceof AuthError) {
        showToast('error', 'Authentication required.');
      } else if (err instanceof ClientScopeError) {
        if (err.code === 'stack_modified') {
          // Preserve the draft; surface the conflict on the slide-over and bounce
          // back so the operator can reload and retry without losing edits.
          setConflict(err.hint || err.message);
          showToast('warning', 'The stack file changed on disk. Reload and retry — your draft is kept.');
          onClose();
          return;
        }
        showToast('error', err.hint ? `${err.message} — ${err.hint}` : err.message);
      } else {
        showToast('error', err instanceof Error ? err.message : 'Save failed');
      }
    } finally {
      setCommitting(false);
    }
  }

  return (
    <Modal isOpen={isOpen} onClose={onClose} title="Review access change" size="wide">
      <div className="space-y-4 text-sm">
        {loading && (
          <div className="flex items-center gap-2 text-[12px] text-text-muted py-6 justify-center" role="status">
            <Loader2 size={14} className="animate-spin" />
            Computing the patch and impact…
          </div>
        )}

        {loadError && !loading && (
          <div className="flex items-start gap-2 rounded-md border border-status-error/40 bg-status-error/[0.06] px-3 py-2" role="alert">
            <ShieldAlert size={13} className="text-status-error flex-shrink-0 mt-0.5" />
            <div className="flex-1 space-y-1.5">
              <p className="text-[11px] text-status-error">{loadError}</p>
              <button
                type="button"
                onClick={() => void loadPreview()}
                className="text-[10px] text-secondary hover:text-secondary-light transition-colors"
              >
                Retry
              </button>
            </div>
          </div>
        )}

        {preview && !loading && (
          <>
            {preview.lockout && (
              <GateWarning tone="error" icon={<ShieldAlert size={13} className="text-status-error" />}>
                This draft leaves <span className="font-medium">{clientName}</span> able to reach{' '}
                <span className="font-medium">no servers or tools</span>. Grant at least one reachable
                server before saving — an empty allow-list means &ldquo;all&rdquo;, so this is blocked
                to avoid a silent lockout.
              </GateWarning>
            )}

            {preview.createsBlock && (
              <GateWarning tone="pending" icon={<AlertTriangle size={13} className="text-status-pending" />}>
                This creates the <span className="font-mono">clients</span> block. Unlisted clients
                become <span className="font-medium">deny by default</span>.
                {preview.affected && preview.affected.length > 0 && (
                  <ul className="mt-1.5 space-y-0.5 text-[10px] text-text-muted">
                    {preview.affected.map((a) => (
                      <li key={a.slug}>
                        <span className="font-mono text-text-secondary">{a.name}</span> loses access to
                        all {a.beforeServers} server{a.beforeServers === 1 ? '' : 's'} ({a.beforeTools}{' '}
                        tool{a.beforeTools === 1 ? '' : 's'} hidden)
                      </li>
                    ))}
                  </ul>
                )}
              </GateWarning>
            )}

            <SelectedImpact preview={preview} clientName={clientName ?? preview.client} />

            <div className="space-y-1.5">
              <div className="flex items-center justify-between">
                <span className="text-[10px] uppercase tracking-[0.18em] text-text-muted/70">
                  stack.yaml patch
                </span>
                <button
                  type="button"
                  onClick={() => void copyDiff()}
                  disabled={!preview.diff}
                  className="inline-flex items-center gap-1 text-[10px] text-text-muted hover:text-text-secondary transition-colors disabled:opacity-40"
                >
                  {copied ? <Check size={10} className="text-status-running" /> : <Copy size={10} />}
                  {copied ? 'Copied' : 'Copy'}
                </button>
              </div>
              <pre className="max-h-56 overflow-auto scrollbar-dark rounded-md border border-border/40 bg-background/70 px-3 py-2 text-[11px] font-mono leading-relaxed">
                {preview.diff ? <DiffLines diff={preview.diff} /> : (
                  <span className="text-text-muted/60 italic">No file change (draft matches saved scope).</span>
                )}
              </pre>
            </div>

            <div className="flex items-center justify-end gap-2 pt-1">
              <button
                type="button"
                onClick={onClose}
                disabled={committing}
                className="rounded-md px-3 py-1.5 text-[11px] text-text-secondary hover:text-text-primary transition-colors disabled:opacity-50"
              >
                Back
              </button>
              <button
                type="button"
                onClick={() => void confirm()}
                disabled={committing || preview.lockout || !preview.diff}
                className={cn(
                  'inline-flex items-center gap-1.5 rounded-md px-3 py-1.5 text-[11px] font-medium border transition-colors',
                  committing || preview.lockout || !preview.diff
                    ? 'bg-surface-highlight/50 text-text-muted border-border/30 cursor-not-allowed'
                    : 'bg-primary/20 text-primary border-primary/30 hover:bg-primary/30',
                )}
              >
                {committing ? (
                  <>
                    <Loader2 size={11} className="animate-spin" />
                    Writing…
                  </>
                ) : (
                  'Save & write'
                )}
              </button>
            </div>
          </>
        )}
      </div>
    </Modal>
  );
}

function SelectedImpact({ preview, clientName }: { preview: ClientScopePreview; clientName: string }) {
  const sel = preview.selected;
  const lost = sel.lostServers ?? [];
  const gained = sel.gainedServers ?? [];
  return (
    <div className="rounded-md border border-border/30 bg-background/40 px-3 py-2.5 text-[11px] text-text-secondary space-y-1" role="status">
      <p>
        <span className="font-medium text-text-primary">{clientName}</span> will reach{' '}
        <span className="font-mono text-primary">{sel.afterServers}</span> of {preview.totalServers}{' '}
        servers · <span className="font-mono text-primary">{sel.afterTools}</span> of{' '}
        {preview.totalTools} tools.
      </p>
      {lost.length > 0 && (
        <p className="text-status-pending">
          Loses: <span className="font-mono">{lost.join(', ')}</span>
        </p>
      )}
      {gained.length > 0 && (
        <p className="text-status-running">
          Gains: <span className="font-mono">{gained.join(', ')}</span>
        </p>
      )}
    </div>
  );
}

// DiffLines colorizes a unified diff: additions teal, removals muted-red,
// context dim. Hunk headers get a subtle accent.
function DiffLines({ diff }: { diff: string }) {
  return (
    <>
      {diff.split('\n').map((line, i) => {
        const c = line[0];
        const cls =
          c === '+'
            ? 'text-secondary'
            : c === '-'
              ? 'text-status-error/80'
              : line.startsWith('@@')
                ? 'text-primary/70'
                : 'text-text-muted';
        return (
          <div key={i} className={cls}>
            {line || ' '}
          </div>
        );
      })}
    </>
  );
}

function GateWarning({
  tone,
  icon,
  children,
}: {
  tone: 'error' | 'pending';
  icon: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <div
      role="alert"
      className={cn(
        'flex items-start gap-2.5 rounded-md px-3 py-3',
        tone === 'error'
          ? 'border border-status-error/40 bg-status-error/[0.06]'
          : 'border border-status-pending/40 bg-status-pending/[0.06]',
      )}
    >
      <span className="flex-shrink-0 mt-0.5">{icon}</span>
      <div className="text-[12px] text-text-secondary leading-relaxed">{children}</div>
    </div>
  );
}

export default AccessLensCommitGate;
