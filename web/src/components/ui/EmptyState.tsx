import type { ReactNode } from 'react';
import type { LucideIcon } from 'lucide-react';
import { cn } from '../../lib/cn';

type EmptyTone = 'muted' | 'primary' | 'secondary' | 'tertiary';

const ICON_BG: Record<EmptyTone, string> = {
  muted: 'bg-surface-elevated/50 border-border/30 text-text-muted/50',
  primary: 'bg-primary/10 border-primary/20 text-primary',
  secondary: 'bg-secondary/10 border-secondary/20 text-secondary',
  tertiary: 'bg-tertiary/10 border-tertiary/20 text-tertiary',
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
}

/**
 * Shared empty-state pattern used by canvases and grids. Concrete copy and
 * actions live with the caller — this component owns the visual shell
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
}: EmptyStateProps) {
  return (
    <div
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
          <Icon size={32} />
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
      {action && <div className="mt-1">{action}</div>}
    </div>
  );
}
