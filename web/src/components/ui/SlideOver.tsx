import { useCallback, useEffect, useId, type ReactNode } from 'react';
import { X } from 'lucide-react';
import { cn } from '../../lib/cn';

interface SlideOverProps {
  isOpen: boolean;
  onClose: () => void;
  title: string;
  children: ReactNode;
  /** Width of the panel. Defaults to a comfortable editing column. */
  widthClass?: string;
  /** Accessible description id wiring, if the body provides one. */
  describedById?: string;
  /** Accessible name of the close button. */
  closeLabel?: string;
}

/**
 * SlideOver is a right-anchored panel that sits beside the content rather than
 * over it. Unlike ui/Modal it deliberately has NO full-viewport backdrop and NO
 * focus trap: the Stack canvas behind it must stay pannable, selectable, and
 * clickable (canvas server clicks edit the same draft the slide-over does), so a
 * trap or backdrop would defeat the entire interaction model. It is keyboard
 * operable on its own (Escape closes, content is tabbable) and labels itself as
 * a non-modal dialog.
 *
 * Built on the existing slide-in-right keyframe — no motion library.
 */
export function SlideOver({
  isOpen,
  onClose,
  title,
  children,
  widthClass,
  describedById,
  closeLabel = 'Close access editor',
}: SlideOverProps) {
  const titleId = useId();

  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return;
      const tag = (e.target as HTMLElement)?.tagName;
      if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') {
        (e.target as HTMLElement).blur();
        return;
      }
      onClose();
    },
    [onClose],
  );

  useEffect(() => {
    if (!isOpen) return;
    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [isOpen, handleKeyDown]);

  if (!isOpen) return null;

  return (
    <div
      role="dialog"
      aria-labelledby={titleId}
      aria-describedby={describedById}
      className={cn(
        // Anchored to the right edge, below the app header band. Pointer events
        // are confined to the panel so the canvas stays live everywhere else.
        'absolute top-3 bottom-3 right-3 z-30 flex flex-col',
        'glass-panel-elevated rounded-xl shadow-bevel animate-slide-in-right',
        widthClass ?? 'w-[340px]',
      )}
    >
      <div className="flex items-center justify-between px-4 py-3 border-b border-border/30 flex-shrink-0">
        <h2 id={titleId} className="text-sm font-medium text-text-primary">
          {title}
        </h2>
        <button
          onClick={onClose}
          aria-label={closeLabel}
          className="p-1.5 rounded-lg hover:bg-surface-highlight transition-colors"
        >
          <X size={14} className="text-text-muted" />
        </button>
      </div>
      <div className="flex-1 overflow-y-auto scrollbar-dark min-h-0">{children}</div>
    </div>
  );
}

export default SlideOver;
