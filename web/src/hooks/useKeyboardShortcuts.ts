import { useEffect } from 'react';
import { isModalDialogOpen, isTextInputTarget } from '../lib/dom';
import { isMacPlatform } from '../lib/platform';
import { WORKSPACE_CONFIG, type Workspace } from '../types/workspace';

interface ShortcutOptions {
  onFitView?: () => void;
  onEscape?: () => void;
  onZoomIn?: () => void;
  onZoomOut?: () => void;
  onRefresh?: () => void;
  // Cmd/Ctrl+J opens the Logs workspace (was the bottom-panel toggle).
  onOpenLogs?: () => void;
  onOpenPalette?: () => void;
  // Workspace navigation: Cmd/Ctrl + <shortcutKey> switches top-level
  // workspaces. The key→id mapping comes from WORKSPACE_CONFIG, so adding a
  // workspace there automatically binds its shortcut.
  onSwitchToWorkspace?: (id: Workspace) => void;
  // Cmd/Ctrl+\ toggles Compact Mode for the active workspace.
  onToggleCompactMode?: () => void;
  // ? opens the keyboard shortcuts panel. Ignored in fields and dialogs.
  // Modifier chords (palette, Logs, workspaces, compact mode, and refresh)
  // also fire from fields outside dialogs; fit, zoom, and Escape do not.
  onOpenShortcuts?: () => void;
}

const WORKSPACE_BY_KEY: ReadonlyMap<string, Workspace> = new Map(
  WORKSPACE_CONFIG.map((w) => [w.shortcutKey, w.id]),
);

// Chords that also fire from a text field outside a dialog. Returns true when
// the event matched one, whether or not a handler is wired.
function handleFieldSafeChord(e: KeyboardEvent, options: ShortcutOptions): boolean {
  const key = e.key.toLowerCase();

  // Refresh: Cmd/Ctrl+Shift+R (Cmd+R alone is reserved for browser refresh)
  if (key === 'r' && e.shiftKey) {
    e.preventDefault();
    options.onRefresh?.();
    return true;
  }

  // Open Logs workspace: Cmd/Ctrl+J. Shift+J is left to browser consoles.
  if (key === 'j' && !e.shiftKey) {
    e.preventDefault();
    options.onOpenLogs?.();
    return true;
  }

  // Workspace switching: Cmd/Ctrl + <shortcutKey from WORKSPACE_CONFIG>
  const ws = WORKSPACE_BY_KEY.get(e.key);
  if (ws) {
    e.preventDefault();
    options.onSwitchToWorkspace?.(ws);
    return true;
  }

  // Open command palette: Cmd/Ctrl+K. Shift+K is left to browser consoles
  // and the editor's delete-line binding.
  if (key === 'k' && !e.shiftKey) {
    e.preventDefault();
    options.onOpenPalette?.();
    return true;
  }

  // Toggle Compact Mode: Cmd/Ctrl+\
  if (e.key === '\\') {
    e.preventDefault();
    options.onToggleCompactMode?.();
    return true;
  }

  return false;
}

export function useKeyboardShortcuts(options: ShortcutOptions) {
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      // A modal dialog owns the keyboard. Return before any preventDefault so
      // browser chords (find, zoom) still work inside it.
      if (isModalDialogOpen()) return;

      // Ctrl+Alt is AltGr on Windows, which types characters, so it never
      // counts as a modifier chord.
      const ctrl = e.ctrlKey && !e.altKey;
      const isMod = e.metaKey || ctrl;

      // Inputs, textareas, selects, and contenteditable (the JSON editor) keep
      // their native keys. Only the field-safe modifier chords pass, and none
      // pass from a field inside a dialog.
      if (isTextInputTarget(e.target)) {
        if (e.target instanceof Element && e.target.closest('[role="dialog"], [role="alertdialog"]')) {
          return;
        }
        // Ctrl+K and similar are text-editing keys in macOS fields; only Cmd counts.
        const fieldMod = isMacPlatform() ? e.metaKey : isMod;
        if (fieldMod) handleFieldSafeChord(e, options);
        return;
      }

      // Fit view: bare f/F, or Cmd/Ctrl+0, and only when a handler is wired
      // (Stack). Modified f is browser Find; do not preventDefault.
      const bareFit = (e.key === 'f' || e.key === 'F') && !e.metaKey && !e.ctrlKey && !e.altKey;
      if (bareFit && options.onFitView) {
        e.preventDefault();
        options.onFitView();
      }
      if (isMod && e.key === '0' && options.onFitView) {
        e.preventDefault();
        options.onFitView();
      }

      // Escape: Close panel
      if (e.key === 'Escape') {
        options.onEscape?.();
      }

      // Zoom: Cmd/Ctrl + +/-. Prevent only when a handler is wired, so the
      // browser keeps zoom and zoom-reset outside the Stack workspace.
      if (isMod && (e.key === '+' || e.key === '=') && options.onZoomIn) {
        e.preventDefault();
        options.onZoomIn();
      }
      if (isMod && e.key === '-' && options.onZoomOut) {
        e.preventDefault();
        options.onZoomOut();
      }

      if (isMod && handleFieldSafeChord(e, options)) return;

      // Keyboard shortcuts panel. ? is Shift+/ on a US keyboard, so shift is
      // expected. Dialogs own their keys; fields are already excluded above.
      if (e.key === '?' && !e.altKey && !e.metaKey && !e.ctrlKey && options.onOpenShortcuts) {
        const target = e.target instanceof Element ? e.target : null;
        if (target?.closest('[role="dialog"], [role="alertdialog"]')) return;
        e.preventDefault();
        options.onOpenShortcuts();
      }
    };

    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [options]);
}
