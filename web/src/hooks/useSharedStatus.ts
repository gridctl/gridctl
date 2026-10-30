import { useCallback, useEffect, useRef, useState } from 'react';
import { fetchStatus } from '../lib/api';
import { POLLING } from '../lib/constants';
import type { GatewayStatus } from '../types';
import { useAuthStore } from '../stores/useAuthStore';
import { useBroadcastChannel, type BroadcastMessage } from './useBroadcastChannel';
import { useInterval } from './useInterval';

// Longer than two visible status cycles so a slow main-window poll does not
// flap the fallback. A hidden main window does not broadcast, so this window
// is also how long a popout waits before polling for itself.
export const STATUS_STALE_MS = 2 * POLLING.STATUS + 1000;

function snapshotStatus(payload: unknown): GatewayStatus | null {
  if (!payload || typeof payload !== 'object') return null;
  const status = (payload as { status?: unknown }).status;
  if (!status || typeof status !== 'object') return null;
  return status as GatewayStatus;
}

export interface SharedStatus {
  status: GatewayStatus | null;
  source: 'broadcast' | 'self' | null;
  // True after a snapshot arrives or the first self-poll attempt finishes.
  // A popout can leave its loading state when status never arrives.
  settled: boolean;
}

// Broadcast is the primary source. Self-poll starts when snapshots go stale
// and stops only after two snapshots arrive within STATUS_STALE_MS. One
// isolated snapshot (a hidden main window must not send one, but a stray
// message must not cancel the fallback) leaves the self-poll running.
export function useSharedStatus({ enabled }: { enabled: boolean }): SharedStatus {
  const [status, setStatus] = useState<GatewayStatus | null>(null);
  const [source, setSource] = useState<'broadcast' | 'self' | null>(null);
  const [settled, setSettled] = useState(false);
  const [selfPoll, setSelfPoll] = useState(false);
  const selfPollRef = useRef(false);
  const lastSnapshotAt = useRef<number | null>(null);
  const pairCandidateAt = useRef<number | null>(null);
  const enabledRef = useRef(enabled);
  const [liveEpoch, setLiveEpoch] = useState(0);
  const generation = useAuthStore((s) => s.generation);
  const authRequired = useAuthStore((s) => s.authRequired);

  useEffect(() => {
    enabledRef.current = enabled;
  }, [enabled]);

  const onMessage = useCallback((msg: BroadcastMessage) => {
    if (!enabledRef.current) return;
    if (msg.type !== 'STATUS_SNAPSHOT' || msg.source !== 'main') return;
    const next = snapshotStatus(msg.payload);
    if (!next) return;
    const now = Date.now();
    lastSnapshotAt.current = now;
    setStatus(next);
    setSource('broadcast');
    setSettled(true);
    if (!selfPollRef.current) {
      setLiveEpoch((n) => n + 1);
      return;
    }
    const candidate = pairCandidateAt.current;
    if (candidate != null && now - candidate <= STATUS_STALE_MS) {
      pairCandidateAt.current = null;
      selfPollRef.current = false;
      setSelfPoll(false);
      setLiveEpoch((n) => n + 1);
      return;
    }
    pairCandidateAt.current = now;
  }, []);

  const { postMessage } = useBroadcastChannel({ onMessage });

  useEffect(() => {
    if (!enabled) return;
    postMessage({ type: 'REQUEST_STATE', source: 'detached' });
  }, [enabled, postMessage]);

  useEffect(() => {
    if (!enabled || selfPoll) return;
    const timer = window.setTimeout(() => {
      selfPollRef.current = true;
      pairCandidateAt.current = null;
      setSelfPoll(true);
    }, STATUS_STALE_MS);
    return () => window.clearTimeout(timer);
  }, [enabled, selfPoll, liveEpoch]);

  const selfFetch = useCallback(async () => {
    const current = () =>
      useAuthStore.getState().generation === generation && !useAuthStore.getState().authRequired;
    if (!current()) return;
    try {
      const next = await fetchStatus();
      if (!current()) return;
      setStatus(next);
      setSource('self');
    } catch {
      // The next tick retries. A status miss must not blank the popout.
    } finally {
      setSettled(true);
    }
  }, [generation]);

  useInterval(selfFetch, POLLING.STATUS, {
    enabled: enabled && selfPoll && !authRequired,
  });

  return { status, source, settled };
}
