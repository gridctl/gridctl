import { describe, it, expect, vi } from 'vitest';
import { renderHook } from '@testing-library/react';
import { useKeyboardShortcuts } from '../hooks/useKeyboardShortcuts';

function fireMod(key: string) {
  const ev = new KeyboardEvent('keydown', { key, metaKey: true, bubbles: true });
  window.dispatchEvent(ev);
}

describe('useKeyboardShortcuts — workspace navigation', () => {
  it('⌘1 calls onSwitchToWorkspace with "topology"', () => {
    const onSwitchToWorkspace = vi.fn();
    renderHook(() => useKeyboardShortcuts({ onSwitchToWorkspace }));
    fireMod('1');
    expect(onSwitchToWorkspace).toHaveBeenCalledTimes(1);
    expect(onSwitchToWorkspace).toHaveBeenCalledWith('topology');
  });

  it('⌘2 calls onSwitchToWorkspace with "skills"', () => {
    const onSwitchToWorkspace = vi.fn();
    renderHook(() => useKeyboardShortcuts({ onSwitchToWorkspace }));
    fireMod('2');
    expect(onSwitchToWorkspace).toHaveBeenCalledTimes(1);
    expect(onSwitchToWorkspace).toHaveBeenCalledWith('skills');
  });

  it('⌘3 calls onSwitchToWorkspace with "runs"', () => {
    const onSwitchToWorkspace = vi.fn();
    renderHook(() => useKeyboardShortcuts({ onSwitchToWorkspace }));
    fireMod('3');
    expect(onSwitchToWorkspace).toHaveBeenCalledTimes(1);
    expect(onSwitchToWorkspace).toHaveBeenCalledWith('runs');
  });

  it('does not fire workspace shortcuts when focus is inside an input', () => {
    const onSwitchToWorkspace = vi.fn();
    renderHook(() => useKeyboardShortcuts({ onSwitchToWorkspace }));

    const input = document.createElement('input');
    document.body.appendChild(input);
    input.focus();

    const ev = new KeyboardEvent('keydown', { key: '2', metaKey: true, bubbles: true });
    Object.defineProperty(ev, 'target', { value: input });
    window.dispatchEvent(ev);

    expect(onSwitchToWorkspace).not.toHaveBeenCalled();
    input.remove();
  });

  it('⌘K still opens the palette', () => {
    const onOpenPalette = vi.fn();
    renderHook(() => useKeyboardShortcuts({ onOpenPalette }));
    fireMod('k');
    expect(onOpenPalette).toHaveBeenCalledTimes(1);
  });

  it('⌘\\ calls onToggleCompactMode', () => {
    const onToggleCompactMode = vi.fn();
    renderHook(() => useKeyboardShortcuts({ onToggleCompactMode }));
    fireMod('\\');
    expect(onToggleCompactMode).toHaveBeenCalledTimes(1);
  });
});
