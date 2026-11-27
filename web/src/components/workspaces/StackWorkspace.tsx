import { useCallback, useId, useMemo, useRef, useState } from 'react';
import { ReactFlowProvider } from '@xyflow/react';
import { useSearchParams } from 'react-router';
import { Sidebar } from '../layout/Sidebar';
import { Canvas } from '../graph/Canvas';
import { AccessLens } from '../stack/AccessLens';
import { StackToolbar } from '../stack/StackToolbar';
import { StackTable, type StackTableHandle } from '../stack/StackTable';
import { SpecPane } from '../spec/SpecPane';
import { ResizeHandle } from '../ui/ResizeHandle';
import { FailedState, FailureBanner } from '../ui/EmptyState';
import { WorkspaceHeader } from '../ui/WorkspaceHeader';
import { useStackStore } from '../../stores/useStackStore';
import { useUIStore } from '../../stores/useUIStore';
import { useAccessLensStore } from '../../stores/useAccessLensStore';
import { STACK_TABLE_AUTO_THRESHOLD, type StackFacet, type StackFilter } from '../../lib/stackRows';
import { usePollingRefresh } from '../../hooks/usePolling';
import { useNodeParamSync } from '../../hooks/useNodeParamSync';
import { WORKSPACE_LABELS } from '../../types/workspace';
import { cn } from '../../lib/cn';
import { refreshPhase } from '../../lib/refreshPhase';

const SIDEBAR_DEFAULT = 320;
const SIDEBAR_MIN = 280;
const SIDEBAR_MAX = 600;

// Exported as a test seam. A zero clientWidth (jsdom, first paint) falls back
// to the constants. Otherwise the open rail stays between 40% and 60% of the
// container, and the constants win once the container is wide enough.
// eslint-disable-next-line react-refresh/only-export-components -- test seam
export function clampSidebarWidth(next: number, containerWidth: number): number {
  if (containerWidth > 0) {
    const lo = Math.min(SIDEBAR_MIN, Math.floor(containerWidth * 0.4));
    const hi = Math.min(SIDEBAR_MAX, Math.floor(containerWidth * 0.6));
    return Math.min(hi, Math.max(lo, next));
  }
  return Math.min(SIDEBAR_MAX, Math.max(SIDEBAR_MIN, next));
}

