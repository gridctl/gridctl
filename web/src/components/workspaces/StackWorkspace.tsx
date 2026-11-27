import { useCallback, useEffect, useId, useMemo, useRef, useState } from 'react';
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
import { useLiveSearchParams } from '../../hooks/useLiveSearchParams';
import { WORKSPACE_LABELS } from '../../types/workspace';
import { cn } from '../../lib/cn';
import { refreshPhase } from '../../lib/refreshPhase';

const SIDEBAR_DEFAULT = 320;
const SIDEBAR_MIN = 280;
const SIDEBAR_MAX = 600;
const SPEC_DEFAULT = 560;
export const SPEC_MIN = 360;
export const SPEC_MAX = 960;

// Exported as a test seam. A zero clientWidth (jsdom, first paint) falls back
// to the bounds. Otherwise the open rail stays between 40% and 60% of the
// container, and the bounds win once the container is wide enough. The bounds
// default to the inspector's; the spec column passes its own.
// eslint-disable-next-line react-refresh/only-export-components -- test seam
export function clampSidebarWidth(
  next: number,
  containerWidth: number,
  min: number = SIDEBAR_MIN,
  max: number = SIDEBAR_MAX,
): number {
  if (containerWidth > 0) {
    const lo = Math.min(min, Math.floor(containerWidth * 0.4));
    const hi = Math.min(max, Math.floor(containerWidth * 0.6));
    return Math.min(hi, Math.max(lo, next));
  }
  return Math.min(max, Math.max(min, next));
}

// Stack workspace body: a slim header, then a toolbar over the graph or table
// body, loading/error overlays, and the right-rail inspector. Rendered inside <AppShell>'s <main>
// outlet. CSS grid: a canvas column plus one collapsible right column (0px
// when closed) holding the spec or the inspector, never both, so switching
// workspaces doesn't shift the canvas.
export function StackWorkspace() {
  const headingId = useId();
  const [sidebarWidth, setSidebarWidth] = useState(SIDEBAR_DEFAULT);
  const [specWidth, setSpecWidth] = useState(SPEC_DEFAULT);
  const containerRef = useRef<HTMLDivElement>(null);
  const [searchParams, setSearchParams] = useSearchParams();

  // ?node=<id> mirrors the selected node (useNodeParamSync). ?spec=1 opens
  // the spec column; the status-bar chip and the palette's "Open spec"
  // deep-link it. Closing clears the flag.
  useNodeParamSync();
  const specOpen = searchParams.get('spec') === '1';
  const liveParams = useLiveSearchParams();
  const closeSpec = useCallback(() => {
    setSearchParams(
      (prev) => {
        const params = liveParams(prev);
        params.delete('spec');
        return params;
      },
      { replace: true },
    );
  }, [setSearchParams, liveParams]);

  // Opening the spec closes the other right-side panels.
  useEffect(() => {
    if (!specOpen) return;
    useUIStore.getState().setSidebarOpen(false);
    useAccessLensStore.getState().closeSlideOver();
  }, [specOpen]);

  // While the spec is open, a newly opened inspector or access editor replaces it.
  useEffect(() => {
    if (!specOpen) return;
    const offUI = useUIStore.subscribe((s, prev) => {
      if (s.sidebarOpen && !prev.sidebarOpen) closeSpec();
    });
    const offLens = useAccessLensStore.subscribe((s, prev) => {
      if (s.slideOverOpen && !prev.slideOverOpen) closeSpec();
    });
    return () => {
      offUI();
      offLens();
    };
  }, [specOpen, closeSpec]);

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
  const rightPanel: 'spec' | 'inspector' | null = specOpen ? 'spec' : sidebarOpen ? 'inspector' : null;
  const rightWidth = rightPanel === 'spec' ? specWidth : sidebarWidth;

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

  const handleSpecResize = useCallback((delta: number) => {
    const containerWidth = containerRef.current?.clientWidth ?? 0;
    setSpecWidth((prev) => clampSidebarWidth(prev + delta, containerWidth, SPEC_MIN, SPEC_MAX));
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
            gridTemplateColumns: `minmax(0, 1fr) ${rightPanel ? `min(${rightWidth}px, 60%)` : '0px'}`,
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
                    {/* Desaturate only the graph; the Access Lens editor stays at full color. */}
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
              </div>
            </div>
          </div>

          {rightPanel === 'spec' && <SpecPane onClose={closeSpec} onResize={handleSpecResize} />}

          {rightPanel === 'inspector' && (
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
