import { useCallback, useEffect, useState } from 'react';
import { Outlet, useLocation, useNavigate } from 'react-router-dom';
import { ReactFlowProvider, useReactFlow } from '@xyflow/react';
import { Header } from '../layout/Header';
import { StatusBar } from '../layout/StatusBar';
import { BottomPanel } from '../layout/BottomPanel';
import { AuthPrompt } from '../auth/AuthPrompt';
import { ResizeHandle } from '../ui/ResizeHandle';
import { CommandPalette } from '../palette/CommandPalette';
import { ToastContainer } from '../ui/Toast';
import { ErrorBoundary } from '../ui/ErrorBoundary';
import { useStackStore } from '../../stores/useStackStore';
import { useUIStore } from '../../stores/useUIStore';
import { useAuthStore } from '../../stores/useAuthStore';
import { usePolling } from '../../hooks/usePolling';
import { useSSEShutdown } from '../../hooks/useSSEShutdown';
import { useKeyboardShortcuts } from '../../hooks/useKeyboardShortcuts';
import { useGlobalCommands } from '../../hooks/useGlobalCommands';
import { documentTitleForWorkspace, isWorkspace, type Workspace } from '../../types/workspace';
import {
  LAST_WORKSPACE_GLOBAL_KEY,
  LAST_WORKSPACE_PER_STACK_PREFIX,
} from '../../lib/landing-workspace';

const HEADER_HEIGHT = 56;
const STATUSBAR_HEIGHT = 32;

const BOTTOM_PANEL_COLLAPSED = 40;
const BOTTOM_PANEL_DEFAULT = 250;
const BOTTOM_PANEL_MIN = 100;
const BOTTOM_PANEL_MAX = 800;

function workspaceFromPath(pathname: string): Workspace | null {
  // First segment after the leading slash, e.g. /topology/foo → "topology"
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

function AppShellInner() {
  const [isRefreshing, setIsRefreshing] = useState(false);
  const [bottomPanelHeight, setBottomPanelHeight] = useState(BOTTOM_PANEL_DEFAULT);
  const [isShuttingDown, setIsShuttingDown] = useState(false);

  const selectNode = useStackStore((s) => s.selectNode);
  const stackId = useStackStore((s) => s.gatewayInfo?.name ?? null);

  const setSidebarOpen = useUIStore((s) => s.setSidebarOpen);
  const toggleBottomPanel = useUIStore((s) => s.toggleBottomPanel);
  const bottomPanelOpen = useUIStore((s) => s.bottomPanelOpen);
  const commandPaletteOpen = useUIStore((s) => s.commandPaletteOpen);
  const setCommandPaletteOpen = useUIStore((s) => s.setCommandPaletteOpen);
  const toggleCommandPalette = useUIStore((s) => s.toggleCommandPalette);
  const setActiveWorkspace = useUIStore((s) => s.setActiveWorkspace);
  const activeWorkspace = useUIStore((s) => s.activeWorkspace);
  const toggleCompactMode = useUIStore((s) => s.toggleCompactMode);

  const authRequired = useAuthStore((s) => s.authRequired);

  const navigate = useNavigate();
  const { pathname } = useLocation();
  const { fitView, zoomIn, zoomOut } = useReactFlow();
  const { refresh } = usePolling();

  useSSEShutdown(useCallback(() => {
    setIsShuttingDown(true);
  }, []));

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

  const handleBottomPanelResize = useCallback((delta: number) => {
    setBottomPanelHeight((prev) => {
      const newHeight = prev + delta;
      return Math.min(BOTTOM_PANEL_MAX, Math.max(BOTTOM_PANEL_MIN, newHeight));
    });
  }, []);

  // Keyboard shortcuts. ⌘1/2 switch workspaces (Topology / Library);
  // bottom-panel tab shortcuts were retired (tabs are still clickable in
  // the panel).
  useKeyboardShortcuts({
    onFitView: () => fitView({ padding: 0.2, duration: 300 }),
    onEscape: () => {
      selectNode(null);
      setSidebarOpen(false);
    },
    onZoomIn: () => zoomIn({ duration: 200 }),
    onZoomOut: () => zoomOut({ duration: 200 }),
    onRefresh: handleRefresh,
    onToggleBottomPanel: toggleBottomPanel,
    onOpenPalette: toggleCommandPalette,
    onSwitchToWorkspace: (id) => navigate(`/${id}`),
    onToggleCompactMode: () => toggleCompactMode(activeWorkspace),
  });

  useGlobalCommands({ onRefresh: handleRefresh });

  const bottomRowHeight = bottomPanelOpen ? bottomPanelHeight : BOTTOM_PANEL_COLLAPSED;

  return (
    <div
      className="h-screen w-screen overflow-hidden bg-background"
      style={{
        display: 'grid',
        gridTemplateRows: `${HEADER_HEIGHT}px 1fr ${bottomRowHeight}px ${STATUSBAR_HEIGHT}px`,
        gridTemplateColumns: '1fr',
      }}
    >
      {authRequired && <AuthPrompt />}

      {isShuttingDown && (
        <div className="absolute top-16 left-1/2 -translate-x-1/2 z-40 px-4 py-2 rounded-lg bg-primary/10 border border-primary/20 text-primary text-sm font-medium backdrop-blur-xl animate-fade-in-scale">
          Gateway is shutting down...
        </div>
      )}

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

      <div className="flex flex-col h-full overflow-hidden">
        {bottomPanelOpen && (
          <ResizeHandle
            direction="horizontal"
            onResize={handleBottomPanelResize}
            className="flex-shrink-0"
          />
        )}
        <div className="flex-1 min-h-0">
          <BottomPanel />
        </div>
      </div>

      <StatusBar />

      <ToastContainer />

      <CommandPalette
        isOpen={commandPaletteOpen}
        onClose={() => setCommandPaletteOpen(false)}
      />
    </div>
  );
}

// AppShell is the parent route for the two workspaces. It owns the
// ReactFlowProvider so workspace canvases (and shell-level hooks like
// useGlobalCommands, which calls useReactFlow) share a single instance.
export function AppShell() {
  return (
    <ReactFlowProvider>
      <AppShellInner />
    </ReactFlowProvider>
  );
}

export default AppShell;
