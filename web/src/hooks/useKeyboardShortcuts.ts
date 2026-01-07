import { useEffect } from 'react';

interface ShortcutOptions {
  onFitView?: () => void;
  onEscape?: () => void;
  onSelectAll?: () => void;
  onZoomIn?: () => void;
  onZoomOut?: () => void;
  onRefresh?: () => void;
}

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

      // Refresh: Cmd/Ctrl+R (only if not browser refresh)
      if (isMod && e.key === 'r' && e.shiftKey) {
        e.preventDefault();
        options.onRefresh?.();
      }
    };

    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [options]);
}
