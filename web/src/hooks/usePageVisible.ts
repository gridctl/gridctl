import { useSyncExternalStore } from 'react';

function subscribe(onStoreChange: () => void): () => void {
  if (typeof document === 'undefined') return () => {};
  document.addEventListener('visibilitychange', onStoreChange);
  return () => document.removeEventListener('visibilitychange', onStoreChange);
}

// True only for a visible document. `prerender` and `hidden` are not visible.
// A missing document (SSR) is treated as visible so the first client render
// does not pause polling before the browser reports a state.
function getSnapshot(): boolean {
  if (typeof document === 'undefined') return true;
  return document.visibilityState === 'visible';
}

function getServerSnapshot(): boolean {
  return true;
}

export function usePageVisible(): boolean {
  return useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);
}
