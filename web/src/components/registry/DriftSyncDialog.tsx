import { useCallback, useEffect, useId } from 'react';
import { AlertTriangle } from 'lucide-react';
import { cn } from '../../lib/cn';
import { useFocusTrap } from '../../hooks/useFocusTrap';

interface DriftSyncDialogProps {
  isOpen: boolean;
  /** Dialog title, e.g. `Sync "owner/repo"`. */
  title: string;
  /** Names of the locally-edited skills a sync would otherwise overwrite. */
  driftedSkills: string[];
  busy?: boolean;
  /** Dismiss without syncing. */
  onCancel: () => void;
  /** Sync, skipping drifted skills (keeps local edits). */
  onSkip: () => void;
  /** Sync, overwriting drifted skills (a backup is written server-side). */
  onOverwrite: () => void;
}

/**
 * Three-way confirm shown before syncing a source that has locally-edited
 * skills. Mirrors ConfirmDialog's chrome but offers Cancel / keep-my-edits /
 * overwrite, since a plain confirm cannot express the skip-vs-overwrite choice.
 * Default focus lands on the safe "keep my edits" action.
 */
export function DriftSyncDialog({
  isOpen,
  title,
  driftedSkills,
  busy = false,
  onCancel,
  onSkip,
  onOverwrite,
}: DriftSyncDialogProps) {
  const titleId = useId();
  const descId = useId();
  const panelRef = useFocusTrap<HTMLDivElement>({ active: isOpen });

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
    if (!isOpen) return;
    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [isOpen, handleKeyDown]);

  if (!isOpen) return null;

  return (
    <div
      className={cn(
        'fixed inset-0 z-[60] animate-fade-in-scale',
        'bg-background/80 backdrop-blur-sm flex items-center justify-center',
      )}
    >
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
          <AlertTriangle size={15} className="text-amber-300" />
          {title}
        </h2>
        <div id={descId} className="text-xs text-text-muted space-y-2">
          <p>
            {driftedSkills.length === 1 ? 'This skill has' : 'These skills have'} local edits that a
            sync would overwrite:
          </p>
          <ul className="max-h-40 overflow-y-auto scrollbar-dark rounded-lg border border-border/40 bg-background/40 divide-y divide-border/20">
            {driftedSkills.map((name) => (
              <li key={name} className="px-3 py-1.5 font-mono text-[11px] text-text-secondary">
                {name}
              </li>
            ))}
          </ul>
        </div>
        <div className="flex flex-wrap justify-end gap-2 pt-2">
          <button
            type="button"
            onClick={onCancel}
            disabled={busy}
            className={cn(
              'px-3 py-1.5 text-xs rounded-lg transition-colors',
              'text-text-secondary hover:text-text-primary bg-surface-elevated hover:bg-surface-highlight',
              'focus:outline-none focus:ring-2 focus:ring-primary/30 focus:ring-offset-2 focus:ring-offset-background',
              busy && 'opacity-50 cursor-not-allowed',
            )}
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={onOverwrite}
            disabled={busy}
            className={cn(
              'px-3 py-1.5 text-xs font-medium rounded-lg transition-colors',
              'text-status-error bg-status-error/10 hover:bg-status-error/20 border border-status-error/30',
              'focus:outline-none focus:ring-2 focus:ring-status-error/40 focus:ring-offset-2 focus:ring-offset-background',
              busy && 'opacity-50 cursor-not-allowed',
            )}
          >
            Overwrite local edits
          </button>
          <button
            type="button"
            autoFocus
            onClick={onSkip}
            disabled={busy}
            className={cn(
              'px-3 py-1.5 text-xs font-medium rounded-lg transition-colors',
              'bg-primary text-background hover:bg-primary-light',
              'focus:outline-none focus:ring-2 focus:ring-primary/40 focus:ring-offset-2 focus:ring-offset-background',
              busy && 'opacity-50 cursor-not-allowed',
            )}
          >
            Keep my edits (skip these)
          </button>
        </div>
      </div>
    </div>
  );
}
