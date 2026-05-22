import { describe, it, expect, beforeEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useUIStore, COMPACT_MODE_DEFAULTS } from '../stores/useUIStore';

describe('useUIStore workspace slice', () => {
  beforeEach(() => {
    useUIStore.setState({ activeWorkspace: 'topology' });
  });

  it('defaults activeWorkspace to topology', () => {
    const { result } = renderHook(() => useUIStore((s) => s.activeWorkspace));
    expect(result.current).toBe('topology');
  });

  it('setActiveWorkspace updates state', () => {
    const { result } = renderHook(() => useUIStore((s) => s.activeWorkspace));
    act(() => {
      useUIStore.getState().setActiveWorkspace('library');
    });
    expect(result.current).toBe('library');
  });

  it('setActiveWorkspace cycles through every workspace', () => {
    const { result } = renderHook(() => useUIStore((s) => s.activeWorkspace));
    for (const ws of ['topology', 'library'] as const) {
      act(() => {
        useUIStore.getState().setActiveWorkspace(ws);
      });
      expect(result.current).toBe(ws);
    }
  });
});

describe('useUIStore compact mode slice', () => {
  beforeEach(() => {
    useUIStore.setState({ compactMode: { ...COMPACT_MODE_DEFAULTS } });
  });

  it('defaults compactMode to all-off', () => {
    const state = useUIStore.getState();
    expect(state.compactMode.topology).toBe(false);
    expect(state.compactMode.library).toBe(false);
  });

  it('setCompactMode updates a single workspace without touching the others', () => {
    act(() => {
      useUIStore.getState().setCompactMode('topology', true);
    });
    const state = useUIStore.getState();
    expect(state.compactMode.topology).toBe(true);
    expect(state.compactMode.library).toBe(false);
  });

  it('toggleCompactMode flips only the targeted workspace', () => {
    act(() => {
      useUIStore.getState().toggleCompactMode('library');
    });
    expect(useUIStore.getState().compactMode.library).toBe(true);
    act(() => {
      useUIStore.getState().toggleCompactMode('library');
    });
    expect(useUIStore.getState().compactMode.library).toBe(false);
    expect(useUIStore.getState().compactMode.topology).toBe(false);
  });
});
