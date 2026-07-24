import { NavLink, useLocation } from 'react-router';
import { cn } from '../../lib/cn';
import { WORKSPACE_CONFIG, type WorkspaceConfig } from '../../types/workspace';
import { useAccessLensStore, isDirty } from '../../stores/useAccessLensStore';

interface WorkspacePillProps {
  workspace: WorkspaceConfig;
}

function WorkspacePill({ workspace }: WorkspacePillProps) {
  const { pathname } = useLocation();
  const isActive =
    pathname === `/${workspace.id}` || pathname.startsWith(`/${workspace.id}/`);
  const Icon = workspace.icon;

  // Dirty-draft navigate-away guard. The Access Lens draft lives in the Stack
  // workspace; leaving it while dirty must confirm. BrowserRouter has no
  // useBlocker, so cancel the NavLink here and route through the store, which
  // AccessLens turns into a discard-with-confirm.
  const handleClick = (e: React.MouseEvent) => {
    const s = useAccessLensStore.getState();
    const leavingStack =
      (pathname === '/stack' || pathname.startsWith('/stack/')) && workspace.id !== 'stack';
    const draftDirty = s.enabled && s.clientSlug != null && isDirty(s.draft, s.baseline);
    if (leavingStack && draftDirty) {
      e.preventDefault();
      s.requestExitNav(`/${workspace.id}`);
    }
  };

  return (
    <NavLink
      to={`/${workspace.id}`}
      role="tab"
      aria-selected={isActive}
      aria-label={workspace.label}
      title={workspace.label}
      data-workspace={workspace.id}
      onClick={handleClick}
      className={cn(
        // Icon-only below 1360px: eight text pills plus the right-side action
        // cluster collide on narrower windows, so labels collapse to icons
        // (title + aria-label keep the pills identifiable).
        'inline-flex items-center gap-1.5 px-2 min-[1360px]:px-3 py-1 rounded-full text-xs font-medium transition-colors',
        'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-primary/60',
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

// Segmented control of pills in the Header, one per WORKSPACE_CONFIG entry.
// role="tablist" with aria-selected on each tab for screen-reader navigation.
export function WorkspaceSwitcher() {
  return (
    <div
      role="tablist"
      aria-label="Workspace"
      className="flex items-center gap-1 p-1 rounded-full bg-surface-elevated/60 backdrop-blur-sm border border-border/50"
    >
      {WORKSPACE_CONFIG.map((ws) => (
        <WorkspacePill key={ws.id} workspace={ws} />
      ))}
    </div>
  );
}
