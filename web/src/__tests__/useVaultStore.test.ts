import { describe, it, expect, beforeEach } from 'vitest';
import { useVaultStore } from '../stores/useVaultStore';

// Covers the session-scoped "recently edited" state that powers the per-set
// indicator dot: marking keys, merge semantics, and clear-on-lock.
describe('useVaultStore — recentlyEdited', () => {
  beforeEach(() => {
    useVaultStore.setState({
      variables: [],
      sets: [],
      usage: {},
      recentlyEdited: {},
      locked: false,
    });
  });

  it('markRecentlyEdited records each key with a timestamp', () => {
    const before = Date.now();
    useVaultStore.getState().markRecentlyEdited(['API_KEY', 'DB_URL']);
    const { recentlyEdited } = useVaultStore.getState();
    expect(Object.keys(recentlyEdited).sort()).toEqual(['API_KEY', 'DB_URL']);
    expect(recentlyEdited.API_KEY).toBeGreaterThanOrEqual(before);
    expect(recentlyEdited.DB_URL).toBeGreaterThanOrEqual(before);
  });

  it('merges new keys without dropping previously marked ones', () => {
    useVaultStore.getState().markRecentlyEdited(['A']);
    useVaultStore.getState().markRecentlyEdited(['B']);
    expect(Object.keys(useVaultStore.getState().recentlyEdited).sort()).toEqual([
      'A',
      'B',
    ]);
  });

  it('treats an empty key list as a no-op (preserves the reference)', () => {
    useVaultStore.getState().markRecentlyEdited(['A']);
    const ref = useVaultStore.getState().recentlyEdited;
    useVaultStore.getState().markRecentlyEdited([]);
    expect(useVaultStore.getState().recentlyEdited).toBe(ref);
  });

  it('clearRecentlyEdited empties the map', () => {
    useVaultStore.getState().markRecentlyEdited(['A']);
    useVaultStore.getState().clearRecentlyEdited();
    expect(useVaultStore.getState().recentlyEdited).toEqual({});
  });

  it('locking the vault clears recentlyEdited', () => {
    useVaultStore.getState().markRecentlyEdited(['A']);
    useVaultStore.getState().setLocked(true);
    expect(useVaultStore.getState().recentlyEdited).toEqual({});
  });

  it('unlocking the vault leaves recentlyEdited untouched', () => {
    useVaultStore.setState({ locked: true, recentlyEdited: {} });
    useVaultStore.getState().markRecentlyEdited(['A']);
    useVaultStore.getState().setLocked(false);
    expect(Object.keys(useVaultStore.getState().recentlyEdited)).toEqual(['A']);
  });
});
