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
      useUIStore.getState().setActiveWorkspace('skills');
    });
    expect(result.current).toBe('skills');
  });

  it('setActiveWorkspace cycles through every workspace', () => {
    const { result } = renderHook(() => useUIStore((s) => s.activeWorkspace));
    for (const ws of ['topology', 'skills', 'runs'] as const) {
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

  it('defaults compactMode to skills-on, topology/runs-off', () => {
    const state = useUIStore.getState();
    expect(state.compactMode.skills).toBe(true);
    expect(state.compactMode.topology).toBe(false);
    expect(state.compactMode.runs).toBe(false);
  });

  it('setCompactMode updates a single workspace without touching the others', () => {
    act(() => {
      useUIStore.getState().setCompactMode('topology', true);
    });
    const state = useUIStore.getState();
    expect(state.compactMode.topology).toBe(true);
    expect(state.compactMode.skills).toBe(true);
    expect(state.compactMode.runs).toBe(false);
  });

  it('toggleCompactMode flips only the targeted workspace', () => {
    act(() => {
      useUIStore.getState().toggleCompactMode('skills');
    });
    expect(useUIStore.getState().compactMode.skills).toBe(false);
    act(() => {
      useUIStore.getState().toggleCompactMode('skills');
    });
    expect(useUIStore.getState().compactMode.skills).toBe(true);
    // Other workspaces unaffected.
    expect(useUIStore.getState().compactMode.topology).toBe(false);
  });
});
