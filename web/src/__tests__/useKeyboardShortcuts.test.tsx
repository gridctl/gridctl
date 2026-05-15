import { describe, it, expect, vi } from 'vitest';
import { renderHook } from '@testing-library/react';
import { useKeyboardShortcuts } from '../hooks/useKeyboardShortcuts';

function fireMod(key: string) {
  const ev = new KeyboardEvent('keydown', { key, metaKey: true, bubbles: true });
  window.dispatchEvent(ev);
}

describe('useKeyboardShortcuts — workspace navigation', () => {
  it('⌘1 calls onSwitchToTopology', () => {
    const onSwitchToTopology = vi.fn();
    renderHook(() => useKeyboardShortcuts({ onSwitchToTopology }));
    fireMod('1');
    expect(onSwitchToTopology).toHaveBeenCalledTimes(1);
  });

  it('⌘2 calls onSwitchToSkills', () => {
    const onSwitchToSkills = vi.fn();
    renderHook(() => useKeyboardShortcuts({ onSwitchToSkills }));
    fireMod('2');
    expect(onSwitchToSkills).toHaveBeenCalledTimes(1);
  });

  it('⌘3 calls onSwitchToRuns', () => {
    const onSwitchToRuns = vi.fn();
    renderHook(() => useKeyboardShortcuts({ onSwitchToRuns }));
    fireMod('3');
    expect(onSwitchToRuns).toHaveBeenCalledTimes(1);
  });

  it('does not fire workspace shortcuts when focus is inside an input', () => {
    const onSwitchToSkills = vi.fn();
    renderHook(() => useKeyboardShortcuts({ onSwitchToSkills }));

    const input = document.createElement('input');
    document.body.appendChild(input);
    input.focus();

    const ev = new KeyboardEvent('keydown', { key: '2', metaKey: true, bubbles: true });
    Object.defineProperty(ev, 'target', { value: input });
    window.dispatchEvent(ev);

    expect(onSwitchToSkills).not.toHaveBeenCalled();
    input.remove();
  });

  it('⌘K still opens the palette', () => {
    const onOpenPalette = vi.fn();
    renderHook(() => useKeyboardShortcuts({ onOpenPalette }));
    fireMod('k');
    expect(onOpenPalette).toHaveBeenCalledTimes(1);
  });
});
