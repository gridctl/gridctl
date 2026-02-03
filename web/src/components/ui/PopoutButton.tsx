import { ExternalLink } from 'lucide-react';
import { cn } from '../../lib/cn';

interface PopoutButtonProps {
  onClick: () => void;
  tooltip?: string;
  disabled?: boolean;
  className?: string;
}

export function PopoutButton({ onClick, tooltip = 'Open in new window', disabled, className }: PopoutButtonProps) {
  return (
    <button
      onClick={onClick}
      disabled={disabled}
      title={tooltip}
      className={cn(
        'p-1.5 rounded-lg transition-all duration-200 ease-out group',
        'text-text-muted hover:text-primary',
        'hover:bg-primary/10 hover:shadow-[0_0_12px_rgba(245,158,11,0.15)]',
        'disabled:opacity-40 disabled:cursor-not-allowed disabled:hover:bg-transparent disabled:hover:text-text-muted disabled:hover:shadow-none',
        'focus:outline-none focus:ring-2 focus:ring-primary/30 focus:ring-offset-1 focus:ring-offset-background',
        className
      )}
    >
      <ExternalLink
        size={14}
        className={cn(
          'transition-transform duration-200',
          'group-hover:scale-110 group-hover:-translate-y-px group-hover:translate-x-px'
        )}
      />
    </button>
  );
}
