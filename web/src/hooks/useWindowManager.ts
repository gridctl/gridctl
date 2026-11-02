import { useCallback, useEffect } from 'react';
import { useUIStore } from '../stores/useUIStore';
import { showToast } from '../components/ui/Toast';
import {
  useBroadcastChannel,
  type BroadcastMessage,
  type DetachableWindow,
} from './useBroadcastChannel';

const WINDOW_TITLES: Record<string, string> = {
  logs: 'Gridctl - Logs',
  sidebar: 'Gridctl - Details',
  editor: 'Gridctl - Editor',
  registry: 'Gridctl - Library',
  metrics: 'Gridctl - Metrics',
  traces: 'Gridctl - Traces',
};

// Window types whose detached route differs from the default /<type>. Each of
// these had its /<type> path claimed by an in-shell workspace; the popout
// moves aside while the type key stays put, so no call-site, broadcast
// channel, or stored detached state has to change.
const WINDOW_PATHS: Record<string, string> = {
  registry: '/library-window',
  metrics: '/metrics-window',
  logs: '/logs-window',
  traces: '/traces-window',
};

const DETACHABLE: ReadonlySet<string> = new Set([
  'logs',
  'sidebar',
  'editor',
  'registry',
  'metrics',
  'traces',
]);

function isDetachable(value: string | undefined): value is DetachableWindow {
  return value != null && DETACHABLE.has(value);
}

function isDetached(type: DetachableWindow): boolean {
  const state = useUIStore.getState();
  switch (type) {
    case 'logs':
      return state.logsDetached;
    case 'sidebar':
      return state.sidebarDetached;
    case 'editor':
      return state.editorDetached;
    case 'registry':
      return state.registryDetached;
    case 'metrics':
      return state.metricsDetached;
    case 'traces':
      return state.tracesDetached;
  }
}

// Module-scope: detached windows live for the lifetime of the opener page,
// not the lifetime of any particular component instance. A per-component ref
// caused unmount cleanups to close windows that openDetachedWindow had only
// just opened (the same call flips a UI flag that can unmount the caller).
const windowRefs: Map<string, Window | null> = new Map();
const closePolls = new Map<string, ReturnType<typeof setInterval>>();

function clearClosePoll(type: string) {
  const id = closePolls.get(type);
  if (id == null) return;
  clearInterval(id);
  closePolls.delete(type);
}

function detachedUrl(type: string, params?: string): string {
  const basePath = WINDOW_PATHS[type] ?? `/${type}`;
  return params ? `${basePath}?${params}` : basePath;
}

function isBlankPopup(win: Window): boolean {
  try {
    return win.location.href === 'about:blank';
  } catch {
    return false;
  }
}

