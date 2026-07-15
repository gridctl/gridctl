import { GripVertical } from 'lucide-react';
import { cn } from '../../lib/cn';

/** The visible divider between split panes: grip glyph + hairline. */
export function SplitPaneHandle({
  onMouseDown,
  isDragging,
}: {
  onMouseDown: (e: React.MouseEvent) => void;
  isDragging: boolean;
}) {
  return (
    <div
      onMouseDown={onMouseDown}
      className={cn(
        'relative flex items-center justify-center cursor-col-resize select-none',
        'w-2 hover:bg-primary/5 transition-colors duration-150',
        isDragging && 'bg-primary/10',
      )}
    >
      {/* Hit area */}
      <div className="absolute inset-y-0 -inset-x-2" />
      {/* Visible grip — subtle at rest, fully visible on hover/drag so the
          resize affordance is discoverable without a blind mouse-over */}
      <div
        className={cn(
          'flex flex-col gap-0.5 transition-opacity duration-150',
          'opacity-30 group-hover/split:opacity-100',
          isDragging && 'opacity-100',
        )}
      >
        <GripVertical
          size={12}
          className={cn(
            'text-text-muted transition-colors',
            isDragging ? 'text-primary' : 'hover:text-primary/60',
          )}
        />
      </div>
      {/* Visible line */}
      <div
        className={cn(
          'absolute top-1/2 -translate-y-1/2 w-px h-12 rounded-full transition-all duration-150',
          'bg-border/30',
          isDragging && 'bg-primary h-20 shadow-[0_0_8px_rgba(245,158,11,0.4)]',
        )}
      />
    </div>
  );
}
