import { useCallback, useRef, useState } from 'react';
import { cn } from '../../lib/cn';

interface ResizeHandleProps {
  direction: 'horizontal' | 'vertical';
  onResize: (delta: number) => void;
  onResizeEnd?: () => void;
  className?: string;
}

export function ResizeHandle({ direction, onResize, onResizeEnd, className }: ResizeHandleProps) {
  const [isDragging, setIsDragging] = useState(false);
  const startPosRef = useRef(0);

  const handleMouseDown = useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault();
      setIsDragging(true);
      startPosRef.current = direction === 'horizontal' ? e.clientY : e.clientX;

      const handleMouseMove = (moveEvent: MouseEvent) => {
        const currentPos = direction === 'horizontal' ? moveEvent.clientY : moveEvent.clientX;
        const delta = startPosRef.current - currentPos;
        startPosRef.current = currentPos;
        onResize(delta);
      };

      const handleMouseUp = () => {
        setIsDragging(false);
        document.removeEventListener('mousemove', handleMouseMove);
        document.removeEventListener('mouseup', handleMouseUp);
        document.body.style.cursor = '';
        document.body.style.userSelect = '';
        onResizeEnd?.();
      };

      document.addEventListener('mousemove', handleMouseMove);
      document.addEventListener('mouseup', handleMouseUp);
      document.body.style.cursor = direction === 'horizontal' ? 'row-resize' : 'col-resize';
      document.body.style.userSelect = 'none';
    },
    [direction, onResize, onResizeEnd]
  );

  const isHorizontal = direction === 'horizontal';

  return (
    <div
      onMouseDown={handleMouseDown}
      className={cn(
        'group relative flex items-center justify-center select-none',
        'transition-colors duration-150',
        isHorizontal ? 'h-2 cursor-row-resize' : 'w-2 cursor-col-resize',
        'hover:bg-primary/5',
        isDragging && 'bg-primary/10',
        className
      )}
    >
      {/* Invisible hit area for easier grabbing */}
      <div
        className={cn(
          'absolute',
          isHorizontal ? 'inset-x-0 -inset-y-1' : 'inset-y-0 -inset-x-1'
        )}
      />

      {/* The visible handle line */}
      <div
        className={cn(
          'relative rounded-full transition-all duration-150',
          'bg-border/30',
          'group-hover:bg-primary/50',
          isDragging && 'bg-primary shadow-[0_0_8px_rgba(245,158,11,0.4)]',
          isHorizontal
            ? 'h-0.5 w-12 group-hover:w-16'
            : 'w-0.5 h-12 group-hover:h-16',
          isDragging && (isHorizontal ? 'w-20' : 'h-20')
        )}
      />

      {/* Grip dots - appear on hover */}
      <div
        className={cn(
          'absolute flex gap-0.5 transition-opacity duration-150',
          'opacity-0 group-hover:opacity-100',
          isDragging && 'opacity-100',
          isHorizontal ? 'flex-row' : 'flex-col'
        )}
      >
        {[0, 1, 2].map((i) => (
          <div
            key={i}
            className={cn(
              'w-1 h-1 rounded-full',
              'bg-text-muted/40',
              'group-hover:bg-primary/60',
              isDragging && 'bg-primary'
            )}
          />
        ))}
      </div>
    </div>
  );
}
