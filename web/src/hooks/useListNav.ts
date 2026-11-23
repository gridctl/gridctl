import { useEffect, useLayoutEffect, useRef } from 'react';
import { isTextInputTarget } from '../lib/dom';

interface UseListNavOptions {
  itemCount: number;
  selectedIndex: number;
  setSelectedIndex: (i: number) => void;
  /** Called when the user presses Enter on the page or inside the list. */
  onEnter?: () => void;
  /** Called when the user presses 'e'. */
  onEdit?: () => void;
  /** Called when the user presses 'd' — typically toggle active state. */
  onToggle?: () => void;
  /** Called when the user presses '/' — typically focus the search input. */
  onSlash?: () => void;
  /** Called when the user presses Escape — typically close a detail pane. */
  onEscape?: () => void;
  /** Disable handling (e.g. while a modal is open). Defaults to true. */
  enabled?: boolean;
}

/** Spread on the element that contains the list rows. */
export const listNavProps = { 'data-list-nav': '' } as const;

/** Spread on each focusable row so focus can follow the selection. */
export const listNavRowProps = { 'data-list-nav-row': '' } as const;

const LIST_SELECTOR = '[data-list-nav]';
const ROW_SELECTOR = '[data-list-nav-row]';
const SELECTED_ROW_SELECTOR =
  '[data-list-nav-row][aria-selected="true"], [data-list-nav-row][aria-current="true"]';
const CONTROL_SELECTOR = [
  'button',
  'a[href]',
  'summary',
  'input',
  'select',
  'textarea',
  '[role="button"]',
  '[role="link"]',
  '[role="tab"]',
  '[role="option"]',
  '[role="switch"]',
  '[role="checkbox"]',
  '[role="radio"]',
  '[role="menuitem"]',
].join(', ');

function isEditableTarget(target: EventTarget | null): boolean {
  if (isTextInputTarget(target)) return true;
  // Inside any open dialog/alertdialog — defer to the dialog's own handling.
  return (
    target instanceof HTMLElement &&
    target.closest('[role="dialog"], [role="alertdialog"]') !== null
  );
}

function classifyTarget(target: EventTarget | null): { scoped: boolean; onControl: boolean } {
  if (!(target instanceof Element)) return { scoped: true, onControl: false };
  if (target === document.body || target === document.documentElement) {
    return { scoped: true, onControl: false };
  }
  return {
    scoped: target.closest(LIST_SELECTOR) !== null,
    onControl: target.closest(CONTROL_SELECTOR) !== null,
  };
}

function isSelectedRow(el: Element): boolean {
  return el.getAttribute('aria-selected') === 'true' || el.getAttribute('aria-current') === 'true';
}

/**
 * Keyboard navigation for a list of items. Binds at the document level so
 * users don't need to tab onto the list first. Ignores keypresses when the
 * user is typing in an input/textarea/contenteditable, when focus is inside
 * an open dialog, or when a component handler already cancelled the key.
 *
 * Arrows, Home, End, 'e', and 'd' act only when focus is on the page itself
 * or inside the element marked with listNavProps, so other controls and
 * scrollable panes keep their native keys. Enter additionally defers to a
 * focused control (button, link, row with a control role) so it activates
 * natively. j, k, '/', and Escape act from any non-text target.
 *
 * When a key handled here moves the selection while a row marked with
 * listNavRowProps has focus, focus moves to the newly selected row of the
 * same list, so the focused and highlighted rows never diverge. An index
 * that shifts because the rows changed (a poll, a sort) leaves focus alone.
 *
 * Bindings:
 *   ArrowDown / ArrowUp (aliases j / k) — move selection, wraps at ends;
 *     with nothing selected, down goes to the first row and up to the last
 *   Home / End — jump to first / last
 *   Enter — call onEnter
 *   Escape — call onEscape
 *   e — call onEdit
 *   d — call onToggle
 *   / — call onSlash
 */
