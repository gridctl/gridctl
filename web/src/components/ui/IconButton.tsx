import type { LucideIcon } from 'lucide-react';
import { cn } from '../../lib/cn';

interface IconButtonProps {
  icon: LucideIcon;
  onClick?: () => void;
  disabled?: boolean;
  tooltip?: string;
  className?: string;
  size?: 'sm' | 'md';
  variant?: 'default' | 'ghost';
}

export function IconButton({
  icon: Icon,
  onClick,
  disabled,
  tooltip,
  className,
  size = 'md',
  variant = 'default',
}: IconButtonProps) {
  const sizeClasses = {
    sm: 'p-1.5',
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
      className={cn(
        'rounded-lg transition-all duration-200 ease-out',
        'disabled:opacity-40 disabled:cursor-not-allowed',
        'focus:outline-none focus:ring-2 focus:ring-primary/30 focus:ring-offset-1 focus:ring-offset-background',
        variantClasses[variant],
        sizeClasses[size],
        className
      )}
    >
      <Icon size={iconSize} />
    </button>
  );
}
