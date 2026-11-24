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

  it('opens shortcuts on ? and ignores the key in a field or dialog', () => {
    const onOpenShortcuts = vi.fn();
    renderHook(() => useKeyboardShortcuts({ onOpenShortcuts }));

    dispatchKey('?');
    expect(onOpenShortcuts).toHaveBeenCalledTimes(1);

    const input = document.createElement('input');
    document.body.appendChild(input);
    dispatchKey('?', {}, input);
    expect(onOpenShortcuts).toHaveBeenCalledTimes(1);
    input.remove();

    const dialog = document.createElement('div');
    dialog.setAttribute('role', 'dialog');
    const button = document.createElement('button');
    dialog.appendChild(button);
    document.body.appendChild(dialog);
    dispatchKey('?', {}, button);
    expect(onOpenShortcuts).toHaveBeenCalledTimes(1);
    dialog.remove();
  });
});

describe('useKeyboardShortcuts — modal dialogs own the keyboard', () => {
  function appendDialog(role: string, modal: boolean) {
    const dialog = document.createElement('div');
    dialog.setAttribute('role', role);
    if (modal) dialog.setAttribute('aria-modal', 'true');
    const button = document.createElement('button');
    dialog.appendChild(button);
    document.body.appendChild(dialog);
    return { dialog, button };
  }

  function allHandlers() {
    return {
      onFitView: vi.fn(),
      onEscape: vi.fn(),
      onZoomIn: vi.fn(),
      onZoomOut: vi.fn(),
      onRefresh: vi.fn(),
      onOpenLogs: vi.fn(),
      onOpenPalette: vi.fn(),
      onSwitchToWorkspace: vi.fn(),
      onToggleCompactMode: vi.fn(),
      onOpenShortcuts: vi.fn(),
    };
  }

  it('calls no handler and prevents no default while an aria-modal dialog is open', () => {
    const handlers = allHandlers();
    renderHook(() => useKeyboardShortcuts(handlers));
    const { dialog, button } = appendDialog('dialog', true);

    const events = [
      dispatchKey('k', { metaKey: true }, button),
      dispatchKey('2', { metaKey: true }, button),
      dispatchKey('f', {}, button),
      dispatchKey('0', { ctrlKey: true }, button),
      dispatchKey('?', { shiftKey: true }, button),
      dispatchKey('j', { metaKey: true }, button),
      dispatchKey('r', { metaKey: true, shiftKey: true }, button),
      dispatchKey('\\', { ctrlKey: true }, button),
      dispatchKey('+', { metaKey: true }, button),
      dispatchKey('Escape', {}, button),
      // Focus can fall to <body> while the dialog is still open.
      dispatchKey('f', {}, document.body),
      dispatchKey('Escape', {}, document.body),
    ];

    for (const fn of Object.values(handlers)) expect(fn).not.toHaveBeenCalled();
    for (const ev of events) expect(ev.defaultPrevented).toBe(false);
    dialog.remove();
  });

  it('ignores ? under a modal alertdialog', () => {
    const onOpenShortcuts = vi.fn();
    renderHook(() => useKeyboardShortcuts({ onOpenShortcuts }));
    const { dialog, button } = appendDialog('alertdialog', true);
    const ev = dispatchKey('?', { shiftKey: true }, button);
    expect(onOpenShortcuts).not.toHaveBeenCalled();
    expect(ev.defaultPrevented).toBe(false);
    dialog.remove();
  });

  it('ignores ? typed inside a non-modal alertdialog', () => {
    const onOpenShortcuts = vi.fn();
    renderHook(() => useKeyboardShortcuts({ onOpenShortcuts }));
    const { dialog, button } = appendDialog('alertdialog', false);
    dispatchKey('?', { shiftKey: true }, button);
    expect(onOpenShortcuts).not.toHaveBeenCalled();
    dialog.remove();
  });

  it('still fits the view beside a non-modal dialog', () => {
    const onFitView = vi.fn();
    renderHook(() => useKeyboardShortcuts({ onFitView }));
    const { dialog, button } = appendDialog('dialog', false);
    const ev = dispatchKey('f', {}, button);
    expect(onFitView).toHaveBeenCalledTimes(1);
    expect(ev.defaultPrevented).toBe(true);
    dialog.remove();
  });

  it('acts again once the modal dialog is gone', () => {
    const onOpenPalette = vi.fn();
    renderHook(() => useKeyboardShortcuts({ onOpenPalette }));
    const { dialog } = appendDialog('dialog', true);
    dispatchKey('k', { metaKey: true });
    expect(onOpenPalette).not.toHaveBeenCalled();
    dialog.remove();
    dispatchKey('k', { metaKey: true });
    expect(onOpenPalette).toHaveBeenCalledTimes(1);
  });
});
