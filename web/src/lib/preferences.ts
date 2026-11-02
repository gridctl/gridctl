import { useCallback, useSyncExternalStore } from 'react';

export const PREF_PREFIX = 'gridctl-pref-';

const listeners = new Map<string, Set<() => void>>();

// Session copy of a preference whose storage write was rejected (blocked
// site data, private mode, quota). Reads prefer it over a stale or missing
// storage value so the control still moves. A later successful write drops
// the copy and storage is the source of truth again.
const sessionPrefs = new Map<string, string>();

export function prefKey(name: string): string {
  return `${PREF_PREFIX}${name}`;
}

export function readPref(name: string): string | null {
  const key = prefKey(name);
  if (sessionPrefs.has(key)) return sessionPrefs.get(key) ?? null;
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

export function writePref(name: string, value: string): void {
  const key = prefKey(name);
  try {
    localStorage.setItem(key, value);
    sessionPrefs.delete(key);
  } catch {
    sessionPrefs.set(key, value);
  }
  notifyPref(name);
}

export function removePref(name: string): void {
  const key = prefKey(name);
  sessionPrefs.delete(key);
  try {
    localStorage.removeItem(key);
  } catch {
    // Storage rejected the delete. The session copy is already gone.
  }
  notifyPref(name);
}

export function subscribePref(name: string, listener: () => void): () => void {
  let set = listeners.get(name);
  if (!set) {
    set = new Set();
    listeners.set(name, set);
  }
  set.add(listener);
  return () => {
    set.delete(listener);
    if (set.size === 0) listeners.delete(name);
  };
}

function notifyPref(name: string) {
  const set = listeners.get(name);
  if (!set) return;
  for (const listener of set) listener();
}

// Snapshot is the stored string (or null). useSyncExternalStore compares it
// with Object.is, so a string from localStorage is stable across reads.
export function usePref(name: string): string | null {
  const subscribe = useCallback(
    (listener: () => void) => subscribePref(name, listener),
    [name],
  );
  const getSnapshot = useCallback(() => readPref(name), [name]);
  return useSyncExternalStore(subscribe, getSnapshot, () => null);
}

const LEGACY_KEYS: ReadonlyArray<readonly [string, string]> = [
  ['gridctl-log-font-size', 'log-font-size'],
  ['gridctl-traces-font-size', 'traces-font-size'],
  ['gridctl-library-zoom', 'library-zoom'],
  ['gridctl-tools-zoom', 'tools-zoom'],
  ['gridctl-palette-frecency', 'palette-frecency'],
  ['gridctl:last-workspace', 'last-workspace'],
  ['gridctl-skill-local-edit-note-seen', 'skill-local-edit-note-seen'],
];

const LEGACY_STACK_PREFIX = 'gridctl:last-workspace:';

function copyLegacy(legacyKey: string, name: string): void {
  const nextKey = prefKey(name);
  if (localStorage.getItem(nextKey) != null) return;
  const value = localStorage.getItem(legacyKey);
  if (value == null) return;
  localStorage.setItem(nextKey, value);
  localStorage.removeItem(legacyKey);
}

// Idempotent. Copies a legacy key only when the new key is absent, then
// removes the legacy key. A failure leaves the legacy key in place.
export function migratePreferences(): void {
  try {
    for (const [legacyKey, name] of LEGACY_KEYS) {
      copyLegacy(legacyKey, name);
    }
    const stackKeys: string[] = [];
    for (let i = 0; i < localStorage.length; i++) {
      const key = localStorage.key(i);
      if (key && key.startsWith(LEGACY_STACK_PREFIX)) stackKeys.push(key);
    }
    for (const key of stackKeys) {
      copyLegacy(key, `last-workspace:${key.slice(LEGACY_STACK_PREFIX.length)}`);
    }
  } catch {
    // Private mode or a mid-copy failure. Leave remaining legacy keys alone.
  }
}
