import { cn } from '../../lib/cn';

interface SeparatorBodyProps {
  orientation: 'vertical' | 'horizontal';
}

/**
 * Visual body of a react-resizable-panels Separator; mirrors
 * `components/ui/ResizeHandle.tsx` but drops the mouse-event logic since
 * RRP's Separator owns pointer/keyboard interaction. The parent Separator
 * must carry `group/separator relative` plus `w-1.5` (vertical) or `h-1.5`
 * (horizontal).
 */
export function SeparatorBody({ orientation }: SeparatorBodyProps) {
  const isVertical = orientation === 'vertical';
  return (
    <div
      className={cn(
        'absolute inset-0 flex items-center justify-center',
        'transition-colors duration-150',
        'group-hover/separator:bg-primary/8',
        'group-data-[separator-active=true]/separator:bg-primary/10',
      )}
    >
      <div
        className={cn(
          'absolute transition-colors duration-150',
          'bg-border',
          'group-hover/separator:bg-primary/30',
          'group-data-[separator-active=true]/separator:bg-primary/50',
          isVertical ? 'w-px inset-y-0 left-1/2 -translate-x-px' : 'h-px inset-x-0 top-1/2 -translate-y-px',
        )}
      />
      <div
        className={cn(
          'absolute flex gap-1 transition-all duration-150',
          'opacity-40 group-hover/separator:opacity-100',
          'group-data-[separator-active=true]/separator:opacity-100',
          isVertical ? 'flex-col' : 'flex-row',
        )}
      >
        {[0, 1, 2].map((i) => (
          <div
            key={i}
            className={cn(
              'rounded-full transition-all duration-150',
              isVertical ? 'w-0.5 h-1.5' : 'w-1.5 h-0.5',
              'bg-text-muted/30',
              'group-hover/separator:bg-primary/70',
              'group-data-[separator-active=true]/separator:bg-primary',
            )}
          />
        ))}
      </div>
    </div>
  );
}
