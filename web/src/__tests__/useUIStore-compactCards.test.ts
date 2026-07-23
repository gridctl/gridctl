import { describe, it, expect, beforeEach, vi } from 'vitest';

// Fresh module (and thus fresh persist rehydration) per test so seeded
// localStorage payloads are actually read.
async function loadStore() {
  vi.resetModules();
  const { useUIStore } = await import('../stores/useUIStore');
  return useUIStore;
}

describe('useUIStore compactCards', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('defaults to compact cards', async () => {
    const store = await loadStore();
    expect(store.getState().compactCards).toBe(true);
  });

  it('drops a stale v0 persisted value so the new default applies', async () => {
    // v0 wrote compactCards for every user regardless of an explicit choice;
    // migrating must not pin existing installs to the old expanded default.
    localStorage.setItem(
      'gridctl-ui-storage',
      JSON.stringify({ state: { compactCards: false }, version: 0 }),
    );
    const store = await loadStore();
    expect(store.getState().compactCards).toBe(true);
  });

  it('honors a v1 persisted choice', async () => {
    localStorage.setItem(
      'gridctl-ui-storage',
      JSON.stringify({ state: { compactCards: false }, version: 1 }),
    );
    const store = await loadStore();
    expect(store.getState().compactCards).toBe(false);
  });
});
