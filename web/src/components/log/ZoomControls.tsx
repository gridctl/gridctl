import { Minus, Plus } from 'lucide-react';
import { cn } from '../../lib/cn';

interface ZoomControlsProps {
  fontSize: number;
  onZoomIn: () => void;
  onZoomOut: () => void;
  onReset: () => void;
  isMin: boolean;
  isMax: boolean;
  isDefault: boolean;
}

export function ZoomControls({
  fontSize,
  onZoomIn,
  onZoomOut,
  onReset,
  isMin,
  isMax,
  isDefault,
}: ZoomControlsProps) {
  return (
    <div className="flex items-center gap-0.5 rounded-md border border-border/50 bg-surface-elevated/60 overflow-hidden">
      <button
        onClick={onZoomOut}
        disabled={isMin}
        title="Decrease font size (Ctrl+Scroll down)"
        className={cn(
          'p-1 transition-all duration-200',
          'hover:bg-surface-highlight hover:text-text-primary',
          'disabled:opacity-30 disabled:cursor-not-allowed',
          'text-text-muted'
        )}
      >
        <Minus size={12} />
      </button>

      <button
        onClick={onReset}
        disabled={isDefault}
        title="Reset font size"
        className={cn(
          'px-1.5 py-1 text-[10px] font-mono tabular-nums transition-all duration-200 min-w-[32px] text-center',
          isDefault
            ? 'text-text-muted'
            : 'text-primary hover:bg-surface-highlight cursor-pointer'
        )}
      >
        {fontSize}px
      </button>

      <button
        onClick={onZoomIn}
        disabled={isMax}
        title="Increase font size (Ctrl+Scroll up)"
        className={cn(
          'p-1 transition-all duration-200',
          'hover:bg-surface-highlight hover:text-text-primary',
          'disabled:opacity-30 disabled:cursor-not-allowed',
          'text-text-muted'
        )}
      >
        <Plus size={12} />
      </button>
    </div>
  );
}
