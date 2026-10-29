import type { ReactNode } from 'react';
import type { LucideIcon } from 'lucide-react';
import { AlertCircle } from 'lucide-react';
import { cn } from '../../lib/cn';
import { Button } from './Button';

type EmptyTone = 'muted' | 'primary' | 'secondary' | 'tertiary' | 'error';

const ICON_BG: Record<EmptyTone, string> = {
  muted: 'bg-surface-elevated/50 border-border/30 text-text-muted/50',
  primary: 'bg-primary/10 border-primary/20 text-primary',
  secondary: 'bg-secondary/10 border-secondary/20 text-secondary',
  tertiary: 'bg-tertiary/10 border-tertiary/20 text-tertiary',
  error: 'bg-status-error/10 border-status-error/20 text-status-error',
};

interface EmptyStateProps {
  icon?: LucideIcon;
  title: string;
  description?: ReactNode;
  action?: ReactNode;
  tone?: EmptyTone;
  // Use `inline` for in-grid empty rows (no full-bleed centering); the
  // default centers in the parent's flex/grid box like a canvas overlay.
  inline?: boolean;
  className?: string;
  /** Extra classes on the icon itself (for example a spinner). */
  iconClassName?: string;
  role?: string;
  // Renders a Retry button when `action` is not supplied.
  onRetry?: () => void;
}

/**
 * Shared empty-state pattern used by canvases and grids. Concrete copy and
 * actions live with the caller. This component owns the visual shell
 * (icon block, headline, description, CTA slot) so spacing stays
 * consistent across workspaces.
 */
export function EmptyState({
  icon: Icon,
  title,
  description,
  action,
  tone = 'muted',
  inline,
  className,
  iconClassName,
  role,
  onRetry,
}: EmptyStateProps) {
  const retry =
    !action && onRetry ? (
      <Button variant="secondary" size="sm" onClick={onRetry}>
        Retry
      </Button>
    ) : null;

  return (
    <div
      role={role}
      className={cn(
        inline
          ? 'flex flex-col items-center gap-3 px-6 py-8 text-center'
          : 'h-full flex flex-col items-center justify-center gap-3 text-center px-12',
        className,
      )}
    >
      {Icon && (
        <div
          className={cn(
            'p-4 rounded-xl border flex-shrink-0',
            ICON_BG[tone],
          )}
        >
          <Icon size={32} className={iconClassName} />
        </div>
      )}
      <div>
        <h2 className="font-sans text-text-secondary text-base font-medium mb-1.5">
          {title}
        </h2>
        {description && (
          <p className="text-text-muted text-sm max-w-sm leading-relaxed">{description}</p>
        )}
      </div>
      {(action || retry) && <div className="mt-1">{action ?? retry}</div>}
    </div>
  );
}

export function FailedState({
  feature,
  message,
  onRetry,
  inline,
  className,
}: {
  feature: string;
  message?: string;
  onRetry?: () => void;
  inline?: boolean;
  className?: string;
}) {
  return (
    <EmptyState
      icon={AlertCircle}
      tone="error"
      role="alert"
      title={`${feature} unavailable`}
      description={message}
      onRetry={onRetry}
      inline={inline}
      className={className}
    />
  );
}

/** One-row failure notice for a surface that still has data on screen. */
export function FailureBanner({
  message,
  onRetry,
}: {
  message: string;
  onRetry: () => void;
}) {
  return (
    <div
      role="alert"
      className="mx-6 mt-4 flex items-center gap-2 px-3 py-2 rounded-lg bg-status-error/10 border border-status-error/20 text-xs text-status-error"
    >
      <AlertCircle size={12} className="flex-shrink-0" />
      <span className="flex-1 min-w-0 text-left">{message}</span>
      <Button variant="secondary" size="sm" onClick={onRetry}>
        Retry
      </Button>
    </div>
  );
}
