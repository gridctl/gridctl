import { X, type LucideIcon } from 'lucide-react';
import type { ReactNode } from 'react';
import { cn } from '../../lib/cn';

type AccentTone = 'primary' | 'secondary' | 'tertiary' | 'violet' | 'muted';

const ACCENT_BG: Record<AccentTone, string> = {
  primary: 'bg-primary/10 border-primary/20',
  secondary: 'bg-secondary/10 border-secondary/20',
  tertiary: 'bg-tertiary/10 border-tertiary/20',
  violet: 'bg-violet-500/10 border-violet-500/20',
  muted: 'bg-surface-elevated/50 border-border/30',
};

const ACCENT_TEXT: Record<AccentTone, string> = {
  primary: 'text-primary',
  secondary: 'text-secondary',
  tertiary: 'text-tertiary',
  violet: 'text-violet-400',
  muted: 'text-text-muted',
};

interface InspectorHeaderProps {
  title: string;
  subtitle?: ReactNode;
  icon?: LucideIcon;
  accent?: AccentTone;
  onClose?: () => void;
  onDetach?: () => void;
  detachDisabled?: boolean;
  // Right-aligned actions slot — for popout buttons or workspace-specific
  // controls. Rendered before the close button.
  actions?: ReactNode;
}

/**
 * InspectorHeader is the shared top-of-aside header for the Topology and
 * Skills inspectors. It owns the icon block, title, subtitle, and the
 * close/popout affordances; the badge/tag row sits in the subtitle slot
 * so each workspace can drop in its own metadata pills without forking.
 */
export function InspectorHeader({
  title,
  subtitle,
  icon: Icon,
  accent = 'primary',
  onClose,
  onDetach,
  detachDisabled,
  actions,
}: InspectorHeaderProps) {
  return (
    <div className="flex items-center justify-between p-4 border-b border-border/50 bg-surface-elevated/30">
      <div className="flex items-center gap-3 min-w-0">
        {Icon && (
          <div
            className={cn(
              'p-2 rounded-xl flex-shrink-0 border relative',
              ACCENT_BG[accent],
            )}
          >
            <Icon size={16} className={ACCENT_TEXT[accent]} />
          </div>
        )}
        <div className="min-w-0">
          <h2 className="font-semibold text-text-primary truncate tracking-tight">{title}</h2>
          {subtitle && <div className="flex items-center gap-1.5">{subtitle}</div>}
        </div>
      </div>
      <div className="flex items-center gap-1">
        {actions}
        {onDetach && (
          <button
            type="button"
            onClick={onDetach}
            disabled={detachDisabled}
            className={cn(
              'p-1.5 rounded-lg transition-colors group',
              detachDisabled
                ? 'opacity-30 cursor-not-allowed'
                : 'hover:bg-surface-highlight',
            )}
            aria-label="Open in new window"
          >
            <svg
              width="14"
              height="14"
              viewBox="0 0 16 16"
              fill="none"
              className={cn(
                'text-text-muted transition-colors',
                !detachDisabled && 'group-hover:text-text-primary',
              )}
            >
              <path
                d="M10 2h4v4M14 2L7 9M6 3H3a1 1 0 0 0-1 1v9a1 1 0 0 0 1 1h9a1 1 0 0 0 1-1v-3"
                stroke="currentColor"
                strokeWidth="1.4"
                strokeLinecap="round"
                strokeLinejoin="round"
              />
            </svg>
          </button>
        )}
        {onClose && (
          <button
            type="button"
            onClick={onClose}
            className="p-1.5 rounded-lg hover:bg-surface-highlight transition-colors group"
            aria-label="Close inspector"
          >
            <X
              size={16}
              className="text-text-muted group-hover:text-text-primary transition-colors"
            />
          </button>
        )}
      </div>
    </div>
  );
}
