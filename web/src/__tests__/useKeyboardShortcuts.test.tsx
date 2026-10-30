import { describe, it, expect, vi } from 'vitest';
import { renderHook } from '@testing-library/react';
import { useKeyboardShortcuts } from '../hooks/useKeyboardShortcuts';

function fireMod(key: string) {
  const ev = new KeyboardEvent('keydown', { key, metaKey: true, bubbles: true });
  window.dispatchEvent(ev);
}

describe('useKeyboardShortcuts — workspace navigation', () => {
  it('⌘1 calls onSwitchToWorkspace with "stack"', () => {
    const onSwitchToWorkspace = vi.fn();
    renderHook(() => useKeyboardShortcuts({ onSwitchToWorkspace }));
    fireMod('1');
    expect(onSwitchToWorkspace).toHaveBeenCalledTimes(1);
    expect(onSwitchToWorkspace).toHaveBeenCalledWith('stack');
  });

  it('⌘2 calls onSwitchToWorkspace with "library"', () => {
    const onSwitchToWorkspace = vi.fn();
    renderHook(() => useKeyboardShortcuts({ onSwitchToWorkspace }));
    fireMod('2');
    expect(onSwitchToWorkspace).toHaveBeenCalledTimes(1);
    expect(onSwitchToWorkspace).toHaveBeenCalledWith('library');
  });

  it('⌘3 calls onSwitchToWorkspace with "vault"', () => {
    const onSwitchToWorkspace = vi.fn();
    renderHook(() => useKeyboardShortcuts({ onSwitchToWorkspace }));
    fireMod('3');
    expect(onSwitchToWorkspace).toHaveBeenCalledTimes(1);
    expect(onSwitchToWorkspace).toHaveBeenCalledWith('vault');
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

  it('Escape calls onEscape', () => {
    const onEscape = vi.fn();
    renderHook(() => useKeyboardShortcuts({ onEscape }));
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    expect(onEscape).toHaveBeenCalledTimes(1);
  });

  it('⌘\\ calls onToggleCompactMode', () => {
    const onToggleCompactMode = vi.fn();
    renderHook(() => useKeyboardShortcuts({ onToggleCompactMode }));
    fireMod('\\');
    expect(onToggleCompactMode).toHaveBeenCalledTimes(1);
  });
});

function dispatchKey(
  key: string,
  init: KeyboardEventInit = {},
  target?: EventTarget,
) {
  const ev = new KeyboardEvent('keydown', {
    key,
    bubbles: true,
    cancelable: true,
    ...init,
  });
  if (target) Object.defineProperty(ev, 'target', { value: target });
  window.dispatchEvent(ev);
  return ev;
}

describe('useKeyboardShortcuts — editable targets and browser chords', () => {
  it('does not swallow f, Cmd+A, or Cmd+Plus in a contenteditable editor', () => {
    const onFitView = vi.fn();
    const onZoomIn = vi.fn();
    renderHook(() => useKeyboardShortcuts({ onFitView, onZoomIn }));

    const editor = document.createElement('div');
    editor.contentEditable = 'true';
    Object.defineProperty(editor, 'isContentEditable', { value: true });
    document.body.appendChild(editor);
    editor.focus();

    const fit = dispatchKey('f', {}, editor);
    const selectAll = dispatchKey('a', { metaKey: true }, editor);
    const zoom = dispatchKey('+', { metaKey: true }, editor);

    expect(onFitView).not.toHaveBeenCalled();
    expect(onZoomIn).not.toHaveBeenCalled();
    expect(fit.defaultPrevented).toBe(false);
    expect(selectAll.defaultPrevented).toBe(false);
    expect(zoom.defaultPrevented).toBe(false);
    editor.remove();
  });

  it('does not swallow shortcuts while a select is focused', () => {
    const onFitView = vi.fn();
    const onSwitchToWorkspace = vi.fn();
    renderHook(() => useKeyboardShortcuts({ onFitView, onSwitchToWorkspace }));

    const select = document.createElement('select');
    document.body.appendChild(select);
    select.focus();

    const fit = dispatchKey('f', {}, select);
    const workspace = dispatchKey('2', { metaKey: true }, select);

    expect(onFitView).not.toHaveBeenCalled();
    expect(onSwitchToWorkspace).not.toHaveBeenCalled();
    expect(fit.defaultPrevented).toBe(false);
    expect(workspace.defaultPrevented).toBe(false);
    select.remove();
  });

  it('leaves Cmd/Ctrl+F for the browser and fits on bare f and Cmd+0', () => {
    const onFitView = vi.fn();
    renderHook(() => useKeyboardShortcuts({ onFitView }));

    const cmdF = dispatchKey('f', { metaKey: true });
    const ctrlF = dispatchKey('f', { ctrlKey: true });
    expect(onFitView).not.toHaveBeenCalled();
    expect(cmdF.defaultPrevented).toBe(false);
    expect(ctrlF.defaultPrevented).toBe(false);

    const bare = dispatchKey('f');
    expect(onFitView).toHaveBeenCalledTimes(1);
    expect(bare.defaultPrevented).toBe(true);

    const fitZero = dispatchKey('0', { metaKey: true });
    expect(onFitView).toHaveBeenCalledTimes(2);
    expect(fitZero.defaultPrevented).toBe(true);
  });

  it('does not prevent zoom or fit when no handler is wired', () => {
    renderHook(() => useKeyboardShortcuts({}));

    const zoom = dispatchKey('+', { metaKey: true });
    const fit = dispatchKey('0', { metaKey: true });
    const bare = dispatchKey('f');

    expect(zoom.defaultPrevented).toBe(false);
    expect(fit.defaultPrevented).toBe(false);
    expect(bare.defaultPrevented).toBe(false);
  });
});
