import { useCallback, useMemo, useState } from 'react';
import { AlertCircle, RefreshCw, WifiOff } from 'lucide-react';
import { Sidebar } from '../layout/Sidebar';
import { Canvas } from '../graph/Canvas';
import { ClientAccessEditor } from './ClientAccessEditor';
import { ResizeHandle } from '../ui/ResizeHandle';
import { useStackStore } from '../../stores/useStackStore';
import { useUIStore } from '../../stores/useUIStore';
import { usePolling } from '../../hooks/usePolling';

const SIDEBAR_DEFAULT = 320;
const SIDEBAR_MIN = 280;
const SIDEBAR_MAX = 600;

// Topology workspace body: canvas, loading/error overlays, and the right-rail
// inspector. Rendered inside <AppShell>'s <main> outlet. CSS grid: a canvas
// column plus a collapsible inspector column (0px when closed) so switching
// workspaces doesn't shift the canvas.
export function TopologyWorkspace() {
  const [sidebarWidth, setSidebarWidth] = useState(SIDEBAR_DEFAULT);

  const isLoading = useStackStore((s) => s.isLoading);
  const error = useStackStore((s) => s.error);
  const mcpServers = useStackStore((s) => s.mcpServers);
  const sidebarOpen = useUIStore((s) => s.sidebarOpen);
  const accessEditorOpen = useUIStore((s) => s.accessEditorOpen);
  const accessEditorSeedSlug = useUIStore((s) => s.accessEditorSeedSlug);
  const closeAccessEditor = useUIStore((s) => s.closeAccessEditor);

  const accessServers = useMemo(
    () => [...mcpServers].sort((a, b) => a.name.localeCompare(b.name)),
    [mcpServers],
  );

  const { refresh } = usePolling();

  const handleRefresh = useCallback(async () => {
    await refresh();
  }, [refresh]);

  const handleSidebarResize = useCallback((delta: number) => {
    setSidebarWidth((prev) => {
      const newWidth = prev + delta;
      return Math.min(SIDEBAR_MAX, Math.max(SIDEBAR_MIN, newWidth));
    });
  }, []);

  return (
    <div className="absolute inset-0 overflow-hidden">
      <div
        className="grid h-full"
        style={{
          gridTemplateColumns: `minmax(0, 1fr) ${sidebarOpen ? sidebarWidth : 0}px`,
        }}
      >
        <div className="relative overflow-hidden">
          {/* Loading State */}
          {isLoading && (
            <div className="absolute inset-0 flex items-center justify-center bg-background/90 backdrop-blur-sm z-30">
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

          {/* Error State */}
          {error && !isLoading && (
            <div className="absolute inset-0 flex items-center justify-center bg-background/90 backdrop-blur-sm z-30">
              <div className="text-center space-y-5 max-w-md p-8 animate-fade-in-scale">
                <div className="relative mx-auto w-20 h-20">
                  <div className="absolute inset-0 bg-status-error/20 rounded-2xl blur-xl" />
                  <div className="relative w-full h-full bg-status-error/10 rounded-2xl border border-status-error/20 flex items-center justify-center">
                    {error.includes('unavailable') ? (
                      <WifiOff size={32} className="text-status-error" />
                    ) : (
                      <AlertCircle size={32} className="text-status-error" />
                    )}
                  </div>
                </div>
                <div>
                  <h2 className="text-lg font-semibold text-text-primary">
                    {error.includes('unavailable') ? 'Gateway Unavailable' : 'Connection Error'}
                  </h2>
                  <p className="text-sm text-text-muted mt-2 leading-relaxed">{error}</p>
                  {error.includes('unavailable') && (
                    <p className="text-xs text-text-muted mt-3">
                      The gateway may have been shut down or restarted. It will reconnect automatically when available.
                    </p>
                  )}
                </div>
                <button
                  onClick={handleRefresh}
                  className="inline-flex items-center gap-2 px-5 py-2.5 bg-gradient-to-r from-primary to-primary-dark text-background font-semibold rounded-lg hover:from-primary-light hover:to-primary transition-all shadow-glow-primary hover:shadow-[0_0_30px_rgba(245,158,11,0.3)]"
                >
                  <RefreshCw size={16} />
                  Retry Connection
                </button>
              </div>
            </div>
          )}

          <Canvas />
        </div>

        {sidebarOpen && (
          <aside className="flex flex-row overflow-hidden bg-surface/80 backdrop-blur-xl border-l border-border/50">
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

      {/* Per-client access editor, opened from the inspector's "Edit Scope"
          seeded to the inspected client. */}
      <ClientAccessEditor
        key={accessEditorSeedSlug ?? 'default'}
        isOpen={accessEditorOpen}
        onClose={closeAccessEditor}
        servers={accessServers}
        initialSlug={accessEditorSeedSlug}
      />
    </div>
  );
}

export default TopologyWorkspace;
