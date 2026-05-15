import { useEffect } from 'react';
import { subscribeToGlobalRunEvents } from '../../lib/agent-api';
import { useRunsStore } from '../../stores/useRunsStore';
import { useUIStore } from '../../stores/useUIStore';

/**
 * useGlobalRunsStream mounts an EventSource against
 * GET /api/agent/runs/events/stream and routes every frame through
 * useRunsStore. The hook is owned by the AppShell-level mount point so
 * the stream stays connected when the user navigates between
 * workspaces; tearing it down on /runs unmount would lose in-flight
 * events while the user is on /topology or /skills.
 *
 * The user can pause the stream via `useUIStore.runsStreamEnabled` — the
 * BottomPanel and StatusBar both expose toggles. When paused, the
 * EventSource is closed and `streamStatus` is set to `'paused'`. The
 * in-flight badge is intentionally NOT cleared, so users see the last
 * known value rather than a misleading zero.
 *
 * SSE replay protection: the store dedupes events by `(run_id, seq)`
 * via its `lastSeenSeq` watermark — see useRunsStore.applyRunEvent.
 */
export function useGlobalRunsStream(): void {
  const enabled = useUIStore((s) => s.runsStreamEnabled);
  const applyRunEvent = useRunsStore((s) => s.applyRunEvent);
  const handleStreamRestart = useRunsStore((s) => s.handleStreamRestart);
  const setStreamStatus = useRunsStore((s) => s.setStreamStatus);

  useEffect(() => {
    if (!enabled) {
      setStreamStatus('paused');
      return;
    }
    setStreamStatus('connecting');
    const sub = subscribeToGlobalRunEvents({
      onEvent: applyRunEvent,
      onReady: () => setStreamStatus('open'),
      onRestart: handleStreamRestart,
      onError: () => setStreamStatus('error'),
    });
    return () => {
      sub.close();
      setStreamStatus('idle');
    };
  }, [enabled, applyRunEvent, handleStreamRestart, setStreamStatus]);
}
