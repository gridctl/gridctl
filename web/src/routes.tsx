import { Suspense, lazy } from 'react';
import { Route, Routes } from 'react-router-dom';
import { AppShell } from './components/shell/AppShell';
import { RootRedirect } from './components/shell/RootRedirect';
import { AgentRedirect } from './components/shell/AgentRedirect';
import { WorkspaceLoadingShell } from './components/shell/WorkspaceLoadingShell';
import { DetachedLogsPage } from './pages/DetachedLogsPage';
import { DetachedSidebarPage } from './pages/DetachedSidebarPage';
import { DetachedEditorPage } from './pages/DetachedEditorPage';
import { DetachedRegistryPage } from './pages/DetachedRegistryPage';
import { DetachedMetricsPage } from './pages/DetachedMetricsPage';
import { DetachedVaultPage } from './pages/DetachedVaultPage';
import { DetachedTracesPage } from './pages/DetachedTracesPage';

// Each workspace is code-split into its own chunk. Phase 2/3 will fill in
// the Skills and Runs workspaces; today they render placeholder cards.
const TopologyWorkspace = lazy(() => import('./components/workspaces/TopologyWorkspace'));
const SkillsWorkspace = lazy(() => import('./components/workspaces/SkillsWorkspace'));
const RunsWorkspace = lazy(() => import('./components/workspaces/RunsWorkspace'));
const RunDetailWorkspace = lazy(() => import('./components/workspaces/RunDetailWorkspace'));

export function AppRoutes() {
  return (
    <Routes>
      {/* Unified shell parent route. The three workspaces render as
          children inside <AppShell>'s <Outlet />. */}
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
          path="/skills"
          element={
            <Suspense fallback={<WorkspaceLoadingShell />}>
              <SkillsWorkspace />
            </Suspense>
          }
        />
        <Route
          path="/runs"
          element={
            <Suspense fallback={<WorkspaceLoadingShell />}>
              <RunsWorkspace />
            </Suspense>
          }
        />
        <Route
          path="/runs/:runID"
          element={
            <Suspense fallback={<WorkspaceLoadingShell />}>
              <RunDetailWorkspace />
            </Suspense>
          }
        />
      </Route>

      {/* Root redirect — chooses a workspace based on stack + storage. */}
      <Route path="/" element={<RootRedirect />} />

      {/* /agent permanently redirects to /skills, preserving query/hash. */}
      <Route path="/agent" element={<AgentRedirect />} />

      {/* Detached windows stay frameless — outside AppShell on purpose. */}
      <Route path="/logs" element={<DetachedLogsPage />} />
      <Route path="/sidebar" element={<DetachedSidebarPage />} />
      <Route path="/editor" element={<DetachedEditorPage />} />
      <Route path="/registry" element={<DetachedRegistryPage />} />
      <Route path="/metrics" element={<DetachedMetricsPage />} />
      <Route path="/vault" element={<DetachedVaultPage />} />
      <Route path="/traces" element={<DetachedTracesPage />} />
    </Routes>
  );
}
