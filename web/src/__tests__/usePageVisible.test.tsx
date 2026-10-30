import { afterEach, describe, expect, it } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import { usePageVisible } from '../hooks/usePageVisible';

function setVisibility(state: DocumentVisibilityState) {
  Object.defineProperty(document, 'visibilityState', {
    configurable: true,
    get: () => state,
  });
  document.dispatchEvent(new Event('visibilitychange'));
}

describe('usePageVisible', () => {
  afterEach(() => {
    setVisibility('visible');
  });

  it('is true while the document is visible', () => {
    setVisibility('visible');
    const { result } = renderHook(() => usePageVisible());
    expect(result.current).toBe(true);
  });

  it('follows visibilitychange and is false when hidden', () => {
    const { result } = renderHook(() => usePageVisible());
    act(() => setVisibility('hidden'));
    expect(result.current).toBe(false);
    act(() => setVisibility('visible'));
    expect(result.current).toBe(true);
  });

  it('is false for a non-visible state', () => {
    const { result } = renderHook(() => usePageVisible());
    act(() => setVisibility('hidden'));
    expect(result.current).toBe(false);
  });
});
