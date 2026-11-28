import { useCallback, useEffect, useRef, useState } from 'react';
import { Outlet, useLocation, useNavigate } from 'react-router';
import { Header } from '../layout/Header';
import { StatusBar } from '../layout/StatusBar';
import { CommandPalette } from '../palette/CommandPalette';
import { PreferencesDialog } from './PreferencesDialog';
import { ForgetCredentialDialog } from './ForgetCredentialDialog';
import { ShortcutsPanel } from './ShortcutsPanel';
import { NavigationGuardDialog } from './NavigationGuardDialog';
import { MinWidthState } from './MinWidthState';
import { ToastContainer } from '../ui/Toast';
import { ErrorBoundary } from '../ui/ErrorBoundary';
import { useStackStore } from '../../stores/useStackStore';
import { requestInspectorLeave } from '../../stores/useInspectorToolsGuardStore';
import { useUIStore } from '../../stores/useUIStore';
import { usePolling } from '../../hooks/usePolling';
import { useViewportWidth } from '../../hooks/useViewportWidth';
import { MIN_SUPPORTED_WIDTH } from '../../lib/viewport';
import { useKeyboardShortcuts } from '../../hooks/useKeyboardShortcuts';
import { useCanvasActionsStore } from '../../stores/useCanvasActionsStore';
import { useShellNavStore } from '../../stores/useShellNavStore';
import { useGlobalCommands } from '../../hooks/useGlobalCommands';
import { useGuardedNavigate } from '../../hooks/useGuardedNavigate';
import { documentTitleForWorkspace, isWorkspace, type Workspace } from '../../types/workspace';
import { writePref } from '../../lib/preferences';
import { restoreNodeFocus } from '../../lib/inspectorFocus';
import { inWorkspace, navigateToWorkspace } from '../../lib/workspaceNav';

const HEADER_HEIGHT = 56;
const MODAL_SELECTOR = '[aria-modal="true"]';
const FOCUSABLE_SELECTOR =
  'button:not(:disabled), [href], input:not(:disabled), select:not(:disabled), textarea:not(:disabled), [tabindex]:not([tabindex="-1"])';
const STATUSBAR_HEIGHT = 32;

function workspaceFromPath(pathname: string): Workspace | null {
  // First segment after the leading slash, e.g. /stack/foo → "stack"
  const first = pathname.split('/').filter(Boolean)[0];
  return isWorkspace(first) ? first : null;
}

function persistLastWorkspace(ws: Workspace, stackId: string | null) {
  writePref('last-workspace', ws);
  if (stackId) writePref(`last-workspace:${stackId}`, ws);
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
  const setShortcutsOpen = useUIStore((s) => s.setShortcutsOpen);

  const navigate = useGuardedNavigate();
  const rawNavigate = useNavigate();
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

  // Store actions that open the Stack inspector navigate through here. Leaving
  // an unsaved editor asks first through the shared navigation guard.
  useEffect(() => {
    useShellNavStore.getState().register({
      goToStack: () => {
        if (inWorkspace(pathname, 'stack')) return;
        navigate('/stack');
      },
    });
    return () => useShellNavStore.getState().clear();
  }, [navigate, pathname]);

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
      requestInspectorLeave(() => {
        if (useUIStore.getState().sidebarOpen) {
          restoreNodeFocus(useStackStore.getState().selectedNodeId);
        }
        selectNode(null);
        setSidebarOpen(false);
      });
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
    // Same rule as the palette's Go to commands: the current workspace's
    // shortcut keeps its params, and leaving asks first when a draft is dirty.
    onSwitchToWorkspace: (id) =>
      navigateToWorkspace((to) => void rawNavigate(to), pathname, `/${id}`),
    onToggleCompactMode: () => toggleCompactMode(activeWorkspace),
    onOpenShortcuts: () => setShortcutsOpen(true),
  });

  useGlobalCommands({ onRefresh: handleRefresh });

  // Minimum supported width. The state never rises over an open dialog, and
  // a dismissal lasts until the page reloads.
  const viewportWidth = useViewportWidth();
  const [blocked, setBlocked] = useState(false);
  const [dismissed, setDismissed] = useState(false);
  const mainRef = useRef<HTMLElement>(null);
  const workspaceRef = useRef<HTMLDivElement>(null);
  const pendingDialogRef = useRef<HTMLElement | null>(null);

  // The open-dialog check reads the committed DOM, so this cannot be derived
  // during render.
  /* eslint-disable react-hooks/set-state-in-effect */
  useEffect(() => {
    if (viewportWidth >= MIN_SUPPORTED_WIDTH) {
      setBlocked(false);
      return;
    }
    if (!dismissed && document.querySelector(MODAL_SELECTOR) === null) setBlocked(true);
  }, [viewportWidth, pathname, dismissed]);
  /* eslint-enable react-hooks/set-state-in-effect */

  // A workspace dialog opened under the state (for example from the palette)
  // takes over: the state gives way without counting as a dismissal. Scoped
  // to the workspace wrapper so the palette's own aria-modal does not count.
  useEffect(() => {
    if (!blocked) return;
    const wrapper = workspaceRef.current;
    if (!wrapper) return;
    const check = () => {
      const dialog = wrapper.querySelector<HTMLElement>(MODAL_SELECTOR);
      if (!dialog) return;
      pendingDialogRef.current = dialog;
      setBlocked(false);
    };
    const observer = new MutationObserver(check);
    observer.observe(wrapper, {
      childList: true,
      subtree: true,
      attributes: true,
      attributeFilter: ['aria-modal'],
    });
    check();
    return () => observer.disconnect();
  }, [blocked]);

  // The dialog's focus trap ran while the wrapper was inert and could not
  // take focus, so move it in once the wrapper is live again.
  useEffect(() => {
    if (blocked) return;
    const dialog = pendingDialogRef.current;
    pendingDialogRef.current = null;
    if (!dialog || !dialog.isConnected || dialog.contains(document.activeElement)) return;
    const target = dialog.querySelector<HTMLElement>(FOCUSABLE_SELECTOR) ?? dialog;
    target.focus();
  }, [blocked]);

  const continueAnyway = useCallback(() => {
    setDismissed(true);
    setBlocked(false);
    mainRef.current?.focus();
  }, []);

  return (
    <div
      className="h-screen w-screen overflow-hidden bg-background"
      style={{
        display: 'grid',
        gridTemplateRows: `${HEADER_HEIGHT}px 1fr ${STATUSBAR_HEIGHT}px`,
        gridTemplateColumns: 'minmax(0, 1fr)',
      }}
    >
      <Header onRefresh={handleRefresh} isRefreshing={isRefreshing} />

      <main ref={mainRef} tabIndex={-1} className="relative overflow-hidden" style={{ minHeight: 100 }}>
        {/* Contain a workspace render throw so the shell (header, nav, status
            bar) stays mounted and the user can navigate away. Resetting on the
            route path lets navigation recover a crashed workspace without a
            full reload. The wrapper is always rendered so toggling inert
            never remounts the workspace. */}
        <div ref={workspaceRef} className="contents" inert={blocked}>
          <ErrorBoundary variant="inline" resetKey={pathname}>
            <Outlet />
          </ErrorBoundary>
        </div>
        {blocked && <MinWidthState onContinue={continueAnyway} />}
      </main>

      <StatusBar />

      <ToastContainer />

      <CommandPalette
        isOpen={commandPaletteOpen}
        onClose={() => setCommandPaletteOpen(false)}
      />
      <PreferencesDialog />
      <ForgetCredentialDialog />
      <ShortcutsPanel />
      <NavigationGuardDialog />
    </div>
  );
}

export default AppShell;
