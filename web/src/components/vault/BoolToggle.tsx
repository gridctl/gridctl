import { cn } from '../../lib/cn';

// BoolToggle is an accessible on/off switch for `bool` variables. The visible
// state text reflects the value (true/false); the control's accessible label
// is stable ("Value") so assistive tech doesn't hear the label change on flip.
export function BoolToggle({
  checked,
  onChange,
  compact,
  className,
}: {
  checked: boolean;
  onChange: (next: boolean) => void;
  compact?: boolean;
  className?: string;
}) {
  return (
    <div className={cn('flex items-center gap-2', className)}>
      <button
        type="button"
        role="switch"
        aria-checked={checked}
        aria-label="Value"
        onClick={() => onChange(!checked)}
        className={cn(
          'relative inline-flex flex-shrink-0 items-center rounded-full border transition-colors',
          'outline-none focus-visible:ring-2 focus-visible:ring-primary/40',
          compact ? 'h-4 w-7' : 'h-5 w-9',
          checked
            ? 'bg-primary/30 border-primary/50'
            : 'bg-surface-elevated border-border',
        )}
      >
        <span
          className={cn(
            'inline-block rounded-full bg-text-primary transition-transform duration-150',
            compact ? 'h-3 w-3' : 'h-3.5 w-3.5',
            checked
              ? compact
                ? 'translate-x-3.5'
                : 'translate-x-[1.125rem]'
              : 'translate-x-0.5',
          )}
        />
      </button>
      <span className="text-xs font-mono text-text-secondary select-none">
        {checked ? 'true' : 'false'}
      </span>
    </div>
  );
}
