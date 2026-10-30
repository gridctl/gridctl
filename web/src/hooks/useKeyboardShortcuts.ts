import { useEffect } from 'react';
import { isTextInputTarget } from '../lib/dom';
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
  // Workspace navigation — Cmd/Ctrl + <shortcutKey> switches top-level
  // workspaces. The key→id mapping comes from WORKSPACE_CONFIG, so adding a
  // workspace there automatically binds its shortcut.
  onSwitchToWorkspace?: (id: Workspace) => void;
  // Cmd/Ctrl+\ toggles Compact Mode for the active workspace.
  onToggleCompactMode?: () => void;
}

const WORKSPACE_BY_KEY: ReadonlyMap<string, Workspace> = new Map(
  WORKSPACE_CONFIG.map((w) => [w.shortcutKey, w.id]),
);

export function useKeyboardShortcuts(options: ShortcutOptions) {
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      // Inputs, textareas, selects, and contenteditable (the JSON editor)
      // keep their native keys. Cmd/Ctrl+F must also reach the browser.
      if (isTextInputTarget(e.target)) return;

      const isMod = e.metaKey || e.ctrlKey;

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

      // Refresh: Cmd/Ctrl+Shift+R (Cmd+R alone is reserved for browser refresh)
      if (isMod && e.key === 'r' && e.shiftKey) {
        e.preventDefault();
        options.onRefresh?.();
      }

      // Open Logs workspace: Cmd/Ctrl+J
      if (isMod && e.key === 'j') {
        e.preventDefault();
        options.onOpenLogs?.();
      }

      // Workspace switching: Cmd/Ctrl + <shortcutKey from WORKSPACE_CONFIG>
      if (isMod) {
        const ws = WORKSPACE_BY_KEY.get(e.key);
        if (ws) {
          e.preventDefault();
          options.onSwitchToWorkspace?.(ws);
        }
      }
      // Open command palette: Cmd/Ctrl+K
      if (isMod && e.key === 'k') {
        e.preventDefault();
        options.onOpenPalette?.();
      }

      // Toggle Compact Mode: Cmd/Ctrl+\
      if (isMod && e.key === '\\') {
        e.preventDefault();
        options.onToggleCompactMode?.();
      }
    };

    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [options]);
}
