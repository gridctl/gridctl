import type { ReactNode, ButtonHTMLAttributes } from 'react';
import { cn } from '../../lib/cn';

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: 'primary' | 'secondary' | 'ghost' | 'danger';
  size?: 'sm' | 'md' | 'lg';
  children: ReactNode;
}

const variants = {
  primary: 'bg-primary text-white hover:bg-primaryLight',
  secondary: 'bg-surface text-text-primary border border-border hover:bg-surfaceHighlight',
  ghost: 'text-text-secondary hover:text-text-primary hover:bg-surfaceHighlight',
  danger: 'bg-status-error text-white hover:bg-red-600',
};

const sizes = {
  sm: 'px-2 py-1 text-xs',
  md: 'px-3 py-1.5 text-sm',
  lg: 'px-4 py-2 text-base',
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
        'transition-colors duration-150',
        'disabled:opacity-50 disabled:cursor-not-allowed',
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
