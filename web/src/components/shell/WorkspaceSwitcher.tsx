import { NavLink, useLocation, useNavigate } from 'react-router';
import { cn } from '../../lib/cn';
import { WORKSPACE_CONFIG, type WorkspaceConfig } from '../../types/workspace';
import { useNavigationGuardStore } from '../../stores/useNavigationGuardStore';

interface WorkspacePillProps {
  workspace: WorkspaceConfig;
}

function WorkspacePill({ workspace }: WorkspacePillProps) {
  const { pathname } = useLocation();
  const isActive =
    pathname === `/${workspace.id}` || pathname.startsWith(`/${workspace.id}/`);
  const Icon = workspace.icon;

  const navigate = useNavigate();
  const target = `/${workspace.id}`;

  // An unsaved editor that this navigation would leave cancels the NavLink
  // and asks through the shared confirm. Modified clicks open a new tab and
  // pass through untouched.
  const handleClick = (e: React.MouseEvent) => {
    if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    const { isBlocked, guard } = useNavigationGuardStore.getState();
    if (!isBlocked(target)) return;
    e.preventDefault();
    guard(target, () => {
      void navigate(target);
    });
  };

  return (
    <NavLink
      to={target}
      aria-current={isActive ? 'page' : undefined}
      aria-label={workspace.label}
      title={workspace.label}
      data-workspace={workspace.id}
      onClick={handleClick}
      className={cn(
        // Icon-only below 1360px: eight text pills plus the right-side action
        // cluster collide on narrower windows, so labels collapse to icons
        // (title + aria-label keep the pills identifiable).
        'inline-flex items-center gap-1.5 px-2 min-[1360px]:px-3 py-1 rounded-full text-xs font-medium transition-colors shrink-0',
        'focus-visible:-outline-offset-2',
        isActive
          ? 'bg-primary/15 text-primary border border-primary/30'
          : 'text-text-secondary hover:text-text-primary hover:bg-surface-highlight/60 border border-transparent',
      )}
    >
      <Icon size={12} aria-hidden="true" />
      <span className="hidden min-[1360px]:inline">{workspace.label}</span>
    </NavLink>
  );
}

// Workspace navigation. NavLink's default prefix match agrees with isActive
// (including /library/:skillName). aria-current is set from that same check
// so a deep link cannot disagree with the pill highlight.
export function WorkspaceSwitcher() {
  return (
    <nav
      aria-label="Workspaces"
      className="flex items-center gap-1 p-1 min-w-0 overflow-x-auto overscroll-x-contain [scrollbar-width:none] [&::-webkit-scrollbar]:hidden rounded-full bg-surface-elevated/60 backdrop-blur-sm border border-border/50"
    >
      {WORKSPACE_CONFIG.map((ws) => (
        <WorkspacePill key={ws.id} workspace={ws} />
      ))}
    </nav>
  );
}
