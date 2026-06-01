import { useEffect, useRef } from 'react';

// useDismiss wires the two standard "close this overlay" gestures, Escape and
// an outside click, to a single onClose. It returns a ref to attach to the
// overlay's wrapper; a pointer event whose target is inside that wrapper is
// treated as interaction, not dismissal, so clicks on the trigger and the
// popover body never close it. Listeners are only bound while `open` is true.
//
// Lives here (not inside a node component) because the tool pill and the
// "+N more" overflow node share the exact same dismissal contract.
export function useDismiss<T extends HTMLElement = HTMLDivElement>(
  open: boolean,
  onClose: () => void,
): React.RefObject<T | null> {
  const ref = useRef<T>(null);

  useEffect(() => {
    if (!open) return;

    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    const onPointerDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) onClose();
    };

    document.addEventListener('keydown', onKeyDown);
    document.addEventListener('mousedown', onPointerDown);
    return () => {
      document.removeEventListener('keydown', onKeyDown);
      document.removeEventListener('mousedown', onPointerDown);
    };
  }, [open, onClose]);

  return ref;
}
