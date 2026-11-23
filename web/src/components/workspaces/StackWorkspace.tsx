import { useCallback, useId, useMemo, useRef, useState } from 'react';
import { ReactFlowProvider } from '@xyflow/react';
import { useSearchParams } from 'react-router';
import { Sidebar } from '../layout/Sidebar';
import { Canvas } from '../graph/Canvas';
import { AccessLens } from '../stack/AccessLens';
import { SpecPane } from '../spec/SpecPane';
import { ResizeHandle } from '../ui/ResizeHandle';
import { FailedState, FailureBanner } from '../ui/EmptyState';
import { useStackStore } from '../../stores/useStackStore';
import { useUIStore } from '../../stores/useUIStore';
import { usePollingRefresh } from '../../hooks/usePolling';
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

// Stack workspace body: canvas, loading/error overlays, and the right-rail
// inspector. Rendered inside <AppShell>'s <main> outlet. CSS grid: a canvas
// column plus a collapsible inspector column (0px when closed) so switching
// workspaces doesn't shift the canvas.
export function StackWorkspace() {
  const headingId = useId();
  const [sidebarWidth, setSidebarWidth] = useState(SIDEBAR_DEFAULT);
  const containerRef = useRef<HTMLDivElement>(null);
  const [searchParams, setSearchParams] = useSearchParams();

  // ?spec=1 opens the spec slide-over; the status-bar chip and the palette's
  // "Open Spec Editor" deep-link it. Closing clears the flag.
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
  const sidebarOpen = useUIStore((s) => s.sidebarOpen);

  const accessServers = useMemo(
    () => [...mcpServers].sort((a, b) => a.name.localeCompare(b.name)),
    [mcpServers],
  );

  const phase = refreshPhase({ error, hasLoaded: lastUpdated !== null });
  // A re-entered credential sets isLoading again; once a graph has loaded it
  // stays on screen instead of going behind the loading overlay.
  const showLoading = phase === 'loading' && isLoading;
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
      <div className="absolute inset-0 overflow-hidden">
        <h1 id={headingId} className="sr-only">{WORKSPACE_LABELS.stack}</h1>
        <div
          ref={containerRef}
          className="grid h-full"
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
              {phase === 'stale' && error && (
                <FailureBanner
                  message={error}
                  onRetry={handleRefresh}
                  lastUpdated={lastUpdated}
                  className="mx-3 mt-2 mb-2 flex-shrink-0"
                />
              )}
              <div className="relative flex-1 min-h-0">
                {/* Desaturate only the graph; the spec and Access Lens editors stay at full color. */}
                <div
                  className={cn(
                    'absolute inset-0 transition motion-reduce:transition-none',
                    phase === 'stale' && 'grayscale opacity-70',
                  )}
                >
                  <Canvas />
                </div>

                {/* Access Lens authoring surface: header toggle, slide-over editor,
                    action bar, commit gate, and dirty-exit guard. Mounted beside
                    the canvas so its absolute overlays anchor to the canvas. */}
                <AccessLens servers={accessServers} />

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
