import { useCallback, useEffect, useId, useState } from 'react';
import { AlertTriangle, Upload } from 'lucide-react';
import { cn } from '../../../lib/cn';
import { useFocusTrap } from '../../../hooks/useFocusTrap';
import { showToast } from '../../ui/Toast';
import {
  addPack,
  PackFindingsError,
  previewPack,
  type PackOrigin,
  type PackPreview,
  type PackPreviewResource,
} from '../../../lib/api';

interface PackUpdateDialogProps {
  packName: string;
  origin: PackOrigin;
  onClose: () => void;
  onUpdated: () => void;
}

/**
 * Update from origin: re-import against the stored repo and ref, which
 * is the documented update path for packs (changed upstream rules
 * refresh, locally edited rules are left alone, the selection
 * re-resolves). Opens on a fresh preview of the origin so the user sees
 * the current selection before committing; findings gate on one
 * pack-wide trust acknowledgment, mirroring the CLI's single --trust.
 */
export function PackUpdateDialog({ packName, origin, onClose, onUpdated }: PackUpdateDialogProps) {
  const titleId = useId();
  const descId = useId();
  const trustId = useId();
  const panelRef = useFocusTrap<HTMLDivElement>({ active: true });

  const [preview, setPreview] = useState<PackPreview | null>(null);
  const [previewError, setPreviewError] = useState<string | null>(null);
  const [trustAck, setTrustAck] = useState(false);
  const [busy, setBusy] = useState(false);
  const [serverFindings, setServerFindings] = useState<PackPreviewResource[] | null>(null);

  useEffect(() => {
    let cancelled = false;
    previewPack({ repo: origin.repo, ref: origin.ref })
      .then((p) => {
        if (!cancelled) setPreview(p);
      })
      .catch((err) => {
        if (!cancelled) setPreviewError(err instanceof Error ? err.message : 'Preview failed');
      });
    return () => {
      cancelled = true;
    };
  }, [origin.repo, origin.ref]);

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

  const previewFlagged = preview
    ? [...preview.skills, ...preview.agents, ...preview.rules].filter((r) => (r.findings ?? []).length > 0)
    : [];
  const flagged = serverFindings ?? previewFlagged;
  const needsTrust = serverFindings !== null || flagged.some((r) => r.blocking);

  const handleUpdate = useCallback(async () => {
    if (busy) return;
    setBusy(true);
    try {
      const res = await addPack({
        repo: origin.repo,
        ref: origin.ref,
        trust: needsTrust && trustAck,
      });
      for (const note of res.notes ?? []) showToast('success', note);
      const skipped = res.doc.skipped ?? [];
      showToast(
        skipped.length > 0 ? 'warning' : 'success',
        skipped.length > 0
          ? `Updated ${packName}; ${skipped.length} skipped (locally modified or refused)`
          : `Updated ${packName} from its origin`,
      );
      onUpdated();
    } catch (err) {
      if (err instanceof PackFindingsError) {
        // Re-arm the trust gate with the server's findings rather than
        // letting every retry repeat the same refusal.
        setServerFindings(err.findings);
        setTrustAck(false);
        showToast('error', err.message);
      } else {
        showToast('error', err instanceof Error ? err.message : 'Update failed');
      }
      setBusy(false);
    }
  }, [busy, origin.repo, origin.ref, needsTrust, trustAck, packName, onUpdated]);

  return (
    <div className="fixed inset-0 z-[60] animate-fade-in-scale bg-background/80 backdrop-blur-sm flex items-center justify-center">
      <div className="absolute inset-0" onClick={onClose} />
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={descId}
        className="relative glass-panel-elevated rounded-xl p-5 max-w-lg w-full mx-4 space-y-3 shadow-lg"
      >
        <h2 id={titleId} className="flex items-center gap-2 text-sm font-semibold text-text-primary">
          <Upload size={15} className="text-primary" />
          Update {packName} from origin
        </h2>
        <div id={descId} className="text-xs text-text-muted space-y-2">
          <p className="font-mono text-[11px] truncate" title={origin.repo}>
            {origin.repo}
            {origin.ref ? `@${origin.ref}` : ''}
          </p>
          {previewError && <p className="text-status-error">{previewError}</p>}
          {!preview && !previewError && <p>Resolving the manifest…</p>}
          {preview && (
            <>
              <p>
                Re-importing resolves the manifest again: rules changed upstream refresh,
                rules you edited locally are skipped and reported, and the skill and agent
                selection updates.
              </p>
              <p className="font-mono text-[11px]">
                {preview.skills.length} skills, {preview.agents.length} agents,{' '}
                {preview.rules.length} rules{preview.wiring ? ', wiring' : ''}
                {(preview.unresolved ?? []).length > 0 &&
                  ` (${(preview.unresolved ?? []).length} unresolved)`}
              </p>
              {flagged.length > 0 && (
                <div className="rounded-lg border border-status-pending/30 bg-status-pending/5 p-2.5 space-y-1.5">
                  <p className="flex items-center gap-1.5 text-status-pending">
                    <AlertTriangle size={12} aria-hidden="true" />
                    Security findings on {flagged.map((f) => `${f.kind}/${f.name}`).join(', ')}
                  </p>
                  {needsTrust ? (
                    <label htmlFor={trustId} className="flex items-start gap-2 cursor-pointer text-text-secondary">
                      <input
                        id={trustId}
                        type="checkbox"
                        checked={trustAck}
                        onChange={(e) => setTrustAck(e.target.checked)}
                        className="mt-0.5"
                      />
                      <span>Update this pack anyway (accept the findings)</span>
                    </label>
                  ) : (
                    <p>These findings do not block the update; they stay visible so nothing refreshes unseen.</p>
                  )}
                </div>
              )}
            </>
          )}
        </div>
        <div className="flex justify-end gap-2 pt-1">
          <button
            type="button"
            onClick={onClose}
            disabled={busy}
            className="px-3 py-1.5 text-xs rounded-lg text-text-secondary hover:text-text-primary bg-surface-elevated hover:bg-surface-highlight transition-colors"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={() => void handleUpdate()}
            disabled={busy || preview === null || (needsTrust && !trustAck)}
            className={cn(
              'px-3 py-1.5 text-xs font-medium rounded-lg transition-colors',
              'bg-primary text-background hover:bg-primary-light',
              (busy || preview === null || (needsTrust && !trustAck)) && 'opacity-50 cursor-not-allowed',
            )}
          >
            {busy ? 'Updating…' : 'Update from origin'}
          </button>
        </div>
      </div>
    </div>
  );
}
