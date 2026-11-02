import type { MouseEvent } from 'react';
import { cn } from '../../lib/cn';

interface SwitchProps {
  checked: boolean;
  onChange: (next: boolean) => void;
  /** Accessible name. Rendered as aria-label, not as visible text. */
  label: string;
  disabled?: boolean;
  title?: string;
  size?: 'sm' | 'md';
  className?: string;
  /** Passthrough so a row click can stopPropagation without owning the toggle. */
  onClick?: (event: MouseEvent<HTMLButtonElement>) => void;
}

export function Switch({
  checked,
  onChange,
  label,
  disabled,
  title,
  size = 'md',
  className,
  onClick,
}: SwitchProps) {
  const compact = size === 'sm';
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      disabled={disabled}
      title={title}
      onClick={(event) => {
        onClick?.(event);
        if (event.defaultPrevented || disabled) return;
        onChange(!checked);
      }}
      className={cn(
        'relative inline-flex flex-shrink-0 items-center rounded-full border transition-colors',
        'outline-none focus-visible:ring-2 focus-visible:ring-primary/40',
        'disabled:opacity-40 disabled:cursor-not-allowed',
        compact ? 'h-4 w-7' : 'h-5 w-9',
        checked
          ? 'bg-primary/30 border-primary/50'
          : 'bg-surface-elevated border-border',
        className,
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
  );
}