export function useWindowManager() {
  const setLogsDetached = useUIStore((s) => s.setLogsDetached);
  const setSidebarDetached = useUIStore((s) => s.setSidebarDetached);
  const setEditorDetached = useUIStore((s) => s.setEditorDetached);
  const setRegistryDetached = useUIStore((s) => s.setRegistryDetached);
  const setMetricsDetached = useUIStore((s) => s.setMetricsDetached);
  const setTracesDetached = useUIStore((s) => s.setTracesDetached);
  const setSidebarOpen = useUIStore((s) => s.setSidebarOpen);

  const setDetached = useCallback((type: DetachableWindow, detached: boolean) => {
    if (type === 'logs') setLogsDetached(detached);
    else if (type === 'sidebar') setSidebarDetached(detached);
    else if (type === 'editor') setEditorDetached(detached);
    else if (type === 'registry') setRegistryDetached(detached);
    else if (type === 'metrics') setMetricsDetached(detached);
    else setTracesDetached(detached);
  }, [setLogsDetached, setSidebarDetached, setEditorDetached, setRegistryDetached, setMetricsDetached, setTracesDetached]);

  // Opening a sidebar or library popout also closes the in-shell panel so the
  // main window updates in the same frame. Closing does not reopen it.
  const markOpened = useCallback((type: DetachableWindow) => {
    setDetached(type, true);
    if (type === 'sidebar' || type === 'registry') setSidebarOpen(false);
  }, [setDetached, setSidebarOpen]);

  const clearDetached = useCallback((type: DetachableWindow) => {
    clearClosePoll(type);
    windowRefs.delete(type);
    setDetached(type, false);
  }, [setDetached]);

  // Popup blocked: undo the eager flip, and restore a panel this call closed.
  const rollbackBlocked = useCallback((type: DetachableWindow) => {
    setDetached(type, false);
    if (type === 'sidebar' || type === 'registry') setSidebarOpen(true);
  }, [setDetached, setSidebarOpen]);

  const armClosePoll = useCallback((type: DetachableWindow, win: Window) => {
    clearClosePoll(type);
    const id = setInterval(() => {
      if (!win.closed) return;
      clearInterval(id);
      closePolls.delete(type);
      windowRefs.delete(type);
      setDetached(type, false);
    }, 500);
    closePolls.set(type, id);
  }, [setDetached]);

  // Handle messages from detached windows
  const handleMessage = useCallback((message: BroadcastMessage) => {
    if (message.source !== 'detached') return;
    const payload = message.payload as { windowType?: string } | undefined;
    const windowType = payload?.windowType;
    if (!isDetachable(windowType)) return;

    if (message.type === 'WINDOW_OPENED') {
      // Closing the in-shell panel is a false-to-true transition.
      // openDetachedWindow already closes it on a local open. A ping
      // reply from a popout that is already detached must not run that
      // side effect again, or mounting the Stack inspector (or any other
      // consumer) closes the inspector while the Library popout is open.
      if (isDetached(windowType)) setDetached(windowType, true);
      else markOpened(windowType);
    } else if (message.type === 'WINDOW_CLOSED') {
      clearDetached(windowType);
    }
  }, [markOpened, clearDetached, setDetached]);

  const { postMessage } = useBroadcastChannel({
    onMessage: handleMessage,
  });

  // One ping per channel instance. Open popouts reply with WINDOW_OPENED.
  // The handler restores a flag that is not yet set and does not re-close
  // the in-shell panel when that popout is already detached.
  useEffect(() => {
    postMessage({ type: 'WHO_IS_OPEN', source: 'main' });
  }, [postMessage]);

  // Open a detached window
  const openDetachedWindow = useCallback((type: DetachableWindow, params?: string) => {
    const existingWindow = windowRefs.get(type);
    if (existingWindow && !existingWindow.closed) {
      existingWindow.focus();
      return;
    }

    markOpened(type);

    const url = detachedUrl(type, params);
    const newWindow = window.open(url, `gridctl-${type}`);

    if (!newWindow) {
      rollbackBlocked(type);
      showToast('warning', 'Your browser blocked the popout window', {
        action: {
          label: 'Open',
          onClick: () => {
            window.open(url, '_blank');
          },
        },
      });
      return;
    }

    windowRefs.set(type, newWindow);

    // Update title after load
    newWindow.addEventListener('load', () => {
      newWindow.document.title = WINDOW_TITLES[type];
    });

    armClosePoll(type, newWindow);
  }, [markOpened, rollbackBlocked, armClosePoll]);

  // Focus an already-detached window. A missing named window is a blank popup;
  // close it and clear the flag so the panel returns inline.
  const focusDetachedWindow = useCallback((type: DetachableWindow) => {
    const existingWindow = windowRefs.get(type);
    if (existingWindow && !existingWindow.closed) {
      existingWindow.focus();
      return;
    }

    const named = window.open('', `gridctl-${type}`);
    if (!named || isBlankPopup(named)) {
      if (named) named.close();
      clearDetached(type);
      return;
    }

    windowRefs.set(type, named);
    named.focus();
    armClosePoll(type, named);
  }, [clearDetached, armClosePoll]);

  // Close a detached window
  const closeDetachedWindow = useCallback((type: DetachableWindow) => {
    const existingWindow = windowRefs.get(type);
    if (existingWindow && !existingWindow.closed) {
      existingWindow.close();
    }
    clearClosePoll(type);
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
    focusDetachedWindow,
    closeDetachedWindow,
    broadcastStateUpdate,
    broadcastSelectionChange,
  };
}
