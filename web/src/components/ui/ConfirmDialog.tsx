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
}: ConfirmDialogProps) {
  const titleId = useId();
  const descId = useId();
  const cancelRef = useRef<HTMLButtonElement | null>(null);
  const confirmRef = useRef<HTMLButtonElement | null>(null);
  const initialFocusRef = autoFocus === 'confirm' ? confirmRef : cancelRef;
  const panelRef = useFocusTrap<HTMLDivElement>({
    active: isOpen,
    initialFocusRef,
  });

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
    if (!isOpen) return;
    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [isOpen, handleKeyDown]);

  if (!isOpen) return null;

  const confirmClasses =
    variant === 'danger'
      ? cn(
          'bg-status-error text-white',
          'hover:bg-status-error/90',
          'focus:outline-none focus:ring-2 focus:ring-status-error/40 focus:ring-offset-2 focus:ring-offset-background',
        )
      : cn(
          'bg-primary text-background',
          'hover:bg-primary-light',
          'focus:outline-none focus:ring-2 focus:ring-primary/40 focus:ring-offset-2 focus:ring-offset-background',
        );

  return (
    <div
      className={cn(
        'fixed inset-0 z-[60] animate-fade-in-scale',
        'bg-background/80 backdrop-blur-sm flex items-center justify-center',
      )}
    >
      <div className="absolute inset-0" onClick={onClose} />

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
          <button
            ref={cancelRef}
            type="button"
            onClick={onClose}
            className={cn(
              'px-3 py-1.5 text-xs rounded-lg transition-colors',
              'text-text-secondary hover:text-text-primary bg-surface-elevated hover:bg-surface-highlight',
              'focus:outline-none focus:ring-2 focus:ring-primary/30 focus:ring-offset-2 focus:ring-offset-background',
            )}
          >
            {cancelLabel}
          </button>
          <button
            ref={confirmRef}
            type="button"
            onClick={() => {
              onConfirm();
            }}
            className={cn(
              'px-3 py-1.5 text-xs font-medium rounded-lg transition-colors',
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
