import { useCallback, useEffect, useRef, useState } from 'react';

export interface UseRevealedValuesResult {
  revealed: Record<string, string>;
  // Fetches the value via `getValue`, stores it, and starts the auto-hide
  // timer when `autoHide` is true. When `autoHide` is true and the key is
  // already revealed, toggles it off instead.
  reveal: (
    key: string,
    getValue: () => Promise<string>,
    autoHide: boolean,
  ) => Promise<void>;
  hide: (key: string) => void;
  // Direct multi-set used by refresh() to hydrate plaintext values without
  // engaging the auto-hide timer (plaintext stays visible until unmount).
  bulkSet: (entries: Record<string, string>) => void;
}

const REVEAL_TIMEOUT_MS = 10_000;

// Local UI state for "which variable values are currently visible." Per-row
// timers live here, not in Zustand — they're presentational and would cause
// unnecessary re-renders if shared globally. Both VaultPanel and the detached
// page consume this; each component instance gets its own reveal map.
export function useRevealedValues(): UseRevealedValuesResult {
  const [revealed, setRevealed] = useState<Record<string, string>>({});
  const timers = useRef<Record<string, ReturnType<typeof setTimeout>>>({});
  const revealedRef = useRef(revealed);

  // Mirror the latest revealed map into a ref so callbacks can read it
  // without re-binding on every change; useEffect (not render-time
  // assignment) keeps it lint-clean per react-hooks/refs-during-render.
  useEffect(() => {
    revealedRef.current = revealed;
  }, [revealed]);

  useEffect(() => {
    return () => {
      Object.values(timers.current).forEach(clearTimeout);
      timers.current = {};
    };
  }, []);

  const clearTimer = useCallback((key: string) => {
    if (timers.current[key]) {
      clearTimeout(timers.current[key]);
      delete timers.current[key];
    }
  }, []);

  const hide = useCallback(
    (key: string) => {
      setRevealed((prev) => {
        if (prev[key] === undefined) return prev;
        const next = { ...prev };
        delete next[key];
        return next;
      });
      clearTimer(key);
    },
    [clearTimer],
  );

  const reveal = useCallback(
    async (
      key: string,
      getValue: () => Promise<string>,
      autoHide: boolean,
    ) => {
      // Toggle off when already revealed for autoHide types (secrets).
      // Plaintext rows are sticky-revealed; toggling them off serves no
      // UX purpose, so callers pass autoHide=false there.
      if (revealedRef.current[key] !== undefined && autoHide) {
        hide(key);
        return;
      }
      const value = await getValue();
      setRevealed((prev) => ({ ...prev, [key]: value }));
      if (autoHide) {
        timers.current[key] = setTimeout(() => {
          setRevealed((prev) => {
            const next = { ...prev };
            delete next[key];
            return next;
          });
          delete timers.current[key];
        }, REVEAL_TIMEOUT_MS);
      }
    },
    [hide],
  );

  const bulkSet = useCallback((entries: Record<string, string>) => {
    if (!entries || Object.keys(entries).length === 0) return;
    setRevealed((prev) => ({ ...prev, ...entries }));
  }, []);

  return { revealed, reveal, hide, bulkSet };
}
