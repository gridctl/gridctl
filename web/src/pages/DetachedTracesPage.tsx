import { useEffect, useState } from 'react';
import { Activity, Maximize2, Minimize2 } from 'lucide-react';
import { IconButton } from '../components/ui/IconButton';
import { useDetachedWindowSync } from '../hooks/useBroadcastChannel';
import { fetchMCPServers } from '../lib/api';
import { TracesView } from '../components/traces/TracesView';
import { ErrorBoundary } from '../components/ui/ErrorBoundary';

// Frameless traces popout. The trace surface itself is the shared TracesView;
// this page only adds the window chrome (title bar, fullscreen, footer). The
// detached window runs its own store instance, so nothing here bleeds into
// the main shell's traces state.
function DetachedTracesPageContent() {
  const [servers, setServers] = useState<string[]>([]);
  const [isFullscreen, setIsFullscreen] = useState(false);

  useDetachedWindowSync('traces');

  // Load deployed server names for the filter dropdown. The detached window
  // has no stack-store poller, so fetch directly.
  useEffect(() => {
    fetchMCPServers()
      .then((list) => setServers(list.map((s) => s.name).sort()))
      .catch(() => {});
  }, []);

  const toggleFullscreen = async () => {
    if (!document.fullscreenElement) {
      await document.documentElement.requestFullscreen();
      setIsFullscreen(true);
    } else {
      await document.exitFullscreen();
      setIsFullscreen(false);
    }
  };

  useEffect(() => {
    const handler = () => setIsFullscreen(!!document.fullscreenElement);
    document.addEventListener('fullscreenchange', handler);
    return () => document.removeEventListener('fullscreenchange', handler);
  }, []);

  return (
    <div className="h-screen w-screen bg-background flex flex-col overflow-hidden">
      {/* Background grain */}
      <div
        className="fixed inset-0 pointer-events-none z-0 opacity-[0.015]"
        style={{
          backgroundImage: `url("data:image/svg+xml,%3Csvg viewBox='0 0 256 256' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='noise'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.9' numOctaves='4' stitchTiles='stitch'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23noise)'/%3E%3C/svg%3E")`,
        }}
      />

      {/* Header */}
      <header className="h-12 flex-shrink-0 bg-surface/90 backdrop-blur-xl border-b border-border/50 flex items-center justify-between px-4 z-10 relative">
        <div className="absolute top-0 left-0 right-0 h-px bg-gradient-to-r from-transparent via-primary/30 to-transparent" />

        <div className="flex items-center gap-3">
          <div className="p-1.5 rounded-lg border bg-primary/10 border-primary/20">
            <Activity size={14} className="text-primary" />
          </div>
          <span className="text-sm font-semibold text-text-primary">Traces</span>
        </div>

        <div className="flex items-center gap-2">
          <IconButton
            icon={isFullscreen ? Minimize2 : Maximize2}
            onClick={toggleFullscreen}
            tooltip={isFullscreen ? 'Exit Fullscreen' : 'Fullscreen'}
            size="sm"
            variant="ghost"
          />
        </div>
      </header>

      {/* Content — shared trace surface (filters, list, waterfall) */}
      <main className="flex-1 min-h-0">
        <TracesView active servers={servers} />
      </main>

      {/* Footer */}
      <footer className="h-6 flex-shrink-0 bg-surface/90 backdrop-blur-xl border-t border-border/50 flex items-center justify-between px-4 text-[10px] text-text-muted">
        <span>Traces</span>
        <span className="flex items-center gap-1">
          <span className="w-1.5 h-1.5 rounded-full bg-text-muted animate-pulse" />
          Detached Window
        </span>
      </footer>
    </div>
  );
}

export function DetachedTracesPage() {
  return (
    <ErrorBoundary variant="window">
      <DetachedTracesPageContent />
    </ErrorBoundary>
  );
}
