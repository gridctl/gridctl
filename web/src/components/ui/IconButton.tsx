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
    sm: 'p-1',
    md: 'p-2',
  };
  const iconSize = size === 'sm' ? 14 : 16;

  const variantClasses = {
    default: 'bg-background text-text-muted hover:bg-surfaceHighlight hover:text-text-primary',
    ghost: 'text-text-muted hover:text-text-primary hover:bg-surfaceHighlight',
  };

  return (
    <button
      onClick={onClick}
      disabled={disabled}
      title={tooltip}
      className={cn(
        'rounded-lg transition-colors duration-150',
        'disabled:opacity-50 disabled:cursor-not-allowed',
        variantClasses[variant],
        sizeClasses[size],
        className
      )}
    >
      <Icon size={iconSize} />
    </button>
  );
}
