import type { LucideIcon } from 'lucide-react';
import { cn } from '../../lib/cn';

interface IconButtonProps {
  icon: LucideIcon;
  onClick?: () => void;
  disabled?: boolean;
  /**
   * Hover text and, because the button renders no visible text, its accessible
   * name. `title` alone is not reliably exposed as an accessible name across
   * assistive technology, so it is mirrored into aria-label.
   */
  tooltip?: string;
  /**
   * Accessible name when it must differ from the hover text, or when a caller
   * deliberately renders no tooltip. Wins over `tooltip`.
   */
  ariaLabel?: string;
  className?: string;
  size?: 'sm' | 'md';
  variant?: 'default' | 'ghost';
  /** For stateful toggles: rendered as aria-pressed. */
  pressed?: boolean;
}

export function IconButton({
  icon: Icon,
  onClick,
  disabled,
  tooltip,
  ariaLabel,
  className,
  size = 'md',
  variant = 'default',
  pressed,
}: IconButtonProps) {
  // The icon is the only child, so without a name the button is announced as
  // just "button". Falling back to the tooltip covers every call site that
  // passes one; a button with neither is left nameless on purpose so the gap
  // is visible in an audit rather than papered over with the icon's name.
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
}
