import { useCallback, useEffect, useState } from 'react';
import { Outlet, useLocation, useNavigate } from 'react-router';
import { Header } from '../layout/Header';
import { StatusBar } from '../layout/StatusBar';
import { CommandPalette } from '../palette/CommandPalette';
import { ToastContainer } from '../ui/Toast';
import { ErrorBoundary } from '../ui/ErrorBoundary';
import { useStackStore } from '../../stores/useStackStore';
import { useToolsDirtyStore } from '../../stores/useToolsDirtyStore';
import { useUIStore } from '../../stores/useUIStore';
import { usePolling } from '../../hooks/usePolling';
import { useKeyboardShortcuts } from '../../hooks/useKeyboardShortcuts';
import { useCanvasActionsStore } from '../../stores/useCanvasActionsStore';
import { useGlobalCommands } from '../../hooks/useGlobalCommands';
import { documentTitleForWorkspace, isWorkspace, type Workspace } from '../../types/workspace';
import {
  LAST_WORKSPACE_GLOBAL_KEY,
  LAST_WORKSPACE_PER_STACK_PREFIX,
} from '../../lib/landing-workspace';

const HEADER_HEIGHT = 56;
const STATUSBAR_HEIGHT = 32;

function workspaceFromPath(pathname: string): Workspace | null {
  // First segment after the leading slash, e.g. /stack/foo → "stack"
  const first = pathname.split('/').filter(Boolean)[0];
  return isWorkspace(first) ? first : null;
}

function persistLastWorkspace(ws: Workspace, stackId: string | null) {
  try {
    localStorage.setItem(LAST_WORKSPACE_GLOBAL_KEY, ws);
    if (stackId) {
      localStorage.setItem(`${LAST_WORKSPACE_PER_STACK_PREFIX}${stackId}`, ws);
    }
  } catch {
    // localStorage may be unavailable; persistence is best-effort.
  }
}

export function AppShell() {
  const [isRefreshing, setIsRefreshing] = useState(false);

  const selectNode = useStackStore((s) => s.selectNode);
  const stackId = useStackStore((s) => s.gatewayInfo?.name ?? null);

  const setSidebarOpen = useUIStore((s) => s.setSidebarOpen);
  const commandPaletteOpen = useUIStore((s) => s.commandPaletteOpen);
  const setCommandPaletteOpen = useUIStore((s) => s.setCommandPaletteOpen);
  const toggleCommandPalette = useUIStore((s) => s.toggleCommandPalette);
  const setActiveWorkspace = useUIStore((s) => s.setActiveWorkspace);
  const activeWorkspace = useUIStore((s) => s.activeWorkspace);
  const toggleCompactMode = useUIStore((s) => s.toggleCompactMode);

  const navigate = useNavigate();
  const { pathname } = useLocation();
  const { refresh } = usePolling({ registerRetry: true });

  const handleRefresh = useCallback(async () => {
    setIsRefreshing(true);
    await refresh();
    setTimeout(() => setIsRefreshing(false), 500);
  }, [refresh]);

  // Sync activeWorkspace + last-workspace persistence from URL changes.
  useEffect(() => {
    const ws = workspaceFromPath(pathname);
    if (!ws) return;
    setActiveWorkspace(ws);
    persistLastWorkspace(ws, stackId);
  }, [pathname, stackId, setActiveWorkspace]);

  // Reflect the active workspace in the document title so browser tabs and
  // history entries are identifiable. Falls back to the base name for
  // non-workspace paths.
  useEffect(() => {
    document.title = documentTitleForWorkspace(workspaceFromPath(pathname));
  }, [pathname]);

  // Keyboard shortcuts. ⌘1-9 switch workspaces; ⌘J opens Logs (the old
  // bottom-panel toggle's muscle memory lands on the panel's main content).
  // Fit and zoom are Stack-only, matching the palette. Outside Stack the
  // keys are not prevented, so the browser keeps Find and zoom.
  const canvasShortcuts = activeWorkspace === 'stack';
  useKeyboardShortcuts({
    onFitView: canvasShortcuts
      ? () => useCanvasActionsStore.getState().fitView({ padding: 0.2, duration: 300 })
      : undefined,
    onEscape: () => {
      selectNode(null);
      setSidebarOpen(false);
    },
    onZoomIn: canvasShortcuts
      ? () => useCanvasActionsStore.getState().zoomIn({ duration: 200 })
      : undefined,
    onZoomOut: canvasShortcuts
      ? () => useCanvasActionsStore.getState().zoomOut({ duration: 200 })
      : undefined,
    onRefresh: handleRefresh,
    onOpenLogs: () => navigate('/logs'),
    onOpenPalette: toggleCommandPalette,
    onSwitchToWorkspace: (id) => {
      // The ⌘1-9 shortcuts are the same action as the workspace pills, so
      // they route through the same Tools dirty guard (see WorkspaceSwitcher).
      const tools = useToolsDirtyStore.getState();
      if (tools.dirty && pathname.startsWith('/tools') && id !== 'tools') {
        tools.requestExitNav(`/${id}`);
        return;
      }
      navigate(`/${id}`);
    },
    onToggleCompactMode: () => toggleCompactMode(activeWorkspace),
  });

  useGlobalCommands({ onRefresh: handleRefresh });

  return (
    <div
      className="h-screen w-screen overflow-hidden bg-background"
      style={{
        display: 'grid',
        gridTemplateRows: `${HEADER_HEIGHT}px 1fr ${STATUSBAR_HEIGHT}px`,
        gridTemplateColumns: '1fr',
      }}
    >
      <Header onRefresh={handleRefresh} isRefreshing={isRefreshing} />

      <main className="relative overflow-hidden" style={{ minHeight: 100 }}>
        {/* Contain a workspace render throw so the shell (header, nav, status
            bar) stays mounted and the user can navigate away. Resetting on the
            route path lets navigation recover a crashed workspace without a
            full reload. */}
        <ErrorBoundary variant="inline" resetKey={pathname}>
          <Outlet />
        </ErrorBoundary>
      </main>

      <StatusBar />

      <ToastContainer />

      <CommandPalette
        isOpen={commandPaletteOpen}
        onClose={() => setCommandPaletteOpen(false)}
      />
    </div>
  );
}

export default AppShell;
