import type { ReactNode } from 'react';
import { cn } from '../../lib/cn';

interface InspectorTabListProps {
  ariaLabel: string;
  children: ReactNode;
  className?: string;
}

/**
 * Tab strip wrapper used at the top of inspectors and workspace rails. The
 * actual tab buttons render as <InspectorTabButton> children — keeps the
 * a11y wiring (role="tablist", aria-label) consistent without forcing a
 * fixed shape on the caller.
 */
export function InspectorTabList({ ariaLabel, children, className }: InspectorTabListProps) {
  return (
    <div
      role="tablist"
      aria-label={ariaLabel}
      className={cn(
        'px-5 pt-3 flex items-center gap-1 border-b border-border-subtle/50',
        className,
      )}
    >
      {children}
    </div>
  );
}

interface InspectorTabButtonProps {
  active: boolean;
  onClick: () => void;
  label: string;
  controls: string;
}

export function InspectorTabButton({
  active,
  onClick,
  label,
  controls,
}: InspectorTabButtonProps) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      aria-controls={controls}
      onClick={onClick}
      className={cn(
        'px-3 py-1.5 -mb-px',
        'font-mono text-[10px] uppercase tracking-[0.2em]',
        'border-b-2 transition-colors',
        active
          ? 'border-primary text-text-primary'
          : 'border-transparent text-text-muted hover:text-text-primary',
        'focus:outline-none focus-visible:ring-1 focus-visible:ring-primary/60',
      )}
    >
      {label}
    </button>
  );
}
