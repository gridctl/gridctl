import { describe, expect, it, beforeEach, vi } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import { migratePreferences, prefKey, readPref, subscribePref, usePref, writePref } from '../lib/preferences';

const LEGACY = [
  ['gridctl-log-font-size', 'log-font-size', '11'],
  ['gridctl-traces-font-size', 'traces-font-size', '12'],
  ['gridctl-library-zoom', 'library-zoom', '14'],
  ['gridctl-tools-zoom', 'tools-zoom', '13'],
  ['gridctl-palette-frecency', 'palette-frecency', '{"a":1}'],
  ['gridctl:last-workspace', 'last-workspace', 'library'],
  ['gridctl-skill-local-edit-note-seen', 'skill-local-edit-note-seen', '1'],
] as const;

describe('migratePreferences', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('copies legacy keys, including a per-stack workspace, and leaves credential storage alone', () => {
    for (const [legacy, , value] of LEGACY) {
      localStorage.setItem(legacy, value);
    }
    localStorage.setItem('gridctl:last-workspace:stack-a', 'tools');
    localStorage.setItem('gridctl-ui-storage', '{"edgeStyle":"default"}');
    localStorage.setItem('gridctl-auth-credential', 'secret');
    localStorage.setItem('gridctl-auth-token', 'legacy-token');

    migratePreferences();

    for (const [legacy, name, value] of LEGACY) {
      expect(localStorage.getItem(prefKey(name))).toBe(value);
      expect(localStorage.getItem(legacy)).toBeNull();
    }
    expect(localStorage.getItem(prefKey('last-workspace:stack-a'))).toBe('tools');
    expect(localStorage.getItem('gridctl:last-workspace:stack-a')).toBeNull();
    expect(localStorage.getItem('gridctl-ui-storage')).toBe('{"edgeStyle":"default"}');
    expect(localStorage.getItem('gridctl-auth-credential')).toBe('secret');
    expect(localStorage.getItem('gridctl-auth-token')).toBe('legacy-token');

    const after = { ...localStorage };
    migratePreferences();
    expect({ ...localStorage }).toEqual(after);
  });

  it('does not overwrite a preference that is already present', () => {
    localStorage.setItem('gridctl-tools-zoom', '18');
    localStorage.setItem(prefKey('tools-zoom'), '12');
    migratePreferences();
    expect(localStorage.getItem(prefKey('tools-zoom'))).toBe('12');
    expect(localStorage.getItem('gridctl-tools-zoom')).toBe('18');
  });

  it('returns the session value when storage reads throw after a rejected write', () => {
    const setItem = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('storage blocked');
    });
    writePref('zoom-fallback', '16');
    setItem.mockRestore();
    const getItem = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('storage blocked');
    });
    try {
      expect(readPref('zoom-fallback')).toBe('16');
    } finally {
      getItem.mockRestore();
      writePref('zoom-fallback', '16');
      localStorage.removeItem(prefKey('zoom-fallback'));
    }
  });
});

describe('cross-window preference updates', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  function storageEvent(key: string | null, newValue: string | null = null) {
    window.dispatchEvent(new StorageEvent('storage', { key, newValue, storageArea: localStorage }));
  }

  it('re-renders a mounted usePref consumer when another window writes its key', () => {
    const { result } = renderHook(() => usePref('library-zoom'));
    expect(result.current).toBeNull();
    localStorage.setItem(prefKey('library-zoom'), '16');
    act(() => storageEvent(prefKey('library-zoom'), '16'));
    expect(result.current).toBe('16');
  });

  it('notifies only the matching preference and ignores unrelated keys', () => {
    const zoom = vi.fn();
    const other = vi.fn();
    const offZoom = subscribePref('library-zoom', zoom);
    const offOther = subscribePref('tools-zoom', other);
    try {
      storageEvent(prefKey('library-zoom'), '15');
      expect(zoom).toHaveBeenCalledTimes(1);
      expect(other).not.toHaveBeenCalled();

      storageEvent('gridctl-ui-storage', '{}');
      storageEvent('library-zoom', '15');
      expect(zoom).toHaveBeenCalledTimes(1);
    } finally {
      offZoom();
      offOther();
    }
  });

  it('notifies every preference when storage is cleared', () => {
    const zoom = vi.fn();
    const other = vi.fn();
    const offZoom = subscribePref('library-zoom', zoom);
    const offOther = subscribePref('tools-zoom', other);
    try {
      storageEvent(null);
      expect(zoom).toHaveBeenCalledTimes(1);
      expect(other).toHaveBeenCalledTimes(1);
    } finally {
      offZoom();
      offOther();
    }
  });

  it('ignores sessionStorage events', () => {
    const zoom = vi.fn();
    const off = subscribePref('library-zoom', zoom);
    try {
      window.dispatchEvent(new StorageEvent('storage', { key: prefKey('library-zoom'), storageArea: sessionStorage }));
      expect(zoom).not.toHaveBeenCalled();
    } finally {
      off();
    }
  });

  it('removes the window listener after the last unsubscribe', () => {
    const add = vi.spyOn(window, 'addEventListener');
    const remove = vi.spyOn(window, 'removeEventListener');
    const zoom = vi.fn();
    const off = subscribePref('library-zoom', zoom);
    const offSecond = subscribePref('library-zoom', vi.fn());
    try {
      expect(add.mock.calls.filter(([type]) => type === 'storage')).toHaveLength(1);
      off();
      expect(remove.mock.calls.filter(([type]) => type === 'storage')).toHaveLength(0);
      offSecond();
      expect(remove.mock.calls.filter(([type]) => type === 'storage')).toHaveLength(1);
      storageEvent(prefKey('library-zoom'), '15');
      expect(zoom).not.toHaveBeenCalled();
    } finally {
      add.mockRestore();
      remove.mockRestore();
    }
  });
});
