import { describe, it, expect } from 'vitest';
import { LIBRARY_ACTIONS_COLLAPSE_BELOW, shouldCollapseLibraryActions } from '../lib/libraryHeader';

describe('shouldCollapseLibraryActions', () => {
  it('keeps the full layout when the width is unknown', () => {
    expect(shouldCollapseLibraryActions(0, false)).toBe(false);
    expect(shouldCollapseLibraryActions(0, true)).toBe(false);
    expect(shouldCollapseLibraryActions(-5, true)).toBe(false);
  });

  it('uses the wider threshold when a source has an update', () => {
    expect(LIBRARY_ACTIONS_COLLAPSE_BELOW.withUpdates).toBe(990);
    expect(shouldCollapseLibraryActions(989, true)).toBe(true);
    expect(shouldCollapseLibraryActions(990, true)).toBe(false);
  });

  it('uses the base threshold without updates', () => {
    expect(LIBRARY_ACTIONS_COLLAPSE_BELOW.base).toBe(850);
    expect(shouldCollapseLibraryActions(849, false)).toBe(true);
    expect(shouldCollapseLibraryActions(850, false)).toBe(false);
  });
});
