import { useEffect, useLayoutEffect, useRef, useCallback } from 'react';
import type { GatewayStatus } from '../types';

// Visible-cadence status from the main window. A hidden heartbeat does not send this.
export interface StatusSnapshotPayload {
  status: GatewayStatus;
  at: number;
}

export type DetachableWindow = 'logs' | 'sidebar' | 'editor' | 'registry' | 'metrics' | 'traces';

// Message types for cross-window communication
export interface BroadcastMessage {
  type:
    | 'STATE_UPDATE'
    | 'WINDOW_OPENED'
    | 'WINDOW_CLOSED'
    | 'REQUEST_STATE'
    | 'SELECTION_CHANGE'
    | 'THEME_CHANGE'
    | 'STATUS_SNAPSHOT'
    | 'WHO_IS_OPEN';
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
  // The listener identity must not recreate the channel. A ping that lands
  // while the channel is closing is dropped, so the effect depends only on
  // the channel name and reads the latest handler from this ref.
  const onMessageRef = useRef(onMessage);
  useLayoutEffect(() => {
    onMessageRef.current = onMessage;
  });

  useEffect(() => {
    const channel = new BroadcastChannel(channelName);
    channelRef.current = channel;

    const handleMessage = (event: MessageEvent<BroadcastMessage>) => {
      onMessageRef.current?.(event.data);
    };

    channel.addEventListener('message', handleMessage);

    return () => {
      channel.removeEventListener('message', handleMessage);
      channel.close();
      if (channelRef.current === channel) channelRef.current = null;
    };
  }, [channelName]);

  const postMessage = useCallback((message: Omit<BroadcastMessage, 'timestamp'>) => {
    channelRef.current?.postMessage({
      ...message,
      timestamp: Date.now(),
    });
  }, []);

  return { postMessage };
}

// Hook for detached windows to sync with main window
export function useDetachedWindowSync(windowType: DetachableWindow) {
  // postMessage is produced by the hook that consumes this callback, so the
  // reply goes through a ref updated after the hook returns. The callback
  // itself stays stable for a given windowType, which is what keeps the
  // channel from tearing down between the ping and the reply.
  const postMessageRef = useRef<(message: Omit<BroadcastMessage, 'timestamp'>) => void>(() => {});

  const onMessage = useCallback((msg: BroadcastMessage) => {
    if (msg.source === 'main' && msg.type === 'WHO_IS_OPEN') {
      postMessageRef.current({
        type: 'WINDOW_OPENED',
        payload: { windowType },
        source: 'detached',
      });
    }
  }, [windowType]);

  const { postMessage } = useBroadcastChannel({ onMessage });

  useLayoutEffect(() => {
    postMessageRef.current = postMessage;
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
