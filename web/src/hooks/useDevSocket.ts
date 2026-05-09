import { useEffect, useState } from 'react';
import {
  subscribeToWatcher,
  type WatcherEvent,
} from '../lib/agent-api';

/**
 * useDevSocket subscribes to /api/agent/dev/events and returns the
 * most-recent change as state. The IDE keys cache invalidation off
 * the returned `lastEvent` ref equality — re-fetch the active skill
 * whenever it changes.
 *
 * Returns connectionState so the IDE can render a "watcher offline"
 * indicator when the daemon hasn't wired a project root.
 */
export type WatcherConnectionState = 'connecting' | 'open' | 'error';

interface UseDevSocketResult {
  lastEvent: WatcherEvent | null;
  connectionState: WatcherConnectionState;
}

export function useDevSocket(enabled: boolean = true): UseDevSocketResult {
  const [lastEvent, setLastEvent] = useState<WatcherEvent | null>(null);
  const [connectionState, setConnectionState] =
    useState<WatcherConnectionState>(enabled ? 'connecting' : 'error');

  useEffect(() => {
    if (!enabled) {
      setConnectionState('error');
      return;
    }
    setConnectionState('connecting');
    const sub = subscribeToWatcher(
      (ev) => {
        setConnectionState('open');
        setLastEvent(ev);
      },
      () => setConnectionState('error'),
    );
    return () => sub.close();
  }, [enabled]);

  return { lastEvent, connectionState };
}
