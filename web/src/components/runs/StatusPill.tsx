import { cn } from '../../lib/cn';
import type { RunStatusTone } from './status';

interface StatusPillProps {
  tone: RunStatusTone;
  /** "row" shrinks the icon for a list cell; "inspector" sizes it
   *  for headers. Defaults to "row". */
  size?: 'row' | 'inspector';
}

/**
 * StatusPill is the dot + icon + label trio rendered in every place
 * the UI surfaces a run's status (the grid, the inspector, the detail
 * page header). Centralised so a styling tweak lands once.
 *
 * The LiveRunChip in BottomPanel keeps its own dot-only layout — the
 * label doesn't fit a compact chip.
 */
export function StatusPill({ tone, size = 'row' }: StatusPillProps) {
  const Icon = tone.icon;
  const iconSize = size === 'inspector' ? 12 : 11;
  return (
    <span className={cn('inline-flex items-center gap-1.5', tone.text)}>
      <span
        aria-hidden
        className={cn(
          'inline-block w-1.5 h-1.5 rounded-full',
          tone.dot,
          tone.pulse && 'animate-pulse',
        )}
      />
      <Icon size={iconSize} aria-hidden />
      <span className="text-[11px] uppercase tracking-[0.14em]">{tone.label}</span>
    </span>
  );
}