// Stack workspace body: a slim header, then a toolbar over the graph or table
// body, loading/error overlays, and the right-rail inspector. Rendered inside <AppShell>'s <main>
// outlet. CSS grid: a canvas column plus a collapsible inspector column (0px
// when closed) so switching workspaces doesn't shift the canvas.
export function StackWorkspace() {
  const headingId = useId();
  const [sidebarWidth, setSidebarWidth] = useState(SIDEBAR_DEFAULT);
  const containerRef = useRef<HTMLDivElement>(null);
  const [searchParams, setSearchParams] = useSearchParams();

  // ?node=<id> mirrors the selected node (useNodeParamSync). ?spec=1 opens
  // the spec slide-over; the status-bar chip and the palette's "Open Spec
  // Editor" deep-link it. Closing clears the flag.
  useNodeParamSync();
  const specOpen = searchParams.get('spec') === '1';
  const closeSpec = useCallback(() => {
    setSearchParams(
      (prev) => {
        const params = new URLSearchParams(prev);
        params.delete('spec');
        return params;
      },
      { replace: true },
    );
  }, [setSearchParams]);

  const isLoading = useStackStore((s) => s.isLoading);
  const error = useStackStore((s) => s.error);
  const connectionStatus = useStackStore((s) => s.connectionStatus);
  const lastUpdated = useStackStore((s) => s.lastUpdated);
  const mcpServers = useStackStore((s) => s.mcpServers);
  const tools = useStackStore((s) => s.tools);
  const sidebarOpen = useUIStore((s) => s.sidebarOpen);
  const gatewayInfo = useStackStore((s) => s.gatewayInfo);
  const storedView = useUIStore((s) => s.stackPrefs.view);
  const lensEnabled = useAccessLensStore((s) => s.enabled);

  // The toolbar filter is transient: component state, not persisted or in the URL.
  const [query, setQuery] = useState('');
  const [facets, setFacets] = useState<Set<StackFacet>>(() => new Set());
  const filter = useMemo<StackFilter>(() => ({ query, facets }), [query, facets]);
  const clearFilter = useCallback(() => {
    setQuery('');
    setFacets(new Set());
  }, []);

  // With no stored choice, a large fleet opens on the table. Decided once per
  // mount at the first status and never written to the preference.
  const [autoView, setAutoView] = useState<'graph' | 'table' | null>(null);
  if (autoView === null && gatewayInfo !== null) {
    setAutoView(mcpServers.length > STACK_TABLE_AUTO_THRESHOLD ? 'table' : 'graph');
  }
  const view: 'graph' | 'table' = lensEnabled ? 'graph' : (storedView ?? autoView ?? 'graph');

  const tableRef = useRef<StackTableHandle>(null);
  const focusTable = useCallback(() => tableRef.current?.focusActiveRow(), []);

  const accessServers = useMemo(
    () => [...mcpServers].sort((a, b) => a.name.localeCompare(b.name)),
    [mcpServers],
  );

  const hasLoaded = lastUpdated !== null;
  const phase = refreshPhase({ error, hasLoaded });
  // A re-entered credential sets isLoading again; once a graph has loaded it
  // stays on screen instead of going behind the loading overlay. Before that,
  // the loading overlay covers a pending cycle even after a failed first load.
  const showLoading = !hasLoaded && isLoading;
  const showFailed = phase === 'failed' && !isLoading;
  const overlayUp = showLoading || showFailed;

  const refresh = usePollingRefresh();

  const handleRefresh = useCallback(async () => {
    await refresh();
  }, [refresh]);

  const handleSidebarResize = useCallback((delta: number) => {
    // Measure once per drag event, outside the state updater.
    const containerWidth = containerRef.current?.clientWidth ?? 0;
    setSidebarWidth((prev) => clampSidebarWidth(prev + delta, containerWidth));
  }, []);

  return (
    <ReactFlowProvider>
      <div className="absolute inset-0 flex flex-col overflow-hidden">
        <WorkspaceHeader
          title={WORKSPACE_LABELS.stack}
          headingId={headingId}
          compact
          summary={hasLoaded ? (
            <span className="font-mono">
              {mcpServers.length} {mcpServers.length === 1 ? 'MCP server' : 'MCP servers'}
              {' · '}
              {tools.length} {tools.length === 1 ? 'tool' : 'tools'}
            </span>
          ) : undefined}
        />
        <div
          ref={containerRef}
          data-testid="stack-grid"
          className="grid flex-1 min-h-0"
          style={{
            // min() is a backstop after a window resize without a drag. A CSS-only
            // clamp would pin the track at 60% and ignore the drag below 280px.
            gridTemplateColumns: `minmax(0, 1fr) ${sidebarOpen ? `min(${sidebarWidth}px, 60%)` : '0px'}`,
          }}
        >
          <div className="relative overflow-hidden">
            {/* Loading State */}
            {showLoading && (
              <div className="absolute inset-0 flex items-center justify-center bg-background/90 backdrop-blur-sm z-overlay">
                <div className="text-center space-y-5 animate-fade-in-scale">
                  <div className="relative mx-auto w-16 h-16">
                    <div className="absolute inset-0 rounded-full border-2 border-primary/20" />
                    <div className="absolute inset-0 rounded-full border-2 border-primary border-t-transparent animate-spin" />
                    <div
                      className="absolute inset-2 rounded-full border-2 border-secondary/30 border-b-transparent animate-spin"
                      style={{ animationDirection: 'reverse', animationDuration: '1.5s' }}
                    />
                  </div>
                  <div>
                    <p className="text-text-secondary font-medium">Loading stack</p>
                    <p className="text-text-muted text-sm mt-1">Connecting to gateway...</p>
                  </div>
                </div>
              </div>
            )}

            {/* Error State: nothing loaded yet */}
            {showFailed && error && (
              <div className="absolute inset-0 flex items-center justify-center bg-background z-overlay">
                <FailedState
                  feature="Stack"
                  message={
                    connectionStatus === 'disconnected'
                      ? `${error} The gateway may have been shut down or restarted. It will reconnect automatically when available.`
                      : error
                  }
                  onRetry={handleRefresh}
                />
              </div>
            )}

            {/* Overlays stay outside this wrapper so their Retry stays usable
                while everything they cover is inert. */}
            <div inert={overlayUp} className="absolute inset-0 flex flex-col">
              <StackToolbar
                filter={filter}
                view={view}
                onQueryChange={setQuery}
                onFacetsChange={setFacets}
                onFocusTable={focusTable}
              />
              {phase === 'stale' && error && (
                <FailureBanner
                  message={error}
                  onRetry={handleRefresh}
                  lastUpdated={lastUpdated}
                  className="mx-3 mt-2 mb-2 flex-shrink-0"
                />
              )}
              <div className="relative flex-1 min-h-0 overflow-hidden">
                {view === 'graph' ? (
                  <>
                    {/* Desaturate only the graph; the spec and Access Lens editors stay at full color. */}
                    <div
                      className={cn(
                        'absolute inset-0 transition motion-reduce:transition-none',
                        phase === 'stale' && 'grayscale opacity-70',
                      )}
                    >
                      <Canvas filter={filter} />
                    </div>

                    {/* Access Lens authoring surface: header toggle, slide-over editor,
                        action bar, commit gate, and dirty-exit guard. Mounted beside
                        the canvas so its absolute overlays anchor to the canvas. */}
                    <AccessLens servers={accessServers} />
                  </>
                ) : (
                  <StackTable ref={tableRef} filter={filter} onClearFilter={clearFilter} />
                )}

                {specOpen && <SpecPane onClose={closeSpec} />}
              </div>
            </div>
          </div>

          {sidebarOpen && (
            <aside aria-label="Stack inspector" className="flex flex-row overflow-hidden bg-surface-elevated border-l border-border shadow-pane-left">
              <ResizeHandle
                direction="vertical"
                onResize={handleSidebarResize}
                className="flex-shrink-0"
              />
              <div className="flex-1 min-w-0 overflow-hidden">
                <Sidebar />
              </div>
            </aside>
          )}
        </div>
      </div>
    </ReactFlowProvider>
  );
}

export default StackWorkspace;
