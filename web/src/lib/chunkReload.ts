// Stale-bundle recovery. A tab loaded before the daemon was upgraded or
// rebuilt requests code-split chunks by their old hashed names, which the
// server now answers with 404. Vite's preload wrapper dispatches
// `vite:preloadError` on window before rethrowing; reloading once fetches the
// new index.html and its current chunk names.
//
// The event is never default-prevented. Preventing it makes Vite resolve the
// import with undefined, and a reload cancelled by a beforeunload prompt
// would then leave React.lazy throwing an unclassifiable TypeError. Letting
// the original error through means the error boundary renders "Reload
// needed" while the reload proceeds.

export const CHUNK_RELOAD_KEY = 'gridctl-chunk-reload-at';
export const CHUNK_RELOAD_WINDOW_MS = 30_000;

export interface ChunkReloadOptions {
  reload?: () => void;
  storage?: Pick<Storage, 'getItem' | 'setItem'>;
  windowMs?: number;
  now?: () => number;
}

// installChunkReload registers the listener and returns a function that
// removes it. Call it once at module scope, not from a component.
export function installChunkReload(options: ChunkReloadOptions = {}): () => void {
  const reload = options.reload ?? (() => window.location.reload());
  const windowMs = options.windowMs ?? CHUNK_RELOAD_WINDOW_MS;
  const now = options.now ?? Date.now;

  const onPreloadError = () => {
    let storage: Pick<Storage, 'getItem' | 'setItem'>;
    try {
      storage = options.storage ?? window.sessionStorage;
      const last = Number(storage.getItem(CHUNK_RELOAD_KEY));
      const at = now();
      // A reload inside the guard window did not help; stop to avoid a loop.
      if (Number.isFinite(last) && last > 0 && at - last < windowMs) return;
      storage.setItem(CHUNK_RELOAD_KEY, String(at));
    } catch {
      // Without storage the guard cannot hold, so never reload.
      return;
    }
    reload();
  };

  window.addEventListener('vite:preloadError', onPreloadError);
  return () => window.removeEventListener('vite:preloadError', onPreloadError);
}

const CHUNK_ERROR_PATTERNS = [
  'dynamically imported module', // Chrome and Firefox
  'Importing a module script failed', // Safari
  'Unable to preload CSS', // Vite
];

// isChunkLoadError reports whether err is a failed code-split chunk load.
export function isChunkLoadError(err: unknown): boolean {
  if (!(err instanceof Error)) return false;
  return CHUNK_ERROR_PATTERNS.some((p) => err.message.includes(p));
}
