import { useCallback } from 'react';
import { useUIStore } from '../stores/useUIStore';
import { useBroadcastChannel, type BroadcastMessage } from './useBroadcastChannel';

const WINDOW_TITLES: Record<string, string> = {
  logs: 'Gridctl - Logs',
  sidebar: 'Gridctl - Details',
  editor: 'Gridctl - Editor',
  registry: 'Gridctl - Library',
  metrics: 'Gridctl - Metrics',
  var: 'Gridctl - Variables',
  traces: 'Gridctl - Traces',
};

// The `registry` window type points at /library-window after the workspace
// promotion; other types render at /<type>. Keeping the type key as
// `registry` avoids churning every call-site and stored detached state.
const WINDOW_PATHS: Record<string, string> = {
  registry: '/library-window',
};

type DetachableWindow = 'logs' | 'sidebar' | 'editor' | 'registry' | 'metrics' | 'var' | 'traces';

// Module-scope: detached windows live for the lifetime of the opener page,
// not the lifetime of any particular component instance. A per-component ref
// caused unmount cleanups to close windows that openDetachedWindow had only
// just opened (the same call flips a UI flag that can unmount the caller).
const windowRefs: Map<string, Window | null> = new Map();

export function useWindowManager() {
  const setLogsDetached = useUIStore((s) => s.setLogsDetached);
  const setSidebarDetached = useUIStore((s) => s.setSidebarDetached);
  const setEditorDetached = useUIStore((s) => s.setEditorDetached);
  const setRegistryDetached = useUIStore((s) => s.setRegistryDetached);
  const setMetricsDetached = useUIStore((s) => s.setMetricsDetached);
  const setVaultDetached = useUIStore((s) => s.setVaultDetached);
  const setTracesDetached = useUIStore((s) => s.setTracesDetached);
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
        } else if (payload?.windowType === 'metrics') {
          setMetricsDetached(true);
        } else if (payload?.windowType === 'var') {
          setVaultDetached(true);
        } else if (payload?.windowType === 'traces') {
          setTracesDetached(true);
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
        } else if (payload?.windowType === 'metrics') {
          setMetricsDetached(false);
        } else if (payload?.windowType === 'var') {
          setVaultDetached(false);
        } else if (payload?.windowType === 'traces') {
          setTracesDetached(false);
        }
        windowRefs.delete(payload?.windowType ?? '');
      }
    }
  }, [setLogsDetached, setSidebarDetached, setEditorDetached, setRegistryDetached, setMetricsDetached, setVaultDetached, setTracesDetached, setBottomPanelOpen, setSidebarOpen]);

  const { postMessage } = useBroadcastChannel({
    onMessage: handleMessage,
  });

  // Open a detached window
  const openDetachedWindow = useCallback((type: DetachableWindow, params?: string) => {
    const existingWindow = windowRefs.get(type);
    if (existingWindow && !existingWindow.closed) {
      existingWindow.focus();
      return;
    }

    // Immediately mark as detached and close associated panels
    // so the main window updates in the same frame (no transition flicker)
    if (type === 'logs') {
      setLogsDetached(true);
      setBottomPanelOpen(false);
    } else if (type === 'sidebar') {
      setSidebarDetached(true);
      setSidebarOpen(false);
    } else if (type === 'editor') {
      setEditorDetached(true);
    } else if (type === 'registry') {
      setRegistryDetached(true);
      setSidebarOpen(false);
    } else if (type === 'metrics') {
      setMetricsDetached(true);
    } else if (type === 'var') {
      setVaultDetached(true);
    } else if (type === 'traces') {
      setTracesDetached(true);
    }

    const basePath = WINDOW_PATHS[type] ?? `/${type}`;
    const url = params ? `${basePath}?${params}` : basePath;
    const newWindow = window.open(url, `gridctl-${type}`);

    if (!newWindow) {
      // Popup blocked or otherwise refused. Roll back the eager state flip so
      // the user still sees the source panel.
      if (type === 'logs') {
        setLogsDetached(false);
        setBottomPanelOpen(true);
      } else if (type === 'sidebar') {
        setSidebarDetached(false);
        setSidebarOpen(true);
      } else if (type === 'editor') {
        setEditorDetached(false);
      } else if (type === 'registry') {
        setRegistryDetached(false);
        setSidebarOpen(true);
      } else if (type === 'metrics') {
        setMetricsDetached(false);
      } else if (type === 'var') {
        setVaultDetached(false);
      } else if (type === 'traces') {
        setTracesDetached(false);
      }
      return;
    }

    windowRefs.set(type, newWindow);

    // Update title after load
    newWindow.addEventListener('load', () => {
      newWindow.document.title = WINDOW_TITLES[type];
    });

    // Track window close
    const checkClosed = setInterval(() => {
      if (newWindow.closed) {
        clearInterval(checkClosed);
        windowRefs.delete(type);
        if (type === 'logs') {
          setLogsDetached(false);
        } else if (type === 'sidebar') {
          setSidebarDetached(false);
        } else if (type === 'editor') {
          setEditorDetached(false);
        } else if (type === 'registry') {
          setRegistryDetached(false);
        } else if (type === 'metrics') {
          setMetricsDetached(false);
        } else if (type === 'var') {
          setVaultDetached(false);
        } else if (type === 'traces') {
          setTracesDetached(false);
        }
      }
    }, 500);
  }, [setLogsDetached, setSidebarDetached, setEditorDetached, setRegistryDetached, setMetricsDetached, setVaultDetached, setTracesDetached, setBottomPanelOpen, setSidebarOpen]);

  // Close a detached window
  const closeDetachedWindow = useCallback((type: DetachableWindow) => {
    const existingWindow = windowRefs.get(type);
    if (existingWindow && !existingWindow.closed) {
      existingWindow.close();
    }
    windowRefs.delete(type);
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

  return {
    openDetachedWindow,
    closeDetachedWindow,
    broadcastStateUpdate,
    broadcastSelectionChange,
  };
}
