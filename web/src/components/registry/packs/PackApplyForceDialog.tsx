import { useCallback, useEffect, useId } from 'react';
import { AlertTriangle } from 'lucide-react';
import { cn } from '../../../lib/cn';
import { useFocusTrap } from '../../../hooks/useFocusTrap';
import type { PackRow } from '../../../lib/api';

interface PackApplyForceDialogProps {
  packName: string;
  /** The skipped-drift rows from the plain apply that just ran. */
  driftedRows: PackRow[];
  busy?: boolean;
  onCancel: () => void;
  /** Re-apply with force: overwrites the listed local edits (after a
   *  server-side backup). */
  onOverwrite: () => void;
}

/**
 * The force follow-up after a plain apply skipped drifted resources.
 * Safe default is keeping the edits (Cancel, focused); the overwrite is
 * a separate red action that names the backup behavior. Foreign-pack
 * skips never reach this dialog: force does not apply to them.
 */
export function PackApplyForceDialog({
  packName,
  driftedRows,
  busy = false,
  onCancel,
  onOverwrite,
}: PackApplyForceDialogProps) {
  const titleId = useId();
  const descId = useId();
  const panelRef = useFocusTrap<HTMLDivElement>({ active: true });

  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.stopPropagation();
        onCancel();
      }
    },
    [onCancel],
  );
  useEffect(() => {
    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [handleKeyDown]);

  return (
    <div className="fixed inset-0 z-[60] animate-fade-in-scale bg-background/80 backdrop-blur-sm flex items-center justify-center">
      <div className="absolute inset-0" onClick={onCancel} />
      <div
        ref={panelRef}
        role="alertdialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={descId}
        className="relative glass-panel-elevated rounded-xl p-5 max-w-md w-full mx-4 space-y-3 shadow-lg"
      >
        <h2 id={titleId} className="flex items-center gap-2 text-sm font-semibold text-text-primary">
          <AlertTriangle size={15} className="text-status-pending" />
          Some resources kept local edits
        </h2>
        <div id={descId} className="text-xs text-text-muted space-y-2">
          <p>
            Applying {packName} skipped {driftedRows.length === 1 ? 'a resource' : 'resources'}{' '}
            whose projected copies were hand-edited:
          </p>
          <ul className="max-h-40 overflow-y-auto scrollbar-dark rounded-lg border border-border/40 bg-background/40 divide-y divide-border/20">
            {driftedRows.map((r, i) => (
              <li key={`${r.kind}:${r.name}:${r.client ?? ''}:${i}`} className="px-3 py-1.5 font-mono text-[11px] text-text-secondary">
                {r.kind}/{r.name}
                {r.client ? ` (${r.client})` : ''}
              </li>
            ))}
          </ul>
          <p>
            Overwriting replaces those edits with the pack's content; a backup is written
            first. Keeping them leaves the projections as you edited them.
          </p>
        </div>
        <div className="flex flex-wrap justify-end gap-2 pt-2">
          <button
            type="button"
            onClick={onOverwrite}
            disabled={busy}
            className={cn(
              'px-3 py-1.5 text-xs font-medium rounded-lg transition-colors',
              'text-status-error bg-status-error/10 hover:bg-status-error/20 border border-status-error/30',
              busy && 'opacity-50 cursor-not-allowed',
            )}
          >
            Overwrite local edits
          </button>
          <button
            type="button"
            autoFocus
            onClick={onCancel}
            disabled={busy}
            className={cn(
              'px-3 py-1.5 text-xs font-medium rounded-lg transition-colors',
              'bg-primary text-background hover:bg-primary-light',
              busy && 'opacity-50 cursor-not-allowed',
            )}
          >
            Keep my edits
          </button>
        </div>
      </div>
    </div>
  );
}
