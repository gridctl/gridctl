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
        isHorizontal ? 'h-1.5 cursor-row-resize' : 'w-1.5 cursor-col-resize',
        'hover:bg-primary/8',
        isDragging && 'bg-primary/10',
        className
      )}
    >
      {/* Hit area — generous for easy grabbing */}
      <div
        className={cn(
          'absolute',
          isHorizontal ? 'inset-x-0 -inset-y-1.5' : 'inset-y-0 -inset-x-1.5'
        )}
      />

      {/* Full-length edge line — always visible as a subtle border */}
      <div
        className={cn(
          'absolute transition-colors duration-150',
          'bg-border/40',
          'group-hover:bg-primary/30',
          isDragging && 'bg-primary/50',
          isHorizontal
            ? 'h-px inset-x-0 top-0'
            : 'w-px inset-y-0 left-0'
        )}
      />

      {/* Grip dots — subtle at rest, prominent on hover */}
      <div
        className={cn(
          'absolute flex gap-1 transition-all duration-150',
          'opacity-40 group-hover:opacity-100',
          isDragging && 'opacity-100',
          isHorizontal ? 'flex-row' : 'flex-col'
        )}
      >
        {[0, 1, 2].map((i) => (
          <div
            key={i}
            className={cn(
              'rounded-full transition-all duration-150',
              isHorizontal ? 'w-1.5 h-0.5' : 'w-0.5 h-1.5',
              'bg-text-muted/30',
              'group-hover:bg-primary/70',
              isDragging && 'bg-primary'
            )}
          />
        ))}
      </div>
    </div>
  );
}
