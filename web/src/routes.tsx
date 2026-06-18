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

// Each workspace is code-split into its own chunk.
const TopologyWorkspace = lazy(() => import('./components/workspaces/TopologyWorkspace'));
const LibraryWorkspace = lazy(() => import('./components/workspaces/LibraryWorkspace'));
const VaultWorkspace = lazy(() => import('./components/workspaces/VaultWorkspace'));
const ToolsWorkspace = lazy(() => import('./components/workspaces/ToolsWorkspace'));
const MetricsWorkspace = lazy(() => import('./components/workspaces/MetricsWorkspace'));

export function AppRoutes() {
  return (
    <Routes>
      {/* Unified shell parent route. Workspaces render as children inside
          <AppShell>'s <Outlet />. */}
      <Route element={<AppShell />}>
        <Route
          path="/topology"
          element={
            <Suspense fallback={<WorkspaceLoadingShell />}>
              <TopologyWorkspace />
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
      </Route>

      {/* Root redirect — chooses a workspace based on stack + storage. */}
      <Route path="/" element={<RootRedirect />} />

      {/* Bookmark redirects for the workspaces removed when the agent runtime
          was retired. Keep through v1.0 so existing links don't 404. */}
      <Route path="/skills" element={<Navigate to="/library" replace />} />
      <Route path="/runs" element={<Navigate to="/library" replace />} />
      <Route path="/runs/:runID" element={<Navigate to="/library" replace />} />
      <Route path="/agent" element={<Navigate to="/library" replace />} />

      {/* Detached windows stay frameless — outside AppShell on purpose. */}
      <Route path="/logs" element={<DetachedLogsPage />} />
      <Route path="/sidebar" element={<DetachedSidebarPage />} />
      <Route path="/editor" element={<DetachedEditorPage />} />
      <Route path="/library-window" element={<DetachedRegistryPage />} />
      {/* /registry → /library-window: silent redirect for bookmarks and
          existing detached window handles. */}
      <Route path="/registry" element={<Navigate to="/library-window" replace />} />
      {/* /metrics is now the in-shell Metrics workspace; the detached popout
          renders at /metrics-window (window type key stays `metrics`). */}
      <Route path="/metrics-window" element={<DetachedMetricsPage />} />
      <Route path="/traces" element={<DetachedTracesPage />} />

      {/* Catch-all: any unmatched URL (typo, stale bookmark, removed route)
          redirects to the root, where RootRedirect resolves the landing
          workspace. Keeps unknown paths from rendering a blank page. */}
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
