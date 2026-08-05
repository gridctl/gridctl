import { useCallback, useEffect, useId, useRef, useState } from 'react';
import { AlertTriangle, Trash2 } from 'lucide-react';
import { cn } from '../../../lib/cn';
import { useFocusTrap } from '../../../hooks/useFocusTrap';
import { showToast } from '../../ui/Toast';
import { removePack, type PackRow } from '../../../lib/api';
import { packKindLabel, PACK_KIND_ORDER } from './packModel';

interface PackRemoveDialogProps {
  packName: string;
  onClose: () => void;
  /** stillExists: a non-force removal kept locally edited members, so
   *  the trimmed pack record remains. */
  onRemoved: (stillExists: boolean) => void;
}

/**
 * Cascade removal with an upfront preview: the dialog opens on the
 * server's dry run and shows two honest lists, what will be removed
 * (grouped by kind) and what a plain remove keeps because it was
 * locally edited (with the engine's remediation verbatim). The safe
 * plain remove is the focused default; removing the edits too is a
 * separate red action with its own confirmation step.
 */
export function PackRemoveDialog({ packName, onClose, onRemoved }: PackRemoveDialogProps) {
  const titleId = useId();
  const descId = useId();
  const panelRef = useFocusTrap<HTMLDivElement>({ active: true });

  const [preview, setPreview] = useState<{ willRemove: PackRow[]; kept: PackRow[] } | null>(null);
  const [busy, setBusy] = useState(false);
  const [confirmingForce, setConfirmingForce] = useState(false);

  // The dry run fires once per pack; a parent re-render (inline onClose
  // identity) must not cancel and re-issue it mid-decision.
  const onCloseRef = useRef(onClose);
  useEffect(() => {
    onCloseRef.current = onClose;
  }, [onClose]);
  useEffect(() => {
    let cancelled = false;
    removePack(packName, { dryRun: true })
      .then((doc) => {
        if (cancelled) return;
        const willRemove = doc.rows.filter((r) => r.action === 'would-remove');
        const kept = doc.rows.filter((r) => r.action === 'skipped-drift');
        setPreview({ willRemove, kept });
      })
      .catch((err) => {
        if (cancelled) return;
        showToast('error', err instanceof Error ? err.message : 'Preview failed');
        onCloseRef.current();
      });
    return () => {
      cancelled = true;
    };
  }, [packName]);

  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.stopPropagation();
        onClose();
      }
    },
    [onClose],
  );
  useEffect(() => {
    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [handleKeyDown]);

  const runRemove = useCallback(
    async (force: boolean) => {
      if (busy) return;
      setBusy(true);
      try {
        const doc = await removePack(packName, force ? { force: true } : undefined);
        const kept = doc.kept ?? [];
        if (kept.length > 0) {
          // Aftermath honesty: the trimmed record remains; never say
          // "removed" while it does.
          showToast(
            'warning',
            `${packName} still exists with ${kept.length} kept member${kept.length === 1 ? '' : 's'} (locally edited). Adopt the edits or remove with overwrite to finish.`,
          );
        } else {
          showToast('success', `Pack "${packName}" removed`);
        }
        onRemoved(kept.length > 0);
      } catch (err) {
        showToast('error', err instanceof Error ? err.message : 'Remove failed');
        setBusy(false);
      }
    },
    [busy, packName, onRemoved],
  );

  const onlyKept = preview !== null && preview.willRemove.length === 0 && preview.kept.length > 0;

  return (
    <div className="fixed inset-0 z-[60] animate-fade-in-scale bg-background/80 backdrop-blur-sm flex items-center justify-center">
      <div className="absolute inset-0" onClick={onClose} />
      <div
        ref={panelRef}
        role="alertdialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={descId}
        className="relative glass-panel-elevated rounded-xl p-5 max-w-lg w-full mx-4 space-y-3 shadow-lg"
      >
        <h2 id={titleId} className="flex items-center gap-2 text-sm font-semibold text-text-primary">
          <Trash2 size={15} className="text-red-400" />
          Remove pack {packName}
        </h2>

        {preview === null ? (
          <p className="text-xs text-text-muted">Previewing the cascade…</p>
        ) : (
          <div id={descId} className="text-xs text-text-muted space-y-3">
            {preview.willRemove.length > 0 && (
              <div>
                <p className="mb-1.5">
                  {preview.willRemove.length} resource
                  {preview.willRemove.length === 1 ? '' : 's'} will be removed (projections,
                  wiring records, then registry entries):
                </p>
                <RemoveList rows={preview.willRemove} />
              </div>
            )}
            {preview.kept.length > 0 && (
              <div>
                <p className="mb-1.5 flex items-center gap-1.5 text-status-pending">
                  <AlertTriangle size={12} aria-hidden="true" />
                  {preview.kept.length} kept (locally edited); a plain remove leaves them
                  imported:
                </p>
                <ul className="max-h-32 overflow-y-auto scrollbar-dark rounded-lg border border-border/40 bg-background/40 divide-y divide-border/20">
                  {preview.kept.map((r, i) => (
                    <li key={`${r.kind}:${r.name}:${i}`} className="px-3 py-1.5">
                      <span className="font-mono text-[11px] text-text-secondary">
                        {r.kind}/{r.name}
                      </span>
                      {r.remediation && (
                        <p className="text-[11px] text-text-muted mt-0.5">{r.remediation}</p>
                      )}
                    </li>
                  ))}
                </ul>
              </div>
            )}
            {onlyKept && (
              <p>
                Every remaining resource was locally edited, so a plain remove has nothing to
                do. Adopt the edits first, or remove them with overwrite below.
              </p>
            )}
          </div>
        )}

        {confirmingForce ? (
          <div className="rounded-lg border border-status-error/30 bg-status-error/5 p-3 text-xs text-text-muted space-y-2">
            <p>
              Removing with overwrite deletes the locally edited projections too. This cannot
              be undone.
            </p>
            <div className="flex justify-end gap-2">
              <button
                type="button"
                onClick={() => setConfirmingForce(false)}
                disabled={busy}
                className="px-3 py-1.5 text-xs rounded-lg text-text-secondary hover:text-text-primary bg-surface-elevated hover:bg-surface-highlight transition-colors"
              >
                Back
              </button>
              <button
                type="button"
                onClick={() => void runRemove(true)}
                disabled={busy}
                className={cn(
                  'px-3 py-1.5 text-xs font-medium rounded-lg transition-colors',
                  'text-status-error bg-status-error/10 hover:bg-status-error/20 border border-status-error/30',
                  busy && 'opacity-50 cursor-not-allowed',
                )}
              >
                {busy ? 'Removing…' : 'Remove and overwrite local edits'}
              </button>
            </div>
          </div>
        ) : (
          <div className="flex flex-wrap justify-end gap-2 pt-1">
            <button
              type="button"
              onClick={onClose}
              disabled={busy}
              className="px-3 py-1.5 text-xs rounded-lg text-text-secondary hover:text-text-primary bg-surface-elevated hover:bg-surface-highlight transition-colors"
            >
              Cancel
            </button>
            {preview !== null && preview.kept.length > 0 && (
              <button
                type="button"
                onClick={() => setConfirmingForce(true)}
                disabled={busy || preview === null}
                className="px-3 py-1.5 text-xs rounded-lg text-status-error border border-status-error/30 hover:bg-status-error/10 transition-colors disabled:opacity-50"
              >
                Remove and overwrite local edits
              </button>
            )}
            <button
              type="button"
              autoFocus
              onClick={() => void runRemove(false)}
              disabled={busy || preview === null || onlyKept}
              className={cn(
                'px-3 py-1.5 text-xs font-medium rounded-lg transition-colors',
                'bg-primary text-background hover:bg-primary-light',
                (busy || preview === null || onlyKept) && 'opacity-50 cursor-not-allowed',
              )}
            >
              {busy ? 'Removing…' : 'Remove (keep local edits)'}
            </button>
          </div>
        )}
      </div>
    </div>
  );
}

/** The will-remove rows, grouped by kind in manifest order. */
function RemoveList({ rows }: { rows: PackRow[] }) {
  const byKind = new Map<string, string[]>();
  for (const r of rows) {
    const g = byKind.get(r.kind);
    if (g) g.push(r.name);
    else byKind.set(r.kind, [r.name]);
  }
  return (
    <ul className="max-h-40 overflow-y-auto scrollbar-dark rounded-lg border border-border/40 bg-background/40 divide-y divide-border/20">
      {PACK_KIND_ORDER.filter((k) => byKind.has(k)).map((kind) => (
        <li key={kind} className="px-3 py-1.5">
          <span className="text-[10px] uppercase tracking-wider text-text-muted">{packKindLabel(kind)}</span>
          <p className="font-mono text-[11px] text-text-secondary mt-0.5">
            {(byKind.get(kind) ?? []).join(', ')}
          </p>
        </li>
      ))}
    </ul>
  );
}