export function useListNav({
  itemCount,
  selectedIndex,
  setSelectedIndex,
  onEnter,
  onEdit,
  onToggle,
  onSlash,
  onEscape,
  enabled = true,
}: UseListNavOptions): void {
  // Keep latest values in a ref so the listener doesn't need to re-bind every
  // time selectedIndex changes. Written in a layout effect (not during render,
  // so an abandoned concurrent render can't leak values; not a passive effect,
  // because the document-level listener can fire between commit and passive
  // flush and must never read a stale selectedIndex).
  const state = useRef({ itemCount, selectedIndex, setSelectedIndex, onEnter, onEdit, onToggle, onSlash, onEscape });
  useLayoutEffect(() => {
    state.current = { itemCount, selectedIndex, setSelectedIndex, onEnter, onEdit, onToggle, onSlash, onEscape };
  });

  // Child rows commit before this effect runs, so the selected row already
  // carries its aria-selected / aria-current value. requestedIndex is the
  // index a key handled below asked for; any index change clears it.
  const lastIndex = useRef(selectedIndex);
  const requestedIndex = useRef<number | null>(null);
  useEffect(() => {
    if (lastIndex.current === selectedIndex) return;
    lastIndex.current = selectedIndex;
    const requested = requestedIndex.current;
    requestedIndex.current = null;
    if (!enabled || requested !== selectedIndex) return;
    const active = document.activeElement;
    if (!(active instanceof HTMLElement) || !active.matches(ROW_SELECTOR)) return;
    const list = active.closest(LIST_SELECTOR);
    if (!list || isSelectedRow(active)) return;
    const next = list.querySelector<HTMLElement>(SELECTED_ROW_SELECTOR);
    if (next && next.closest(LIST_SELECTOR) === list) next.focus({ preventScroll: true });
  }, [selectedIndex, enabled]);

  useEffect(() => {
    if (!enabled) return;

    const handler = (e: KeyboardEvent) => {
      if (e.defaultPrevented) return;
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      if (isEditableTarget(e.target)) return;

      const { itemCount, selectedIndex, setSelectedIndex, onEnter, onEdit, onToggle, onSlash, onEscape } = state.current;
      const { scoped, onControl } = classifyTarget(e.target);

      // Escape works even on an empty list (a detail pane may still be open).
      if (e.key === 'Escape') {
        if (onEscape) {
          e.preventDefault();
          onEscape();
        }
        return;
      }
      // '/' focuses search regardless of list size.
      if (e.key === '/') {
        if (onSlash) {
          e.preventDefault();
          onSlash();
        }
        return;
      }
      if (itemCount <= 0) return;

      const select = (n: number) => {
        requestedIndex.current = n;
        setSelectedIndex(n);
      };
      const clamp = (n: number) => ((n % itemCount) + itemCount) % itemCount;
      const next = () => (selectedIndex < 0 ? 0 : clamp(selectedIndex + 1));
      const prev = () => (selectedIndex < 0 ? itemCount - 1 : clamp(selectedIndex - 1));

      switch (e.key) {
        case 'j':
          e.preventDefault();
          select(next());
          return;
        case 'k':
          e.preventDefault();
          select(prev());
          return;
        case 'ArrowDown':
          if (!scoped) return;
          e.preventDefault();
          select(next());
          return;
        case 'ArrowUp':
          if (!scoped) return;
          e.preventDefault();
          select(prev());
          return;
        case 'Home':
          if (!scoped) return;
          e.preventDefault();
          select(0);
          return;
        case 'End':
          if (!scoped) return;
          e.preventDefault();
          select(itemCount - 1);
          return;
        case 'Enter':
          if (onEnter && scoped && !onControl) {
            e.preventDefault();
            onEnter();
          }
          return;
        case 'e':
          if (onEdit && scoped) {
            e.preventDefault();
            onEdit();
          }
          return;
        case 'd':
          if (onToggle && scoped) {
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
