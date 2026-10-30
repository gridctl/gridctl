import { NavLink, useLocation } from 'react-router';
import { cn } from '../../lib/cn';
import { WORKSPACE_CONFIG, type WorkspaceConfig } from '../../types/workspace';
import { useAccessLensStore, isDirty } from '../../stores/useAccessLensStore';
import { useToolsDirtyStore } from '../../stores/useToolsDirtyStore';

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
      return;
    }
    // Same guard for the Tools workspace's whitelist editor: its dirty flag
    // is mirrored into useToolsDirtyStore, and ToolsWorkspace renders the
    // confirm for the stashed target.
    const t = useToolsDirtyStore.getState();
    const leavingTools =
      (pathname === '/tools' || pathname.startsWith('/tools/')) && workspace.id !== 'tools';
    if (leavingTools && t.dirty) {
      e.preventDefault();
      t.requestExitNav(`/${workspace.id}`);
    }
  };

  return (
    <NavLink
      to={`/${workspace.id}`}
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
