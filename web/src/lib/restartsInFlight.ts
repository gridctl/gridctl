import { useSyncExternalStore } from 'react';

// Server restarts this tab has sent and not yet seen settle. Module state so a
// remounted Restart control (selection changed and came back) still knows its
// server is restarting. It does not guard other tabs or the CLI.
const inFlight = new Set<string>();
const listeners = new Set<() => void>();

function notify(): void {
  for (const listener of listeners) listener();
}

function subscribe(onStoreChange: () => void): () => void {
  listeners.add(onStoreChange);
  return () => listeners.delete(onStoreChange);
}

export function isRestartInFlight(name: string): boolean {
  return inFlight.has(name);
}

export function beginRestart(name: string): void {
  inFlight.add(name);
  notify();
}

export function endRestart(name: string): void {
  if (inFlight.delete(name)) notify();
}

export function useRestartInFlight(name: string): boolean {
  return useSyncExternalStore(subscribe, () => inFlight.has(name), () => false);
}

/** Clears every tracked restart. Exists for test isolation. */
export function resetRestartsInFlight(): void {
  inFlight.clear();
  notify();
}
