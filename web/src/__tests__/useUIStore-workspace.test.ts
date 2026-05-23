import { describe, it, expect, beforeEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useUIStore, COMPACT_MODE_DEFAULTS } from '../stores/useUIStore';
import { WORKSPACES } from '../types/workspace';

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
    for (const ws of WORKSPACES) {
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

  it('defaults compactMode to all-off for every workspace', () => {
    const state = useUIStore.getState();
    for (const ws of WORKSPACES) {
      expect(state.compactMode[ws]).toBe(false);
    }
  });

  it('setCompactMode updates a single workspace without touching the others', () => {
    act(() => {
      useUIStore.getState().setCompactMode('topology', true);
    });
    const state = useUIStore.getState();
    expect(state.compactMode.topology).toBe(true);
    for (const ws of WORKSPACES) {
      if (ws === 'topology') continue;
      expect(state.compactMode[ws]).toBe(false);
    }
  });

  it('toggleCompactMode flips only the targeted workspace', () => {
    act(() => {
      useUIStore.getState().toggleCompactMode('library');
    });
    expect(useUIStore.getState().compactMode.library).toBe(true);
    act(() => {
      useUIStore.getState().toggleCompactMode('library');
    });
    const state = useUIStore.getState();
    expect(state.compactMode.library).toBe(false);
    for (const ws of WORKSPACES) {
      if (ws === 'library') continue;
      expect(state.compactMode[ws]).toBe(false);
    }
  });
});
