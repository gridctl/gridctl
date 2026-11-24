import { afterEach, describe, it, expect, vi } from 'vitest';
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

  it('fires workspace shortcuts when focus is inside an input', () => {
    const onSwitchToWorkspace = vi.fn();
    renderHook(() => useKeyboardShortcuts({ onSwitchToWorkspace }));

    const input = document.createElement('input');
    document.body.appendChild(input);
    input.focus();

    const ev = new KeyboardEvent('keydown', { key: '2', ctrlKey: true, bubbles: true, cancelable: true });
    Object.defineProperty(ev, 'target', { value: input });
    window.dispatchEvent(ev);

    expect(onSwitchToWorkspace).toHaveBeenCalledWith('library');
    expect(ev.defaultPrevented).toBe(true);
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
    const onZoomOut = vi.fn();
    const onEscape = vi.fn();
    const onOpenShortcuts = vi.fn();
    renderHook(() =>
      useKeyboardShortcuts({ onFitView, onZoomIn, onZoomOut, onEscape, onOpenShortcuts }),
    );

    const editor = document.createElement('div');
    editor.contentEditable = 'true';
    Object.defineProperty(editor, 'isContentEditable', { value: true });
    document.body.appendChild(editor);
    editor.focus();

    const fit = dispatchKey('f', {}, editor);
    const selectAll = dispatchKey('a', { metaKey: true }, editor);
    const zoom = dispatchKey('+', { metaKey: true }, editor);
    const others = [
      dispatchKey('a', { ctrlKey: true }, editor),
      dispatchKey('f', { metaKey: true }, editor),
      dispatchKey('f', { ctrlKey: true }, editor),
      dispatchKey('0', { metaKey: true }, editor),
      dispatchKey('0', { ctrlKey: true }, editor),
      dispatchKey('=', { ctrlKey: true }, editor),
      dispatchKey('-', { metaKey: true }, editor),
      dispatchKey('-', { ctrlKey: true }, editor),
      dispatchKey('?', { shiftKey: true }, editor),
      dispatchKey('Escape', {}, editor),
    ];

    expect(onFitView).not.toHaveBeenCalled();
    expect(onZoomIn).not.toHaveBeenCalled();
    expect(onZoomOut).not.toHaveBeenCalled();
    expect(onEscape).not.toHaveBeenCalled();
    expect(onOpenShortcuts).not.toHaveBeenCalled();
    expect(fit.defaultPrevented).toBe(false);
    expect(selectAll.defaultPrevented).toBe(false);
    expect(zoom.defaultPrevented).toBe(false);
    for (const ev of others) expect(ev.defaultPrevented).toBe(false);
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
    expect(onSwitchToWorkspace).toHaveBeenCalledWith('library');
    expect(fit.defaultPrevented).toBe(false);
    expect(workspace.defaultPrevented).toBe(true);
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

function allShellHandlers() {
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

function makeFields(): HTMLElement[] {
  const input = document.createElement('input');
  const textarea = document.createElement('textarea');
  const select = document.createElement('select');
  const editor = document.createElement('div');
  editor.contentEditable = 'true';
  Object.defineProperty(editor, 'isContentEditable', { value: true });
  const fields = [input, textarea, select, editor];
  for (const field of fields) document.body.appendChild(field);
  return fields;
}

describe('useKeyboardShortcuts — modifier chords from fields', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    document.body.innerHTML = '';
  });

  it('fires the field allowlist from an input, textarea, select, and contenteditable', () => {
    const handlers = allShellHandlers();
    renderHook(() => useKeyboardShortcuts(handlers));

    for (const field of makeFields()) {
      for (const ev of [
        dispatchKey('k', { ctrlKey: true }, field),
        dispatchKey('5', { ctrlKey: true }, field),
        dispatchKey('j', { ctrlKey: true }, field),
        dispatchKey('\\', { ctrlKey: true }, field),
        dispatchKey('R', { ctrlKey: true, shiftKey: true }, field),
      ]) {
        expect(ev.defaultPrevented, `${field.tagName} ${ev.key}`).toBe(true);
      }
    }

    expect(handlers.onOpenPalette).toHaveBeenCalledTimes(4);
    expect(handlers.onSwitchToWorkspace).toHaveBeenCalledTimes(4);
    expect(handlers.onSwitchToWorkspace).toHaveBeenCalledWith('metrics');
    expect(handlers.onOpenLogs).toHaveBeenCalledTimes(4);
    expect(handlers.onToggleCompactMode).toHaveBeenCalledTimes(4);
    expect(handlers.onRefresh).toHaveBeenCalledTimes(4);
    expect(handlers.onFitView).not.toHaveBeenCalled();
    expect(handlers.onEscape).not.toHaveBeenCalled();
  });

  it('refreshes on Ctrl+Shift+R with an uppercase key, in and out of a field', () => {
    const onRefresh = vi.fn();
    renderHook(() => useKeyboardShortcuts({ onRefresh }));

    const outside = dispatchKey('R', { ctrlKey: true, shiftKey: true });
    expect(onRefresh).toHaveBeenCalledTimes(1);
    expect(outside.defaultPrevented).toBe(true);

    const [input] = makeFields();
    const inField = dispatchKey('R', { ctrlKey: true, shiftKey: true }, input);
    expect(onRefresh).toHaveBeenCalledTimes(2);
    expect(inField.defaultPrevented).toBe(true);
  });

  it('matches K and J with Caps Lock on and Shift up', () => {
    const onOpenPalette = vi.fn();
    const onOpenLogs = vi.fn();
    renderHook(() => useKeyboardShortcuts({ onOpenPalette, onOpenLogs }));

    expect(dispatchKey('K', { metaKey: true }).defaultPrevented).toBe(true);
    expect(dispatchKey('J', { ctrlKey: true }).defaultPrevented).toBe(true);
    expect(onOpenPalette).toHaveBeenCalledTimes(1);
    expect(onOpenLogs).toHaveBeenCalledTimes(1);
  });

  it('leaves Ctrl+Shift+K and Ctrl+Shift+J to the browser', () => {
    const handlers = allShellHandlers();
    renderHook(() => useKeyboardShortcuts(handlers));
    const [input] = makeFields();

    const events = [
      dispatchKey('K', { ctrlKey: true, shiftKey: true }),
      dispatchKey('J', { ctrlKey: true, shiftKey: true }),
      dispatchKey('K', { ctrlKey: true, shiftKey: true }, input),
      dispatchKey('J', { ctrlKey: true, shiftKey: true }, input),
    ];

    for (const fn of Object.values(handlers)) expect(fn).not.toHaveBeenCalled();
    for (const ev of events) expect(ev.defaultPrevented).toBe(false);
  });

  it('never treats Ctrl+Alt (AltGr) as a modifier chord, in or out of a field', () => {
    const handlers = allShellHandlers();
    renderHook(() => useKeyboardShortcuts(handlers));
    const [input] = makeFields();

    const events = [
      dispatchKey('\\', { ctrlKey: true, altKey: true }, input),
      dispatchKey('\\', { ctrlKey: true, altKey: true }),
      dispatchKey('k', { ctrlKey: true, altKey: true }),
      dispatchKey('2', { ctrlKey: true, altKey: true }),
      dispatchKey('0', { ctrlKey: true, altKey: true }),
      dispatchKey('+', { ctrlKey: true, altKey: true }),
      dispatchKey('R', { ctrlKey: true, altKey: true, shiftKey: true }),
    ];

    for (const fn of Object.values(handlers)) expect(fn).not.toHaveBeenCalled();
    for (const ev of events) expect(ev.defaultPrevented).toBe(false);
  });

  it('counts only Cmd in a field on macOS', () => {
    vi.spyOn(window.navigator, 'platform', 'get').mockReturnValue('MacIntel');
    const onOpenPalette = vi.fn();
    renderHook(() => useKeyboardShortcuts({ onOpenPalette }));
    const fields = makeFields();

    let calls = 0;
    for (const field of fields) {
      const ctrlInField = dispatchKey('k', { ctrlKey: true }, field);
      expect(onOpenPalette).toHaveBeenCalledTimes(calls);
      expect(ctrlInField.defaultPrevented).toBe(false);

      const cmdInField = dispatchKey('k', { metaKey: true }, field);
      calls += 1;
      expect(onOpenPalette).toHaveBeenCalledTimes(calls);
      expect(cmdInField.defaultPrevented).toBe(true);
    }

    const ctrlOutside = dispatchKey('k', { ctrlKey: true });
    expect(onOpenPalette).toHaveBeenCalledTimes(calls + 1);
    expect(ctrlOutside.defaultPrevented).toBe(true);
  });

  it.each(['dialog', 'alertdialog'])('ignores chords from a field inside a role="%s"', (role) => {
    const handlers = allShellHandlers();
    renderHook(() => useKeyboardShortcuts(handlers));

    const dialog = document.createElement('div');
    dialog.setAttribute('role', role);
    const input = document.createElement('input');
    dialog.appendChild(input);
    document.body.appendChild(dialog);

    const events = [
      dispatchKey('k', { ctrlKey: true }, input),
      dispatchKey('2', { ctrlKey: true }, input),
      dispatchKey('k', { metaKey: true }, input),
      dispatchKey('R', { ctrlKey: true, shiftKey: true }, input),
    ];

    for (const fn of Object.values(handlers)) expect(fn).not.toHaveBeenCalled();
    for (const ev of events) expect(ev.defaultPrevented).toBe(false);
  });
});
