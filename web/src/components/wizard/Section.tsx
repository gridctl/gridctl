import { useId } from 'react';
import { AlertCircle, ChevronDown, ChevronRight } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { cn } from '../../lib/cn';

interface SectionProps {
  title: string;
  icon: LucideIcon;
  expanded: boolean;
  onToggle: () => void;
  children: React.ReactNode;
  badge?: string;
  hasError?: boolean;
  iconClassName?: string;
}

// Controlled accordion section shared by the wizard forms. The panel unmounts
// while collapsed, so aria-controls is set only while it exists.
export function Section({
  title,
  icon: Icon,
  expanded,
  onToggle,
  children,
  badge,
  hasError,
  iconClassName = 'text-primary',
}: SectionProps) {
  const panelId = useId();
  return (
    <div className="border border-border/20 rounded-xl overflow-hidden transition-colors hover:border-border/30">
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={expanded}
        aria-controls={expanded ? panelId : undefined}
        className="w-full flex items-center gap-2.5 px-4 py-3 text-left hover:bg-surface-overlay-1 transition-colors"
      >
        {expanded ? (
          <ChevronDown size={14} className="text-text-muted flex-shrink-0" />
        ) : (
          <ChevronRight size={14} className="text-text-muted flex-shrink-0" />
        )}
        <Icon size={14} className={cn(iconClassName, 'flex-shrink-0')} />
        <span className="text-xs font-medium text-text-primary">{title}</span>
        {hasError && (
          <span className="flex items-center text-status-error flex-shrink-0">
            <AlertCircle size={12} aria-hidden="true" />
            <span className="sr-only">, has errors</span>
          </span>
        )}
        {badge && (
          <span className="ml-auto text-3xs text-text-muted bg-surface-highlight px-1.5 py-0.5 rounded-full">
            {badge}
          </span>
        )}
      </button>
      {expanded && (
        <div id={panelId} className="px-4 pb-4 pt-1 space-y-3 animate-fade-in-up" style={{ animationDuration: '200ms' }}>
          {children}
        </div>
      )}
    </div>
  );
}
