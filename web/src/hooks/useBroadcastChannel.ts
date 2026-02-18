import { useEffect, useRef, useCallback } from 'react';

// Message types for cross-window communication
export interface BroadcastMessage {
  type: 'STATE_UPDATE' | 'WINDOW_OPENED' | 'WINDOW_CLOSED' | 'REQUEST_STATE' | 'SELECTION_CHANGE';
  payload?: unknown;
  source: 'main' | 'detached';
  timestamp: number;
}

interface UseBroadcastChannelOptions {
  channelName?: string;
  onMessage?: (message: BroadcastMessage) => void;
}

const DEFAULT_CHANNEL = 'gridctl-sync';

export function useBroadcastChannel(options: UseBroadcastChannelOptions = {}) {
  const { channelName = DEFAULT_CHANNEL, onMessage } = options;
  const channelRef = useRef<BroadcastChannel | null>(null);

  useEffect(() => {
    // Create the broadcast channel
    channelRef.current = new BroadcastChannel(channelName);

    // Set up message listener
    const handleMessage = (event: MessageEvent<BroadcastMessage>) => {
      onMessage?.(event.data);
    };

    channelRef.current.addEventListener('message', handleMessage);

    return () => {
      channelRef.current?.removeEventListener('message', handleMessage);
      channelRef.current?.close();
      channelRef.current = null;
    };
  }, [channelName, onMessage]);

  const postMessage = useCallback((message: Omit<BroadcastMessage, 'timestamp'>) => {
    channelRef.current?.postMessage({
      ...message,
      timestamp: Date.now(),
    });
  }, []);

  return { postMessage };
}

// Hook for detached windows to sync with main window
export function useDetachedWindowSync(windowType: 'logs' | 'sidebar' | 'editor' | 'registry') {
  const { postMessage } = useBroadcastChannel({
    onMessage: (msg) => {
      // Handle messages from main window
      if (msg.source === 'main' && msg.type === 'STATE_UPDATE') {
        // State updates are handled by store subscriptions
      }
    },
  });

  // Notify main window when detached window opens
  useEffect(() => {
    postMessage({
      type: 'WINDOW_OPENED',
      payload: { windowType },
      source: 'detached',
    });

    // Notify on close
    const handleBeforeUnload = () => {
      postMessage({
        type: 'WINDOW_CLOSED',
        payload: { windowType },
        source: 'detached',
      });
    };

    window.addEventListener('beforeunload', handleBeforeUnload);

    return () => {
      window.removeEventListener('beforeunload', handleBeforeUnload);
      handleBeforeUnload();
    };
  }, [windowType, postMessage]);

  return { postMessage };
}
