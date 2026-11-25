import { useEffect, useRef } from 'react';
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
  const linkRef = useRef<HTMLAnchorElement>(null);

  // On narrow windows the strip scrolls; keep the active pill in view.
  useEffect(() => {
    if (isActive) linkRef.current?.scrollIntoView({ block: 'nearest', inline: 'nearest' });
  }, [isActive]);

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
      ref={linkRef}
      className={cn(
        // The active pill is always labeled. The others are labeled only when
        // the header's left group (@container/header) has room for all of
        // them; title and aria-label keep icon-only pills identifiable.
        'inline-flex items-center justify-center gap-1.5 min-h-8 min-w-8 py-1 rounded-full text-xs font-medium transition-colors shrink-0',
        'focus-visible:-outline-offset-2',
        isActive
          ? 'px-2.5 bg-primary/15 text-primary border border-primary/30'
          : 'px-2 @[880px]/header:px-2.5 text-text-secondary hover:text-text-primary hover:bg-surface-highlight/60 border border-transparent',
      )}
    >
      <Icon size={12} aria-hidden="true" />
      <span className={isActive ? 'inline' : 'hidden @[880px]/header:inline'}>{workspace.label}</span>
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
