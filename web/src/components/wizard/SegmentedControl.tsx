import type { LucideIcon } from 'lucide-react';
import { cn } from '../../lib/cn';

export interface SegmentedOption<T extends string> {
  value: T;
  label: string;
  icon?: LucideIcon;
}

interface SegmentedControlProps<T extends string> {
  ariaLabel: string;
  value: T;
  onChange: (value: T) => void;
  options: SegmentedOption<T>[];
  className?: string;
}

// The selected segment carries a border and a heavier weight as well as the
// tint, so the selection survives without color.
export function SegmentedControl<T extends string>({
  ariaLabel,
  value,
  onChange,
  options,
  className,
}: SegmentedControlProps<T>) {
  return (
    <div
      role="group"
      aria-label={ariaLabel}
      className={cn('inline-flex items-center bg-surface-elevated/60 border border-border/40 rounded-lg p-0.5', className)}
    >
      {options.map((option) => {
        const selected = value === option.value;
        const Icon = option.icon;
        return (
          <button
            key={option.value}
            type="button"
            onClick={() => onChange(option.value)}
            aria-pressed={selected}
            className={cn(
              'flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs border transition-all duration-200',
              selected
                ? 'bg-primary/15 text-primary shadow-sm border-primary/40 font-semibold'
                : 'text-text-muted hover:text-text-secondary border-transparent font-medium',
            )}
          >
            {Icon && <Icon size={12} aria-hidden="true" />}
            {option.label}
          </button>
        );
      })}
    </div>
  );
}
