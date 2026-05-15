import { NavLink, useLocation } from 'react-router-dom';
import { cn } from '../../lib/cn';
import { WORKSPACES, WORKSPACE_LABELS, type Workspace } from '../../types/workspace';

interface WorkspacePillProps {
  workspace: Workspace;
  label: string;
}

function WorkspacePill({ workspace, label }: WorkspacePillProps) {
  const { pathname } = useLocation();
  const isActive = pathname === `/${workspace}` || pathname.startsWith(`/${workspace}/`);
  return (
    <NavLink
      to={`/${workspace}`}
      role="tab"
      aria-selected={isActive}
      data-workspace={workspace}
      className={cn(
        'px-3 py-1 rounded-full text-xs font-medium transition-colors',
        'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-primary/60',
        isActive
          ? 'bg-primary/15 text-primary border border-primary/30'
          : 'text-text-secondary hover:text-text-primary hover:bg-surface-highlight/60 border border-transparent',
      )}
    >
      {label}
    </NavLink>
  );
}

// Segmented control of three NavLink pills in the Header. Always visible.
// role="tablist" with aria-selected on each tab for screen-reader navigation.
export function WorkspaceSwitcher() {
  return (
    <div
      role="tablist"
      aria-label="Workspace"
      className="flex items-center gap-1 p-1 rounded-full bg-surface-elevated/60 backdrop-blur-sm border border-border/50"
    >
      {WORKSPACES.map((ws) => (
        <WorkspacePill key={ws} workspace={ws} label={WORKSPACE_LABELS[ws]} />
      ))}
    </div>
  );
}
