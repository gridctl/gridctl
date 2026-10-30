import { useEffect, useRef, useState } from 'react';
import { usePageVisible } from './usePageVisible';

export interface UseIntervalOptions {
  // Default true. False stops the timer and does not fire.
  enabled?: boolean;
  // Default null: pause while hidden. A number runs that slow cadence while hidden.
  hiddenIntervalMs?: number | null;
  // Default true: fire once on mount and when enabled goes true. Never on a
  // visibility change; that path uses the lastRunAt rule instead.
  immediate?: boolean;
}

function isPromise(value: void | Promise<void>): value is Promise<void> {
  return typeof (value as Promise<void> | undefined)?.then === 'function';
}

// One scheduler for network polling. Visibility does not restart the timer and
// never takes the immediate path. A visibility change reschedules against the
// interval that is active now and fires only when the last run is missing or
// already older than that interval.
export function useInterval(
  callback: () => void | Promise<void>,
  intervalMs: number,
  options?: UseIntervalOptions,
): { lastRunAt: number | null } {
  const enabled = options?.enabled ?? true;
  const hiddenIntervalMs = options?.hiddenIntervalMs === undefined ? null : options.hiddenIntervalMs;
  const immediate = options?.immediate ?? true;

  const callbackRef = useRef(callback);
  const lastRunAtRef = useRef<number | null>(null);
  const pendingRef = useRef(false);
  const timerRef = useRef<number | null>(null);
  const rescheduleRef = useRef<(() => void) | null>(null);
  const visible = usePageVisible();
  const visibleRef = useRef(visible);
  const wasEnabledRef = useRef(false);
  const startedRef = useRef(false);
  // The return value is part of the hook contract. State keeps it current
  // for a caller; scheduling reads the ref. The microtask avoids a
  // synchronous setState in the effect below. No caller reads it today, and
  // the extra render is the cost of that contract.
  const [lastRunAt, setLastRunAt] = useState<number | null>(null);

  useEffect(() => {
    callbackRef.current = callback;
  }, [callback]);

  useEffect(() => {
    const first = !startedRef.current;
    startedRef.current = true;
    const becameEnabled = enabled && !wasEnabledRef.current;
    wasEnabledRef.current = enabled;

    if (!enabled) {
      rescheduleRef.current = null;
      return;
    }

    let cancelled = false;
    const fireNow = immediate && (first || becameEnabled);

    const activeInterval = (): number | null => {
      if (visibleRef.current) return intervalMs;
      return hiddenIntervalMs;
    };

    const clearTimer = () => {
      if (timerRef.current != null) {
        window.clearTimeout(timerRef.current);
        timerRef.current = null;
      }
    };

    const schedule = (delay: number) => {
      clearTimer();
      timerRef.current = window.setTimeout(() => {
        timerRef.current = null;
        void tick();
      }, Math.max(0, delay));
    };

    const recordRun = () => {
      const now = Date.now();
      lastRunAtRef.current = now;
      queueMicrotask(() => {
        if (!cancelled) setLastRunAt(now);
      });
    };

    // A throw or a rejected promise must not kill the chain. Callers that
    // care catch their own errors; this scheduler still records the attempt
    // and arms the next tick.
    const invoke = async (): Promise<boolean> => {
      if (pendingRef.current) return false;
      let result: void | Promise<void>;
      try {
        result = callbackRef.current();
      } catch {
        recordRun();
        return true;
      }
      recordRun();
      if (isPromise(result)) {
        pendingRef.current = true;
        try {
          await result;
        } catch {
          // Rejection is recorded above. The timer continues.
        } finally {
          pendingRef.current = false;
        }
      }
      return true;
    };

    const armNext = () => {
      if (cancelled) return;
      const next = activeInterval();
      if (next == null) return;
      schedule(next);
    };

    const tick = async () => {
      if (cancelled) return;
      const interval = activeInterval();
      if (interval == null) return;
      if (pendingRef.current) {
        schedule(interval);
        return;
      }
      try {
        await invoke();
      } finally {
        armNext();
      }
    };

    const reschedule = () => {
      if (cancelled) return;
      const interval = activeInterval();
      clearTimer();
      if (interval == null) return;
      const last = lastRunAtRef.current;
      const now = Date.now();
      if (last == null || now - last >= interval) {
        void tick();
      } else {
        schedule(last + interval - now);
      }
    };

    rescheduleRef.current = reschedule;

    // Start the callback in this effect so a same-tick disable still observes
    // the in-flight request. State writes stay after the callback's first await.
    if (fireNow) {
      void invoke().finally(armNext);
    } else {
      const interval = activeInterval();
      if (interval != null) schedule(interval);
    }

    return () => {
      cancelled = true;
      rescheduleRef.current = null;
      clearTimer();
    };
  }, [enabled, intervalMs, hiddenIntervalMs, immediate]);

  // Visibility is not a timer dependency. Update the ref, then apply the
  // lastRunAt rule against whichever interval is active now.
  useEffect(() => {
    const previous = visibleRef.current;
    visibleRef.current = visible;
    if (previous === visible) return;
    rescheduleRef.current?.();
  }, [visible]);

  return { lastRunAt };
}
