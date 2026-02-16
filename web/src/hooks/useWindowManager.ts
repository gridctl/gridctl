import { useCallback, useEffect, useRef } from 'react';
import { useUIStore } from '../stores/useUIStore';
import { useBroadcastChannel, type BroadcastMessage } from './useBroadcastChannel';

interface WindowConfig {
  width: number;
  height: number;
  title: string;
}

const WINDOW_CONFIGS: Record<string, WindowConfig> = {
  logs: {
    width: 900,
    height: 500,
    title: 'Gridctl - Logs',
  },
  sidebar: {
    width: 420,
    height: 700,
    title: 'Gridctl - Details',
  },
  editor: {
    width: 720,
    height: 750,
    title: 'Gridctl - Editor',
  },
};

export function useWindowManager() {
  const windowRefs = useRef<Map<string, Window | null>>(new Map());

  const setLogsDetached = useUIStore((s) => s.setLogsDetached);
  const setSidebarDetached = useUIStore((s) => s.setSidebarDetached);
  const setEditorDetached = useUIStore((s) => s.setEditorDetached);
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
        }
      } else if (message.type === 'WINDOW_CLOSED') {
        if (payload?.windowType === 'logs') {
          setLogsDetached(false);
        } else if (payload?.windowType === 'sidebar') {
          setSidebarDetached(false);
        } else if (payload?.windowType === 'editor') {
          setEditorDetached(false);
        }
        windowRefs.current.delete(payload?.windowType ?? '');
      }
    }
  }, [setLogsDetached, setSidebarDetached, setEditorDetached, setBottomPanelOpen, setSidebarOpen]);

  const { postMessage } = useBroadcastChannel({
    onMessage: handleMessage,
  });

  // Open a detached window
  const openDetachedWindow = useCallback((type: 'logs' | 'sidebar' | 'editor', params?: string) => {
    const existingWindow = windowRefs.current.get(type);
    if (existingWindow && !existingWindow.closed) {
      existingWindow.focus();
      return;
    }

    const config = WINDOW_CONFIGS[type];
    const left = window.screenX + (window.outerWidth - config.width) / 2;
    const top = window.screenY + (window.outerHeight - config.height) / 2;

    const url = params ? `/${type}?${params}` : `/${type}`;
    const features = [
      `width=${config.width}`,
      `height=${config.height}`,
      `left=${left}`,
      `top=${top}`,
      'menubar=no',
      'toolbar=no',
      'location=no',
      'status=no',
      'resizable=yes',
    ].join(',');

    const newWindow = window.open(url, `gridctl-${type}`, features);

    if (newWindow) {
      windowRefs.current.set(type, newWindow);

      // Update title after load
      newWindow.addEventListener('load', () => {
        newWindow.document.title = config.title;
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
          }
        }
      }, 500);
    }
  }, [setLogsDetached, setSidebarDetached, setEditorDetached]);

  // Close a detached window
  const closeDetachedWindow = useCallback((type: 'logs' | 'sidebar' | 'editor') => {
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
