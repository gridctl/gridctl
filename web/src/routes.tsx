import { Suspense, lazy } from 'react';
import { Navigate, Route, Routes } from 'react-router-dom';
import { AppShell } from './components/shell/AppShell';
import { RootRedirect } from './components/shell/RootRedirect';
import { WorkspaceLoadingShell } from './components/shell/WorkspaceLoadingShell';
import { DetachedLogsPage } from './pages/DetachedLogsPage';
import { DetachedSidebarPage } from './pages/DetachedSidebarPage';
import { DetachedEditorPage } from './pages/DetachedEditorPage';
import { DetachedRegistryPage } from './pages/DetachedRegistryPage';
import { DetachedMetricsPage } from './pages/DetachedMetricsPage';
import { DetachedTracesPage } from './pages/DetachedTracesPage';
import { useThemeSync } from './themes/useThemeSync';

// Each workspace is code-split into its own chunk.
const StackWorkspace = lazy(() => import('./components/workspaces/StackWorkspace'));
const LibraryWorkspace = lazy(() => import('./components/workspaces/LibraryWorkspace'));
const VaultWorkspace = lazy(() => import('./components/workspaces/VaultWorkspace'));
const ToolsWorkspace = lazy(() => import('./components/workspaces/ToolsWorkspace'));
const MetricsWorkspace = lazy(() => import('./components/workspaces/MetricsWorkspace'));
const PinsWorkspace = lazy(() => import('./components/workspaces/PinsWorkspace'));
const LogsWorkspace = lazy(() => import('./components/workspaces/LogsWorkspace'));
const TracesWorkspace = lazy(() => import('./components/workspaces/TracesWorkspace'));
const ConnectionsWorkspace = lazy(() => import('./components/workspaces/ConnectionsWorkspace'));

export function AppRoutes() {
  // Single mount point for theme application + cross-window sync; covers the
  // main shell and every detached popout route below.
  useThemeSync();

  return (
    <Routes>
      {/* Unified shell parent route. Workspaces render as children inside
          <AppShell>'s <Outlet />. */}
      <Route element={<AppShell />}>
        <Route
          path="/stack"
          element={
            <Suspense fallback={<WorkspaceLoadingShell />}>
              <StackWorkspace />
            </Suspense>
          }
        />
        <Route
          path="/library"
          element={
            <Suspense fallback={<WorkspaceLoadingShell />}>
              <LibraryWorkspace />
            </Suspense>
          }
        />
        {/* /library/:skillName deep-links the editor for a single skill;
            the workspace component looks up the name and either mounts
            the SkillEditor or falls back with a toast. */}
        <Route
          path="/library/:skillName"
          element={
            <Suspense fallback={<WorkspaceLoadingShell />}>
              <LibraryWorkspace />
            </Suspense>
          }
        />
        <Route
          path="/vault"
          element={
            <Suspense fallback={<WorkspaceLoadingShell />}>
              <VaultWorkspace />
            </Suspense>
          }
        />
        <Route
          path="/tools"
          element={
            <Suspense fallback={<WorkspaceLoadingShell />}>
              <ToolsWorkspace />
            </Suspense>
          }
        />
        <Route
          path="/metrics"
          element={
            <Suspense fallback={<WorkspaceLoadingShell />}>
              <MetricsWorkspace />
            </Suspense>
          }
        />
        <Route
          path="/pins"
          element={
            <Suspense fallback={<WorkspaceLoadingShell />}>
              <PinsWorkspace />
            </Suspense>
          }
        />
        <Route
          path="/logs"
          element={
            <Suspense fallback={<WorkspaceLoadingShell />}>
              <LogsWorkspace />
            </Suspense>
          }
        />
        <Route
          path="/traces"
          element={
            <Suspense fallback={<WorkspaceLoadingShell />}>
              <TracesWorkspace />
            </Suspense>
          }
        />
        <Route
          path="/connections"
          element={
            <Suspense fallback={<WorkspaceLoadingShell />}>
              <ConnectionsWorkspace />
            </Suspense>
          }
        />
      </Route>

      {/* Root redirect — chooses a workspace based on stack + storage. */}
      <Route path="/" element={<RootRedirect />} />

      {/* Bookmark redirects for the workspaces removed when the agent runtime
          was retired. Keep through v1.0 so existing links don't 404. */}
      <Route path="/skills" element={<Navigate to="/library" replace />} />
      <Route path="/runs" element={<Navigate to="/library" replace />} />
      <Route path="/runs/:runID" element={<Navigate to="/library" replace />} />
      {/* /topology → /stack: the workspace was renamed when the UI label
          caught up with the backend's Topology→Stack migration. */}
      <Route path="/topology" element={<Navigate to="/stack" replace />} />
      <Route path="/agent" element={<Navigate to="/library" replace />} />

      {/* Detached windows stay frameless — outside AppShell on purpose. */}
      <Route path="/sidebar" element={<DetachedSidebarPage />} />
      <Route path="/editor" element={<DetachedEditorPage />} />
      <Route path="/library-window" element={<DetachedRegistryPage />} />
      {/* /registry → /library-window: silent redirect for bookmarks and
          existing detached window handles. */}
      <Route path="/registry" element={<Navigate to="/library-window" replace />} />
      {/* /metrics, /logs, and /traces are in-shell workspaces; each detached
          popout renders at /<type>-window (window type keys stay put). */}
      <Route path="/metrics-window" element={<DetachedMetricsPage />} />
      <Route path="/logs-window" element={<DetachedLogsPage />} />
      <Route path="/traces-window" element={<DetachedTracesPage />} />

      {/* Catch-all: any unmatched URL (typo, stale bookmark, removed route)
          redirects to the root, where RootRedirect resolves the landing
          workspace. Keeps unknown paths from rendering a blank page. */}
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
