import { useCallback, useEffect, useState } from 'react';

export type LoadedEvidence =
  | { status: 'idle' }
  | { status: 'loading' }
  | { status: 'ready'; text: string }
  | { status: 'error'; message: string };

type Settled =
  | { key: string; attempt: number; ok: true; text: string }
  | { key: string; attempt: number; ok: false; message: string };

/**
 * Fetches the evidence a trust action acts on and reports it as idle (no
 * key, so nothing to fetch), loading, ready, or error. Loading is derived
 * from a key or attempt mismatch rather than set, so a key change or a
 * retry never shows the previous result, and an error is never stored as
 * evidence text. `fetcher` must be stable for a given key (useCallback).
 */
export function useLoadedEvidence(
  key: string | null,
  fetcher: () => Promise<string>,
): LoadedEvidence & { retry: () => void } {
  const [attempt, setAttempt] = useState(0);
  const [settled, setSettled] = useState<Settled | null>(null);

  useEffect(() => {
    if (key === null) return;
    let cancelled = false;
    fetcher()
      .then((text) => {
        if (!cancelled) setSettled({ key, attempt, ok: true, text });
      })
      .catch((err) => {
        if (!cancelled) {
          setSettled({
            key,
            attempt,
            ok: false,
            message: err instanceof Error && err.message ? err.message : 'Diff unavailable',
          });
        }
      });
    return () => {
      cancelled = true;
    };
  }, [key, attempt, fetcher]);

  const retry = useCallback(() => setAttempt((n) => n + 1), []);

  if (key === null) return { status: 'idle', retry };
  if (!settled || settled.key !== key || settled.attempt !== attempt) return { status: 'loading', retry };
  return settled.ok
    ? { status: 'ready', text: settled.text, retry }
    : { status: 'error', message: settled.message, retry };
}
