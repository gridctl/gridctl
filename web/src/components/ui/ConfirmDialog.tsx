import { useCallback, useEffect, useId, useRef, type ReactNode } from 'react';
import { cn } from '../../lib/cn';
import { useFocusTrap } from '../../hooks/useFocusTrap';

type Variant = 'default' | 'danger';

interface ConfirmDialogProps {
  isOpen: boolean;
  onClose: () => void;
  onConfirm: () => void;
  title: string;
  message: ReactNode;
  confirmLabel: ReactNode;
  cancelLabel?: string;
  variant?: Variant;
  /** Which button to focus on open. Defaults to 'cancel' for safety. */
  autoFocus?: 'cancel' | 'confirm';
  /** Disables the confirm button while a mutation is in flight. */
  busy?: boolean;
  /**
   * Omits the cancel button for a decision with no safe way back. Escape and
   * the backdrop then do nothing, and focus starts on the confirm button.
   */
  hideCancel?: boolean;
}

export function ConfirmDialog({
  isOpen,
  onClose,
  onConfirm,
  title,
  message,
  confirmLabel,
  cancelLabel = 'Cancel',
  variant = 'default',
  autoFocus = 'cancel',
  busy = false,
  hideCancel = false,
}: ConfirmDialogProps) {
  const titleId = useId();
  const descId = useId();
  const cancelRef = useRef<HTMLButtonElement | null>(null);
  const confirmRef = useRef<HTMLButtonElement | null>(null);
  const initialFocusRef = autoFocus === 'confirm' || hideCancel ? confirmRef : cancelRef;
  const panelRef = useFocusTrap<HTMLDivElement>({
    active: isOpen,
    initialFocusRef,
  });

  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.stopPropagation();
        if (!hideCancel) onClose();
      }
    },
    [onClose, hideCancel],
  );

  useEffect(() => {
    if (!isOpen) return;
    // Capture phase, matching useDismiss: one Escape closes this confirm and
    // must not also reach a host Modal's bubble-phase listener on document.
    // stopPropagation is enough for that; it does not silence other capture
    // listeners on document (useDismiss, AccessLens).
    document.addEventListener('keydown', handleKeyDown, { capture: true });
    return () => document.removeEventListener('keydown', handleKeyDown, { capture: true });
  }, [isOpen, handleKeyDown]);

  if (!isOpen) return null;

  const confirmClasses =
    variant === 'danger'
      ? cn(
          'bg-danger text-white',
          'hover:bg-danger-hover',
        )
      : cn(
          'bg-primary text-background',
          'hover:bg-primary-light',
        );

  return (
    <div
      className={cn(
        'fixed inset-0 z-modal animate-fade-in-scale',
        'bg-background/80 backdrop-blur-sm flex items-center justify-center',
      )}
    >
      <div className="absolute inset-0" onClick={hideCancel ? undefined : onClose} />

      <div
        ref={panelRef}
        role="alertdialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={descId}
        className={cn(
          'relative glass-panel-elevated rounded-xl p-5 max-w-sm w-full mx-4 space-y-3 shadow-lg',
        )}
      >
        <h2
          id={titleId}
          className="text-sm font-semibold text-text-primary"
        >
          {title}
        </h2>
        <div id={descId} className="text-xs text-text-muted space-y-2">
          {message}
        </div>
        <div className="flex justify-end gap-2 pt-2">
          {!hideCancel && (
            <button
              ref={cancelRef}
              type="button"
              onClick={onClose}
              className={cn(
                'px-3 py-1.5 text-xs rounded-lg transition-colors',
                'text-text-secondary hover:text-text-primary bg-surface-elevated hover:bg-surface-highlight',
              )}
            >
              {cancelLabel}
            </button>
          )}
          <button
            ref={confirmRef}
            type="button"
            onClick={() => {
              onConfirm();
            }}
            disabled={busy}
            className={cn(
              'px-3 py-1.5 text-xs font-medium rounded-lg transition-colors',
              'disabled:opacity-50 disabled:cursor-not-allowed',
              confirmClasses,
            )}
          >
            {confirmLabel}
          </button>
        </div>
      </div>
    </div>
  );
}
