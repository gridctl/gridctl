import { useEffect, useLayoutEffect, useRef } from 'react';

interface UseListNavOptions {
  itemCount: number;
  selectedIndex: number;
  setSelectedIndex: (i: number) => void;
  /** Called when the user presses Enter while the list has keyboard focus. */
  onEnter?: () => void;
  /** Called when the user presses 'e'. */
  onEdit?: () => void;
  /** Called when the user presses 'd' — typically toggle active state. */
  onToggle?: () => void;
  /** Called when the user presses Escape — typically close a detail pane. */
  onEscape?: () => void;
  /** Disable handling (e.g. while a modal is open). Defaults to true. */
  enabled?: boolean;
}

function isEditableTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  const tag = target.tagName;
  if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return true;
  if (target.isContentEditable) return true;
  // Inside any open dialog/alertdialog — defer to the dialog's own handling.
  if (target.closest('[role="dialog"], [role="alertdialog"]')) return true;
  return false;
}

/**
 * Keyboard navigation for a list of items. Binds at the document level so
 * users don't need to tab onto the list first. Ignores keypresses when the
 * user is typing in an input/textarea/contenteditable, or when focus is
 * inside an open dialog.
 *
 * Bindings:
 *   ArrowDown / ArrowUp (aliases j / k) — move selection, wraps at ends
 *   Home / End — jump to first / last
 *   Enter — call onEnter
 *   Escape — call onEscape
 *   e — call onEdit
 *   d — call onToggle
 */
export function useListNav({
  itemCount,
  selectedIndex,
  setSelectedIndex,
  onEnter,
  onEdit,
  onToggle,
  onEscape,
  enabled = true,
}: UseListNavOptions): void {
  // Keep latest values in a ref so the listener doesn't need to re-bind every
  // time selectedIndex changes. Written in a layout effect (not during render,
  // so an abandoned concurrent render can't leak values; not a passive effect,
  // because the document-level listener can fire between commit and passive
  // flush and must never read a stale selectedIndex).
  const state = useRef({ itemCount, selectedIndex, setSelectedIndex, onEnter, onEdit, onToggle, onEscape });
  useLayoutEffect(() => {
    state.current = { itemCount, selectedIndex, setSelectedIndex, onEnter, onEdit, onToggle, onEscape };
  });

  useEffect(() => {
    if (!enabled) return;

    const handler = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      if (isEditableTarget(e.target)) return;

      const { itemCount, selectedIndex, setSelectedIndex, onEnter, onEdit, onToggle, onEscape } = state.current;

      // Escape works even on an empty list (a detail pane may still be open).
      if (e.key === 'Escape') {
        if (onEscape) {
          e.preventDefault();
          onEscape();
        }
        return;
      }
      if (itemCount <= 0) return;

      const clamp = (n: number) => ((n % itemCount) + itemCount) % itemCount;

      switch (e.key) {
        case 'ArrowDown':
        case 'j':
          e.preventDefault();
          setSelectedIndex(clamp(selectedIndex + 1));
          return;
        case 'ArrowUp':
        case 'k':
          e.preventDefault();
          setSelectedIndex(clamp(selectedIndex - 1));
          return;
        case 'Home':
          e.preventDefault();
          setSelectedIndex(0);
          return;
        case 'End':
          e.preventDefault();
          setSelectedIndex(itemCount - 1);
          return;
        case 'Enter':
          if (onEnter) {
            e.preventDefault();
            onEnter();
          }
          return;
        case 'e':
          if (onEdit) {
            e.preventDefault();
            onEdit();
          }
          return;
        case 'd':
          if (onToggle) {
            e.preventDefault();
            onToggle();
          }
          return;
      }
    };

    document.addEventListener('keydown', handler);
    return () => document.removeEventListener('keydown', handler);
  }, [enabled]);
}
