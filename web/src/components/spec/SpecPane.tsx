import { useCallback, useEffect, useRef } from 'react';
import { X } from 'lucide-react';
import { useSpecStore } from '../../stores/useSpecStore';
import { useEscapeLayer } from '../../hooks/useEscapeLayer';
import { ResizeHandle } from '../ui/ResizeHandle';
import { SpecTab } from './SpecTab';

interface SpecPaneProps {
  onClose: () => void;
  /** Drag delta from the left-edge handle; StackWorkspace owns the width. */
  onResize?: (delta: number) => void;
}

// The Stack workspace's spec column (the spec IS the stack definition, so it
// sits beside the canvas, which refits to the narrower column). Opened via
// /stack?spec=1 so the status-bar chip and palette command deep-link it.
// Escape closes it unless a field has focus or the spec diff modal is open.
export function SpecPane({ onClose, onResize }: SpecPaneProps) {
  const rootRef = useRef<HTMLElement>(null);

  // Land keyboard and screen reader users in the pane they opened.
  useEffect(() => {
    rootRef.current?.focus();
  }, []);

  // A non-modal Escape layer, as the slide-over was: any open modal outranks it.
  const onEscape = useCallback(() => {
    if (useSpecStore.getState().diffModalOpen) return;
    onClose();
  }, [onClose]);
  useEscapeLayer({ active: true, onEscape, modal: false, ref: rootRef });

  return (
    <aside
      ref={rootRef}
      tabIndex={-1}
      aria-label="Stack spec"
      className="flex flex-row overflow-hidden bg-surface-elevated border-l border-border shadow-pane-left"
    >
      {onResize && (
        <ResizeHandle direction="vertical" onResize={onResize} className="flex-shrink-0" />
      )}
      <div className="flex flex-1 min-w-0 flex-col overflow-hidden">
        <div className="flex items-center justify-between px-4 py-3 border-b border-border/30 flex-shrink-0">
          <h2 className="text-sm font-medium text-text-primary">Spec</h2>
          <button
            type="button"
            onClick={onClose}
            aria-label="Close spec pane"
            className="p-1.5 rounded-lg hover:bg-surface-highlight transition-colors focus-visible:outline-2 focus-visible:outline-primary"
          >
            <X size={14} className="text-text-muted" />
          </button>
        </div>
        <div className="flex-1 min-h-0">
          <SpecTab />
        </div>
      </div>
    </aside>
  );
}
