import { describe, it, expect, beforeEach, vi } from 'vitest';

// Fresh module (and thus fresh persist rehydration) per test so seeded
// localStorage payloads are actually read.
async function loadStore() {
  vi.resetModules();
  return import('../stores/useUIStore');
}

describe('useUIStore stackPrefs', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('defaults to no view choice', async () => {
    const { useUIStore } = await loadStore();
    expect(useUIStore.getState().stackPrefs).toEqual({ view: null });
  });

  it('loads a payload without stackPrefs cleanly', async () => {
    localStorage.setItem(
      'gridctl-ui-storage',
      JSON.stringify({ state: { compactCards: false }, version: 1 }),
    );
    const { useUIStore } = await loadStore();
    expect(useUIStore.getState().stackPrefs).toEqual({ view: null });
  });

  it('coerces a bad persisted view to null', async () => {
    localStorage.setItem(
      'gridctl-ui-storage',
      JSON.stringify({ state: { stackPrefs: { view: 'grid' } }, version: 1 }),
    );
    const { useUIStore } = await loadStore();
    expect(useUIStore.getState().stackPrefs).toEqual({ view: null });
  });

  it('honors and persists a chosen view', async () => {
    localStorage.setItem(
      'gridctl-ui-storage',
      JSON.stringify({ state: { stackPrefs: { view: 'table' } }, version: 1 }),
    );
    const { useUIStore } = await loadStore();
    expect(useUIStore.getState().stackPrefs.view).toBe('table');
    useUIStore.getState().setStackPrefs({ view: 'graph' });
    expect(useUIStore.getState().stackPrefs.view).toBe('graph');
    const stored = JSON.parse(localStorage.getItem('gridctl-ui-storage') ?? '{}');
    expect(stored.state.stackPrefs).toEqual({ view: 'graph' });
  });

  it('normalizes anything else to null', async () => {
    const { normalizeStackPrefs } = await loadStore();
    expect(normalizeStackPrefs(undefined)).toEqual({ view: null });
    expect(normalizeStackPrefs({ view: 3 })).toEqual({ view: null });
    expect(normalizeStackPrefs({ view: 'graph' })).toEqual({ view: 'graph' });
  });
});
