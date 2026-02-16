import { useState, useEffect, useCallback, type ReactNode } from 'react';
import { X, Maximize2, Minimize2, ExternalLink } from 'lucide-react';
import { cn } from '../../lib/cn';

type ModalSize = 'default' | 'wide' | 'full';

interface ModalProps {
  isOpen: boolean;
  onClose: () => void;
  title: string;
  children: ReactNode;
  /** Allow user to toggle between default and wide sizes */
  expandable?: boolean;
  /** Callback to pop out into a new window */
  onPopout?: () => void;
  /** Disable popout button (e.g., already detached) */
  popoutDisabled?: boolean;
  /** Force a specific size (overrides internal expand toggle) */
  size?: ModalSize;
  /** Flush mode: panel fills the entire viewport (for detached windows) */
  flush?: boolean;
}

const sizeClasses: Record<ModalSize, string> = {
  default: 'max-w-lg',
  wide: 'max-w-3xl',
  full: 'max-w-5xl',
};

export function Modal({
  isOpen,
  onClose,
  title,
  children,
  expandable,
  onPopout,
  popoutDisabled,
  size: forcedSize,
  flush,
}: ModalProps) {
  const [expanded, setExpanded] = useState(false);

  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return;
      const tag = (e.target as HTMLElement)?.tagName;
      if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') {
        // Blur the field instead of closing the modal
        (e.target as HTMLElement).blur();
        return;
      }
      onClose();
    },
    [onClose],
  );

  useEffect(() => {
    if (isOpen) {
      document.addEventListener('keydown', handleKeyDown);
      return () => document.removeEventListener('keydown', handleKeyDown);
    }
  }, [isOpen, handleKeyDown]);

  // Reset expanded state when modal closes
  useEffect(() => {
    if (!isOpen) setExpanded(false);
  }, [isOpen]);

  if (!isOpen) return null;

  const currentSize = forcedSize ?? (expanded ? 'wide' : 'default');

  return (
    <div
      className={cn(
        'fixed inset-0 z-50 animate-fade-in-scale',
        flush
          ? 'bg-background flex flex-col'
          : 'bg-background/80 backdrop-blur-sm flex items-center justify-center',
      )}
    >
      {/* Backdrop click (not in flush mode - panel fills viewport) */}
      {!flush && <div className="absolute inset-0" onClick={onClose} />}

      {/* Panel */}
      <div
        className={cn(
          'relative flex flex-col',
          flush
            ? 'flex-1 min-h-0 bg-surface-elevated'
            : cn(
                'glass-panel-elevated rounded-xl w-full mx-4 max-h-[85vh] shadow-lg',
                'transition-[max-width] duration-300 ease-out',
                sizeClasses[currentSize],
              ),
        )}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-border/30 flex-shrink-0">
          <h2 className="text-sm font-medium text-text-primary">{title}</h2>
          <div className="flex items-center gap-1">
            {expandable && (
              <button
                onClick={() => setExpanded(!expanded)}
                title={expanded ? 'Compact view' : 'Expanded view'}
                className="p-1.5 rounded-lg hover:bg-surface-highlight transition-all duration-200 group"
              >
                {expanded ? (
                  <Minimize2 size={14} className="text-text-muted group-hover:text-primary transition-colors" />
                ) : (
                  <Maximize2 size={14} className="text-text-muted group-hover:text-primary transition-colors" />
                )}
              </button>
            )}
            {onPopout && (
              <button
                onClick={onPopout}
                disabled={popoutDisabled}
                title="Open in new window"
                className={cn(
                  'p-1.5 rounded-lg transition-all duration-200 group',
                  'hover:bg-primary/10 hover:shadow-[0_0_12px_rgba(245,158,11,0.15)]',
                  'disabled:opacity-40 disabled:cursor-not-allowed disabled:pointer-events-none',
                )}
              >
                <ExternalLink
                  size={14}
                  className={cn(
                    'text-text-muted transition-all duration-200',
                    'group-hover:text-primary group-hover:scale-110 group-hover:-translate-y-px group-hover:translate-x-px',
                  )}
                />
              </button>
            )}
            <button
              onClick={onClose}
              className="p-1.5 rounded-lg hover:bg-surface-highlight transition-colors"
            >
              <X size={14} className="text-text-muted" />
            </button>
          </div>
        </div>

        {/* Scrollable content */}
        <div className="flex-1 overflow-y-auto scrollbar-dark px-6 py-4 min-h-0">
          {children}
        </div>
      </div>
    </div>
  );
}
