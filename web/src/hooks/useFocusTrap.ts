import { useEffect, useRef } from 'react';

const FOCUSABLE_SELECTOR = [
  'a[href]',
  'area[href]',
  'input:not([disabled]):not([type="hidden"])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  'button:not([disabled])',
  'iframe',
  'object',
  'embed',
  '[contenteditable="true"]',
  '[tabindex]:not([tabindex^="-"])',
].join(',');

function getFocusable(container: HTMLElement): HTMLElement[] {
  return Array.from(container.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR)).filter(
    (el) => !el.hasAttribute('aria-hidden') && el.offsetParent !== null,
  );
}

interface UseFocusTrapOptions {
  /** Whether the trap is currently active. */
  active: boolean;
  /** Optional ref to the element that should receive initial focus. If omitted, focuses the first focusable element. */
  initialFocusRef?: React.RefObject<HTMLElement | null>;
  /** If true, restores focus to the previously focused element when the trap deactivates. Defaults to true. */
  restoreFocus?: boolean;
}

/**
 * Traps keyboard focus within a container while active, and (by default) restores
 * focus to the previously focused element when the trap deactivates. Intended for
 * modal dialogs.
 */
export function useFocusTrap<T extends HTMLElement = HTMLElement>({
  active,
  initialFocusRef,
  restoreFocus = true,
}: UseFocusTrapOptions): React.RefObject<T | null> {
  const containerRef = useRef<T | null>(null);
  const previouslyFocusedRef = useRef<HTMLElement | null>(null);

  useEffect(() => {
    if (!active) return;

    const container = containerRef.current;
    if (!container) return;

    previouslyFocusedRef.current = document.activeElement as HTMLElement | null;

    // Defer initial focus one frame so the container is fully in the DOM/layout
    // and any autofocus attributes have settled.
    const rafId = requestAnimationFrame(() => {
      const target =
        initialFocusRef?.current ??
        getFocusable(container)[0] ??
        container;
      if (target && !container.contains(document.activeElement)) {
        if (!target.hasAttribute('tabindex') && target === container) {
          target.setAttribute('tabindex', '-1');
        }
        target.focus();
      }
    });

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key !== 'Tab') return;
      const focusable = getFocusable(container);
      if (focusable.length === 0) {
        e.preventDefault();
        container.focus();
        return;
      }
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      const activeEl = document.activeElement as HTMLElement | null;

      if (e.shiftKey) {
        if (activeEl === first || !container.contains(activeEl)) {
          e.preventDefault();
          last.focus();
        }
      } else {
        if (activeEl === last || !container.contains(activeEl)) {
          e.preventDefault();
          first.focus();
        }
      }
    };

    document.addEventListener('keydown', handleKeyDown);

    return () => {
      cancelAnimationFrame(rafId);
      document.removeEventListener('keydown', handleKeyDown);
      if (restoreFocus) {
        const previous = previouslyFocusedRef.current;
        if (previous && document.contains(previous)) {
          previous.focus();
        }
      }
    };
  }, [active, initialFocusRef, restoreFocus]);

  return containerRef;
}
