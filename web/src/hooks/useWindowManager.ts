import { useCallback, useEffect, useRef } from 'react';
import { useUIStore } from '../stores/useUIStore';
import { useBroadcastChannel, type BroadcastMessage } from './useBroadcastChannel';

const WINDOW_TITLES: Record<string, string> = {
  logs: 'Gridctl - Logs',
  sidebar: 'Gridctl - Details',
  editor: 'Gridctl - Editor',
  registry: 'Gridctl - Registry',
  workflow: 'Gridctl - Workflow Designer',
  metrics: 'Gridctl - Metrics',
};

export function useWindowManager() {
  const windowRefs = useRef<Map<string, Window | null>>(new Map());

  const setLogsDetached = useUIStore((s) => s.setLogsDetached);
  const setSidebarDetached = useUIStore((s) => s.setSidebarDetached);
  const setEditorDetached = useUIStore((s) => s.setEditorDetached);
  const setRegistryDetached = useUIStore((s) => s.setRegistryDetached);
  const setWorkflowDetached = useUIStore((s) => s.setWorkflowDetached);
  const setMetricsDetached = useUIStore((s) => s.setMetricsDetached);
  const setBottomPanelOpen = useUIStore((s) => s.setBottomPanelOpen);
  const setSidebarOpen = useUIStore((s) => s.setSidebarOpen);

  // Handle messages from detached windows
  const handleMessage = useCallback((message: BroadcastMessage) => {
    if (message.source === 'detached') {
      const payload = message.payload as { windowType: string } | undefined;

      if (message.type === 'WINDOW_OPENED') {
        if (payload?.windowType === 'logs') {
          setLogsDetached(true);
          setBottomPanelOpen(false);
        } else if (payload?.windowType === 'sidebar') {
          setSidebarDetached(true);
          setSidebarOpen(false);
        } else if (payload?.windowType === 'editor') {
          setEditorDetached(true);
        } else if (payload?.windowType === 'registry') {
          setRegistryDetached(true);
          setSidebarOpen(false);
        } else if (payload?.windowType === 'workflow') {
          setWorkflowDetached(true);
        } else if (payload?.windowType === 'metrics') {
          setMetricsDetached(true);
        }
      } else if (message.type === 'WINDOW_CLOSED') {
        if (payload?.windowType === 'logs') {
          setLogsDetached(false);
        } else if (payload?.windowType === 'sidebar') {
          setSidebarDetached(false);
        } else if (payload?.windowType === 'editor') {
          setEditorDetached(false);
        } else if (payload?.windowType === 'registry') {
          setRegistryDetached(false);
        } else if (payload?.windowType === 'workflow') {
          setWorkflowDetached(false);
        } else if (payload?.windowType === 'metrics') {
          setMetricsDetached(false);
        }
        windowRefs.current.delete(payload?.windowType ?? '');
      }
    }
  }, [setLogsDetached, setSidebarDetached, setEditorDetached, setRegistryDetached, setWorkflowDetached, setMetricsDetached, setBottomPanelOpen, setSidebarOpen]);

  const { postMessage } = useBroadcastChannel({
    onMessage: handleMessage,
  });

  // Open a detached window
  const openDetachedWindow = useCallback((type: 'logs' | 'sidebar' | 'editor' | 'registry' | 'workflow' | 'metrics', params?: string) => {
    const existingWindow = windowRefs.current.get(type);
    if (existingWindow && !existingWindow.closed) {
      existingWindow.focus();
      return;
    }

    const url = params ? `/${type}?${params}` : `/${type}`;
    const newWindow = window.open(url, `gridctl-${type}`);

    if (newWindow) {
      windowRefs.current.set(type, newWindow);

      // Update title after load
      newWindow.addEventListener('load', () => {
        newWindow.document.title = WINDOW_TITLES[type];
      });

      // Track window close
      const checkClosed = setInterval(() => {
        if (newWindow.closed) {
          clearInterval(checkClosed);
          windowRefs.current.delete(type);
          if (type === 'logs') {
            setLogsDetached(false);
          } else if (type === 'sidebar') {
            setSidebarDetached(false);
          } else if (type === 'editor') {
            setEditorDetached(false);
          } else if (type === 'registry') {
            setRegistryDetached(false);
          } else if (type === 'workflow') {
            setWorkflowDetached(false);
          } else if (type === 'metrics') {
            setMetricsDetached(false);
          }
        }
      }, 500);
    }
  }, [setLogsDetached, setSidebarDetached, setEditorDetached, setRegistryDetached, setWorkflowDetached, setMetricsDetached]);

  // Close a detached window
  const closeDetachedWindow = useCallback((type: 'logs' | 'sidebar' | 'editor' | 'registry' | 'workflow' | 'metrics') => {
    const existingWindow = windowRefs.current.get(type);
    if (existingWindow && !existingWindow.closed) {
      existingWindow.close();
    }
    windowRefs.current.delete(type);
  }, []);

  // Notify detached windows of state changes
  const broadcastStateUpdate = useCallback((state: unknown) => {
    postMessage({
      type: 'STATE_UPDATE',
      payload: state,
      source: 'main',
    });
  }, [postMessage]);

  // Broadcast selection changes
  const broadcastSelectionChange = useCallback((nodeId: string | null) => {
    postMessage({
      type: 'SELECTION_CHANGE',
      payload: { nodeId },
      source: 'main',
    });
  }, [postMessage]);

  // Clean up on unmount
  useEffect(() => {
    const refs = windowRefs.current;
    return () => {
      refs.forEach((win) => {
        if (win && !win.closed) {
          win.close();
        }
      });
      refs.clear();
    };
  }, []);

  return {
    openDetachedWindow,
    closeDetachedWindow,
    broadcastStateUpdate,
    broadcastSelectionChange,
  };
}
