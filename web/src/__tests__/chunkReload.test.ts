import { describe, it, expect, vi, afterEach } from 'vitest';
import {
  CHUNK_RELOAD_KEY,
  installChunkReload,
  isChunkLoadError,
} from '../lib/chunkReload';

function memoryStorage() {
  const data = new Map<string, string>();
  return {
    getItem: (k: string) => data.get(k) ?? null,
    setItem: (k: string, v: string) => {
      data.set(k, v);
    },
  };
}

function dispatchPreloadError(): Event {
  const event = new Event('vite:preloadError', { cancelable: true });
  window.dispatchEvent(event);
  return event;
}

describe('installChunkReload', () => {
  let uninstall: (() => void) | undefined;
  afterEach(() => {
    uninstall?.();
    uninstall = undefined;
  });

  it('reloads once per guard window and never prevents the event', () => {
    const reload = vi.fn();
    const storage = memoryStorage();
    let clock = 1_000_000;
    uninstall = installChunkReload({ reload, storage, now: () => clock });

    const first = dispatchPreloadError();
    expect(reload).toHaveBeenCalledTimes(1);
    expect(storage.getItem(CHUNK_RELOAD_KEY)).toBe(String(1_000_000));
    expect(first.defaultPrevented).toBe(false);

    clock += 29_999;
    const second = dispatchPreloadError();
    expect(reload).toHaveBeenCalledTimes(1);
    expect(second.defaultPrevented).toBe(false);

    // Past the 30 second default window a later upgrade reloads again.
    clock += 1;
    dispatchPreloadError();
    expect(reload).toHaveBeenCalledTimes(2);
  });

  it('honors a reload recorded by a previous page in the same tab', () => {
    const reload = vi.fn();
    const storage = memoryStorage();
    storage.setItem(CHUNK_RELOAD_KEY, String(Date.now()));
    uninstall = installChunkReload({ reload, storage });

    const event = dispatchPreloadError();
    expect(reload).not.toHaveBeenCalled();
    expect(event.defaultPrevented).toBe(false);
  });

  it('never reloads when storage is unavailable', () => {
    const reload = vi.fn();
    const storage = {
      getItem: () => {
        throw new Error('denied');
      },
      setItem: vi.fn(),
    };
    uninstall = installChunkReload({ reload, storage });

    const event = dispatchPreloadError();
    expect(reload).not.toHaveBeenCalled();
    expect(storage.setItem).not.toHaveBeenCalled();
    expect(event.defaultPrevented).toBe(false);
  });

  it('never reloads when the timestamp cannot be written', () => {
    const reload = vi.fn();
    const storage = {
      getItem: () => null,
      setItem: () => {
        throw new Error('quota');
      },
    };
    uninstall = installChunkReload({ reload, storage });

    dispatchPreloadError();
    expect(reload).not.toHaveBeenCalled();
  });

  it('stops listening once uninstalled', () => {
    const reload = vi.fn();
    installChunkReload({ reload, storage: memoryStorage() })();
    dispatchPreloadError();
    expect(reload).not.toHaveBeenCalled();
  });
});

describe('isChunkLoadError', () => {
  it('matches browser and Vite chunk-load failures', () => {
    expect(
      isChunkLoadError(new TypeError('Failed to fetch dynamically imported module: http://x/assets/A-1.js')),
    ).toBe(true);
    expect(isChunkLoadError(new TypeError('error loading dynamically imported module: http://x/a.js'))).toBe(true);
    expect(isChunkLoadError(new TypeError('Importing a module script failed.'))).toBe(true);
    expect(isChunkLoadError(new Error('Unable to preload CSS for /assets/a.css'))).toBe(true);
  });

  it('rejects other errors and non-errors', () => {
    expect(isChunkLoadError(new Error('boom'))).toBe(false);
    expect(isChunkLoadError(new TypeError("Cannot read properties of undefined (reading 'default')"))).toBe(false);
    expect(isChunkLoadError('Failed to fetch dynamically imported module')).toBe(false);
    expect(isChunkLoadError(null)).toBe(false);
  });
});
