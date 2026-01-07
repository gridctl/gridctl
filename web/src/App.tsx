import { useCallback, useState } from 'react';
import { ReactFlowProvider, useReactFlow } from '@xyflow/react';
import { Header } from './components/layout/Header';
import { Sidebar } from './components/layout/Sidebar';
import { StatusBar } from './components/layout/StatusBar';
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
  });

  return (
    <div className="h-screen w-screen flex flex-col overflow-hidden bg-background">
      <Header onRefresh={handleRefresh} isRefreshing={isRefreshing} />

      <div className="flex-1 flex relative overflow-hidden">
        {/* Loading State */}
        {isLoading && (
          <div className="absolute inset-0 flex items-center justify-center bg-background/80 z-30">
            <div className="text-center space-y-4">
              <div className="w-12 h-12 border-4 border-primary border-t-transparent rounded-full animate-spin mx-auto" />
              <p className="text-text-muted">Loading topology...</p>
            </div>
          </div>
        )}

        {/* Error State */}
        {error && !isLoading && (
          <div className="absolute inset-0 flex items-center justify-center bg-background/80 z-30">
            <div className="text-center space-y-4 max-w-md p-6">
              <div className="w-16 h-16 bg-status-error/20 rounded-full flex items-center justify-center mx-auto">
                <span className="text-2xl">!</span>
              </div>
              <h2 className="text-lg font-semibold text-text-primary">Connection Error</h2>
              <p className="text-sm text-text-muted">{error}</p>
              <button
                onClick={handleRefresh}
                className="px-4 py-2 bg-primary text-white rounded-lg hover:bg-primaryLight transition-colors"
              >
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
