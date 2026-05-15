import { useEffect } from 'react';
import { WORKSPACE_CONFIG, type Workspace } from '../types/workspace';

interface ShortcutOptions {
  onFitView?: () => void;
  onEscape?: () => void;
  onSelectAll?: () => void;
  onZoomIn?: () => void;
  onZoomOut?: () => void;
  onRefresh?: () => void;
  onToggleBottomPanel?: () => void;
  onSwitchToTraces?: () => void;
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
      // Ignore if typing in input
      if (
        e.target instanceof HTMLInputElement ||
        e.target instanceof HTMLTextAreaElement
      ) {
        return;
      }

      const isMod = e.metaKey || e.ctrlKey;

      // Fit view: F or Cmd/Ctrl+0
      if (e.key === 'f' || e.key === 'F' || (isMod && e.key === '0')) {
        e.preventDefault();
        options.onFitView?.();
      }

      // Escape: Close panel
      if (e.key === 'Escape') {
        options.onEscape?.();
      }

      // Select all: Cmd/Ctrl+A
      if (isMod && e.key === 'a') {
        e.preventDefault();
        options.onSelectAll?.();
      }

      // Zoom: Cmd/Ctrl + +/-
      if (isMod && (e.key === '+' || e.key === '=')) {
        e.preventDefault();
        options.onZoomIn?.();
      }
      if (isMod && e.key === '-') {
        e.preventDefault();
        options.onZoomOut?.();
      }

      // Refresh: Cmd/Ctrl+Shift+R (Cmd+R alone is reserved for browser refresh)
      if (isMod && e.key === 'r' && e.shiftKey) {
        e.preventDefault();
        options.onRefresh?.();
      }

      // Toggle bottom panel: Cmd/Ctrl+J
      if (isMod && e.key === 'j') {
        e.preventDefault();
        options.onToggleBottomPanel?.();
      }

      // Workspace switching: Cmd/Ctrl + <shortcutKey from WORKSPACE_CONFIG>
      if (isMod) {
        const ws = WORKSPACE_BY_KEY.get(e.key);
        if (ws) {
          e.preventDefault();
          options.onSwitchToWorkspace?.(ws);
        }
      }
      // Traces panel quick-jump: Cmd/Ctrl+4 (tabs themselves remain clickable)
      if (isMod && e.key === '4') {
        e.preventDefault();
        options.onSwitchToTraces?.();
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
