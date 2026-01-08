import { useCallback, useState } from 'react';
import { ReactFlowProvider, useReactFlow } from '@xyflow/react';
import { AlertCircle, RefreshCw } from 'lucide-react';
import { Header } from './components/layout/Header';
import { Sidebar } from './components/layout/Sidebar';
import { StatusBar } from './components/layout/StatusBar';
import { BottomPanel } from './components/layout/BottomPanel';
import { Canvas } from './components/graph/Canvas';
import { useTopologyStore } from './stores/useTopologyStore';
import { useUIStore } from './stores/useUIStore';
import { usePolling } from './hooks/usePolling';
import { useKeyboardShortcuts } from './hooks/useKeyboardShortcuts';

function AppContent() {
  const [isRefreshing, setIsRefreshing] = useState(false);
  const isLoading = useTopologyStore((s) => s.isLoading);
  const error = useTopologyStore((s) => s.error);
  const selectNode = useTopologyStore((s) => s.selectNode);
  const setSidebarOpen = useUIStore((s) => s.setSidebarOpen);

  const { fitView, zoomIn, zoomOut } = useReactFlow();
  const { refresh } = usePolling();

  const handleRefresh = useCallback(async () => {
    setIsRefreshing(true);
    await refresh();
    setTimeout(() => setIsRefreshing(false), 500);
  }, [refresh]);

  const toggleBottomPanel = useUIStore((s) => s.toggleBottomPanel);

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

  return (
    <div className="h-screen w-screen flex flex-col overflow-hidden bg-background relative">
      {/* Subtle background grain is applied via CSS */}
      <Header onRefresh={handleRefresh} isRefreshing={isRefreshing} />

      {/* Main content area */}
      <div className="flex-1 flex relative overflow-hidden min-h-0">
        {/* Loading State */}
        {isLoading && (
          <div className="absolute inset-0 flex items-center justify-center bg-background/90 backdrop-blur-sm z-30">
            <div className="text-center space-y-5 animate-fade-in-scale">
              {/* Animated loader */}
              <div className="relative mx-auto w-16 h-16">
                <div className="absolute inset-0 rounded-full border-2 border-primary/20" />
                <div className="absolute inset-0 rounded-full border-2 border-primary border-t-transparent animate-spin" />
                <div className="absolute inset-2 rounded-full border-2 border-secondary/30 border-b-transparent animate-spin" style={{ animationDirection: 'reverse', animationDuration: '1.5s' }} />
              </div>
              <div>
                <p className="text-text-secondary font-medium">Loading topology</p>
                <p className="text-text-muted text-sm mt-1">Connecting to gateway...</p>
              </div>
            </div>
          </div>
        )}

        {/* Error State */}
        {error && !isLoading && (
          <div className="absolute inset-0 flex items-center justify-center bg-background/90 backdrop-blur-sm z-30">
            <div className="text-center space-y-5 max-w-md p-8 animate-fade-in-scale">
              {/* Error icon with glow */}
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

        {/* Canvas */}
        <Canvas />

        {/* Sidebar */}
        <Sidebar />
      </div>

      {/* Bottom Panel for Logs */}
      <BottomPanel />

      <StatusBar />
    </div>
  );
}

function App() {
  return (
    <ReactFlowProvider>
      <AppContent />
    </ReactFlowProvider>
  );
}

export default App;
