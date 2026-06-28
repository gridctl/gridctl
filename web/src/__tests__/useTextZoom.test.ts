import { describe, it, expect, beforeEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useTextZoom } from '../hooks/useTextZoom';

const KEY = 'gridctl-test-zoom';

beforeEach(() => {
  localStorage.clear();
});

describe('useTextZoom', () => {
  it('starts at the default size when nothing is stored', () => {
    const { result } = renderHook(() => useTextZoom({ storageKey: KEY, defaultSize: 13 }));
    expect(result.current.fontSize).toBe(13);
    expect(result.current.isDefault).toBe(true);
  });

  it('restores a valid persisted size', () => {
    localStorage.setItem(KEY, '17');
    const { result } = renderHook(() => useTextZoom({ storageKey: KEY, defaultSize: 13 }));
    expect(result.current.fontSize).toBe(17);
    expect(result.current.isDefault).toBe(false);
  });

  it('ignores an out-of-range persisted size and falls back to the default', () => {
    localStorage.setItem(KEY, '999');
    const { result } = renderHook(() =>
      useTextZoom({ storageKey: KEY, defaultSize: 13, maxSize: 22 }),
    );
    expect(result.current.fontSize).toBe(13);
  });

  it('zooms in and out by the step and persists the new size', () => {
    const { result } = renderHook(() =>
      useTextZoom({ storageKey: KEY, defaultSize: 13, step: 2 }),
    );
    act(() => result.current.zoomIn());
    expect(result.current.fontSize).toBe(15);
    expect(localStorage.getItem(KEY)).toBe('15');

    act(() => result.current.zoomOut());
    expect(result.current.fontSize).toBe(13);
    expect(localStorage.getItem(KEY)).toBe('13');
  });

  it('clamps at the min and max bounds and flips isMin/isMax', () => {
    const { result } = renderHook(() =>
      useTextZoom({ storageKey: KEY, defaultSize: 13, minSize: 12, maxSize: 14, step: 1 }),
    );
    act(() => result.current.zoomIn());
    expect(result.current.fontSize).toBe(14);
    expect(result.current.isMax).toBe(true);
    act(() => result.current.zoomIn());
    expect(result.current.fontSize).toBe(14); // clamped, no overflow

    act(() => result.current.zoomOut());
    act(() => result.current.zoomOut());
    expect(result.current.fontSize).toBe(12);
    expect(result.current.isMin).toBe(true);
    act(() => result.current.zoomOut());
    expect(result.current.fontSize).toBe(12); // clamped
  });

  it('resets back to the default size', () => {
    const { result } = renderHook(() => useTextZoom({ storageKey: KEY, defaultSize: 13 }));
    act(() => result.current.zoomIn());
    expect(result.current.isDefault).toBe(false);
    act(() => result.current.resetZoom());
    expect(result.current.fontSize).toBe(13);
    expect(result.current.isDefault).toBe(true);
    expect(localStorage.getItem(KEY)).toBe('13');
  });

  it('exposes the current size as the --text-zoom-size custom property', () => {
    const { result } = renderHook(() => useTextZoom({ storageKey: KEY, defaultSize: 13 }));
    const style = result.current.containerProps.style as Record<string, string>;
    expect(style['--text-zoom-size']).toBe('13px');
    act(() => result.current.zoomIn());
    const next = result.current.containerProps.style as Record<string, string>;
    expect(next['--text-zoom-size']).toBe('14px');
  });
});
