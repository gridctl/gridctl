import type { ReactNode, Ref } from 'react';
import { cn } from '../../lib/cn';

interface WorkspaceHeaderProps {
  /** Visible page title; callers pass the WORKSPACE_LABELS value. */
  title: string;
  /** Applied as the h1 id so a section's aria-labelledby can reference it. */
  headingId?: string;
  /** One-line summary that truncates before the actions wrap. */
  summary?: ReactNode;
  /** View switcher rendered between the title and the summary. */
  switcher?: ReactNode;
  /** Right-aligned controls. */
  actions?: ReactNode;
  /** Selects py-2 instead of py-3. */
  compact?: boolean;
  /** Additional rows below the title row. */
  children?: ReactNode;
  className?: string;
  /** Ref to the title row, for callers that measure the row's width. */
  rowRef?: Ref<HTMLDivElement>;
}

// The single visible h1 for a workspace, with its summary, optional view
// switcher, and actions on one wrapping row.
export function WorkspaceHeader({
  title,
  headingId,
  summary,
  switcher,
  actions,
  compact = false,
  children,
  className,
  rowRef,
}: WorkspaceHeaderProps) {
  const hasSummary = summary !== undefined && summary !== null && summary !== false && summary !== '';
  return (
    <header
      className={cn(
        'flex-shrink-0 bg-surface/30 backdrop-blur-sm border-b border-border-subtle px-6 flex flex-col gap-2',
        compact ? 'py-2' : 'py-3',
        className,
      )}
    >
      <div ref={rowRef} className="flex flex-wrap items-center gap-x-3 gap-y-2">
        <h1 id={headingId} className="text-sm font-semibold text-text-primary whitespace-nowrap">
          {title}
        </h1>
        {switcher != null && switcher !== false && <div className="flex items-center">{switcher}</div>}
        {hasSummary && (
          <div data-testid="workspace-header-summary" className="min-w-0 flex-1 truncate text-xs text-text-muted">
            {summary}
          </div>
        )}
        {actions != null && actions !== false && (
          <div className="ml-auto flex items-center gap-2 flex-shrink-0">{actions}</div>
        )}
      </div>
      {children}
    </header>
  );
}
