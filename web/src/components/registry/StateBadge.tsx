import { cn } from '../../lib/cn';
import { stateBadgeClasses } from '../../lib/stateColors';
import type { ItemState } from '../../types';

interface StateBadgeProps {
  state: ItemState;
  className?: string;
}

export function StateBadge({ state, className }: StateBadgeProps) {
  const style = stateBadgeClasses[state] ?? stateBadgeClasses.draft;
  return (
    <span
      className={cn(
        'inline-flex items-center text-[10px] px-1.5 py-0.5 rounded font-mono border flex-shrink-0',
        style,
        className,
      )}
    >
      {state}
    </span>
  );
}
