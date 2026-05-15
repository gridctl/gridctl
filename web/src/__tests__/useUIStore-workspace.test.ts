import { describe, it, expect, beforeEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useUIStore } from '../stores/useUIStore';

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
