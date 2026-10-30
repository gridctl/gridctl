import { forwardRef } from 'react';
import type { LucideIcon } from 'lucide-react';
import { cn } from '../../lib/cn';

interface IconButtonBase {
  icon: LucideIcon;
  onClick?: () => void;
  disabled?: boolean;
  className?: string;
  size?: 'sm' | 'md';
  variant?: 'default' | 'ghost';
  /** For stateful toggles: rendered as aria-pressed. */
  pressed?: boolean;
}

type IconButtonProps = IconButtonBase & (
  | {
      /** Accessible name when it must differ from the hover text. Wins over `tooltip`. */
      ariaLabel: string;
      /**
       * Hover text. Mirrored into aria-label when `ariaLabel` is omitted, because
       * `title` alone is not reliably exposed as an accessible name.
       */
      tooltip?: string;
    }
  | {
      tooltip: string;
      ariaLabel?: string;
    }
);

export const IconButton = forwardRef<HTMLButtonElement, IconButtonProps>(function IconButton({
  icon: Icon,
  onClick,
  disabled,
  tooltip,
  ariaLabel,
  className,
  size = 'md',
  variant = 'default',
  pressed,
}, ref) {
  const accessibleName = ariaLabel ?? tooltip;
  const sizeClasses = {
    sm: 'p-2',
    md: 'p-2',
  };
  const iconSize = size === 'sm' ? 14 : 16;

  const variantClasses = {
    default: cn(
      'bg-surface-elevated/60 text-text-muted border border-border/50',
      'hover:bg-surface-highlight hover:text-text-primary hover:border-text-muted/30',
      'backdrop-blur-sm'
    ),
    ghost: cn(
      'text-text-muted',
      'hover:text-text-primary hover:bg-surface-highlight/60'
    ),
  };

  return (
    <button
      ref={ref}
      type="button"
      onClick={onClick}
      disabled={disabled}
      title={tooltip}
      aria-label={accessibleName}
      aria-pressed={pressed}
      className={cn(
        'rounded-lg transition-all duration-200 ease-out',
        'disabled:opacity-40 disabled:cursor-not-allowed',
        'focus:outline-none focus:ring-2 focus:ring-primary/30 focus:ring-offset-1 focus:ring-offset-background',
        variantClasses[variant],
        sizeClasses[size],
        className
      )}
    >
      <Icon size={iconSize} aria-hidden="true" />
    </button>
  );
});
