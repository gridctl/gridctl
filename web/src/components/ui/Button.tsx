import type { ReactNode, ButtonHTMLAttributes } from 'react';
import { cn } from '../../lib/cn';

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: 'primary' | 'secondary' | 'ghost' | 'danger';
  size?: 'sm' | 'md' | 'lg';
  children: ReactNode;
}

const variants = {
  primary: cn(
    'bg-gradient-to-r from-primary to-primary-dark text-background font-semibold',
    'hover:from-primary-light hover:to-primary',
    'shadow-[0_2px_12px_rgba(245,158,11,0.2)]',
    'hover:shadow-[0_4px_20px_rgba(245,158,11,0.3)]',
    'hover:-translate-y-0.5 active:translate-y-0'
  ),
  secondary: cn(
    'bg-surface-elevated text-text-primary border border-border',
    'hover:bg-surface-highlight hover:border-text-muted/30',
    'hover:-translate-y-0.5 active:translate-y-0'
  ),
  ghost: cn(
    'text-text-secondary hover:text-text-primary',
    'hover:bg-surface-highlight'
  ),
  danger: cn(
    'bg-gradient-to-r from-status-error to-rose-600 text-white font-semibold',
    'shadow-[0_2px_12px_rgba(244,63,94,0.2)]',
    'hover:shadow-[0_4px_20px_rgba(244,63,94,0.3)]',
    'hover:-translate-y-0.5 active:translate-y-0'
  ),
};

const sizes = {
  sm: 'px-3 py-1.5 text-xs',
  md: 'px-4 py-2 text-sm',
  lg: 'px-5 py-2.5 text-base',
};

export function Button({
  variant = 'secondary',
  size = 'md',
  children,
  className,
  disabled,
  ...props
}: ButtonProps) {
  return (
    <button
      className={cn(
        'inline-flex items-center justify-center gap-2 rounded-lg font-medium',
        'transition-all duration-200 ease-out',
        'disabled:opacity-50 disabled:cursor-not-allowed disabled:transform-none disabled:shadow-none',
        variants[variant],
        sizes[size],
        className
      )}
      disabled={disabled}
      {...props}
    >
      {children}
    </button>
  );
}
