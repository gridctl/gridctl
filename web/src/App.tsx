import { useCallback, useState, Component, type ReactNode } from 'react';
import { ReactFlowProvider, useReactFlow } from '@xyflow/react';
import { AlertCircle, RefreshCw } from 'lucide-react';

// Error boundary to catch React crashes
interface ErrorBoundaryState {
  hasError: boolean;
  error: Error | null;
}

class ErrorBoundary extends Component<{ children: ReactNode }, ErrorBoundaryState> {
  constructor(props: { children: ReactNode }) {
    super(props);
    this.state = { hasError: false, error: null };
  }

  static getDerivedStateFromError(error: Error) {
    return { hasError: true, error };
  }

  render() {
    if (this.state.hasError) {
      return (
        <div className="h-screen w-screen bg-background flex items-center justify-center">
          <div className="text-center p-8">
            <h1 className="text-xl text-status-error mb-4">Something went wrong</h1>
            <pre className="text-xs text-text-muted bg-surface p-4 rounded max-w-lg overflow-auto">
              {this.state.error?.message}
            </pre>
            <button
              onClick={() => window.location.reload()}
              className="mt-4 px-4 py-2 bg-primary text-background rounded"
            >
              Reload
            </button>
          </div>
        </div>
      );
    }
    return this.props.children;
  }
}
import { Header } from './components/layout/Header';
import { Sidebar } from './components/layout/Sidebar';
import { StatusBar } from './components/layout/StatusBar';
import { BottomPanel } from './components/layout/BottomPanel';
import { AuthPrompt } from './components/auth/AuthPrompt';
import { Canvas } from './components/graph/Canvas';
import { ResizeHandle } from './components/ui/ResizeHandle';
import { useStackStore } from './stores/useStackStore';
import { useUIStore } from './stores/useUIStore';
import { useAuthStore } from './stores/useAuthStore';
import { usePolling } from './hooks/usePolling';
import { useKeyboardShortcuts } from './hooks/useKeyboardShortcuts';
import { cn } from './lib/cn';

// Constants for panel sizing
const HEADER_HEIGHT = 56;
const STATUSBAR_HEIGHT = 32;

// Bottom panel constraints
const BOTTOM_PANEL_COLLAPSED = 40;
const BOTTOM_PANEL_DEFAULT = 250;
const BOTTOM_PANEL_MIN = 100;
const BOTTOM_PANEL_MAX = 800;

// Sidebar constraints
const SIDEBAR_DEFAULT = 320;
const SIDEBAR_MIN = 280;
const SIDEBAR_MAX = 600;

function AppContent() {
  const [isRefreshing, setIsRefreshing] = useState(false);
  const [bottomPanelHeight, setBottomPanelHeight] = useState(BOTTOM_PANEL_DEFAULT);
  const [sidebarWidth, setSidebarWidth] = useState(SIDEBAR_DEFAULT);

  const isLoading = useStackStore((s) => s.isLoading);
  const error = useStackStore((s) => s.error);
  const selectNode = useStackStore((s) => s.selectNode);
  const setSidebarOpen = useUIStore((s) => s.setSidebarOpen);
  const toggleBottomPanel = useUIStore((s) => s.toggleBottomPanel);
  const bottomPanelOpen = useUIStore((s) => s.bottomPanelOpen);
  const sidebarOpen = useUIStore((s) => s.sidebarOpen);
  const authRequired = useAuthStore((s) => s.authRequired);

  const { fitView, zoomIn, zoomOut } = useReactFlow();
  const { refresh } = usePolling();

  const handleRefresh = useCallback(async () => {
    setIsRefreshing(true);
    await refresh();
    setTimeout(() => setIsRefreshing(false), 500);
  }, [refresh]);

  // Handle bottom panel resize
  const handleBottomPanelResize = useCallback((delta: number) => {
    setBottomPanelHeight((prev) => {
      const newHeight = prev + delta;
      return Math.min(BOTTOM_PANEL_MAX, Math.max(BOTTOM_PANEL_MIN, newHeight));
    });
  }, []);

  // Handle sidebar resize
  const handleSidebarResize = useCallback((delta: number) => {
    setSidebarWidth((prev) => {
      const newWidth = prev + delta;
      return Math.min(SIDEBAR_MAX, Math.max(SIDEBAR_MIN, newWidth));
    });
  }, []);

  // Keyboard shortcuts
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
  });

  // Calculate grid row height for bottom panel
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
      {/* Auth overlay - above everything */}
      {authRequired && <AuthPrompt />}

      {/* Row 1: Header */}
      <Header onRefresh={handleRefresh} isRefreshing={isRefreshing} />

      {/* Row 2: Main Content Area (Canvas + Sidebar overlay) */}
      <main className="relative overflow-hidden" style={{ minHeight: 100 }}>
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
                  <AlertCircle size={32} className="text-status-error" />
                </div>
              </div>
              <div>
                <h2 className="text-lg font-semibold text-text-primary">Connection Error</h2>
                <p className="text-sm text-text-muted mt-2 leading-relaxed">{error}</p>
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

        {/* Canvas fills the entire main area via absolute positioning */}
        <Canvas />

        {/* Sidebar - absolute overlay within main content */}
        <aside
          className={cn(
            'absolute top-0 right-0 bottom-0 z-20',
            'bg-surface/80 backdrop-blur-xl border-l border-border/50',
            'transform transition-transform duration-300 ease-out',
            'flex flex-row overflow-hidden',
            sidebarOpen ? 'translate-x-0' : 'translate-x-full'
          )}
          style={{ width: sidebarWidth }}
        >
          {/* Resize handle on the left edge */}
          <ResizeHandle
            direction="vertical"
            onResize={handleSidebarResize}
            className="flex-shrink-0"
          />

          {/* Sidebar content */}
          <div className="flex-1 min-w-0 overflow-hidden">
            <Sidebar />
          </div>
        </aside>
      </main>

      {/* Row 3: Resize Handle + Bottom Panel */}
      <div className="flex flex-col h-full overflow-hidden">
        {/* Resize handle - only interactive when panel is open */}
        {bottomPanelOpen && (
          <ResizeHandle
            direction="horizontal"
            onResize={handleBottomPanelResize}
            className="flex-shrink-0"
          />
        )}

        {/* Bottom Panel content */}
        <div className="flex-1 min-h-0">
          <BottomPanel />
        </div>
      </div>

      {/* Row 4: Status Bar */}
      <StatusBar />
    </div>
  );
}

function App() {
  return (
    <ErrorBoundary>
      <ReactFlowProvider>
        <AppContent />
      </ReactFlowProvider>
    </ErrorBoundary>
  );
}

export default App;
