import { cn } from '../../lib/cn';
import type { ItemState } from '../../types';

const STYLES: Record<ItemState, string> = {
  active: 'text-emerald-400 bg-emerald-400/10 border-emerald-400/25',
  draft: 'text-amber-400 bg-amber-400/10 border-amber-400/25',
  disabled: 'text-text-muted bg-surface border-border/40',
};

interface StateBadgeProps {
  state: ItemState;
  className?: string;
}

export function StateBadge({ state, className }: StateBadgeProps) {
  const style = STYLES[state] ?? STYLES.draft;
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
