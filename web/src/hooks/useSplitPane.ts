import { useCallback, useEffect, useRef, useState } from 'react';
import { releasePointerCapture } from '../lib/pointerCapture';

/**
 * Draggable horizontal split-pane state, extracted from SkillEditor so the
 * Global Context editor shares the exact same resize behavior. The
 * container element receives `containerRef`; the divider receives
 * `handlePointerDown`. Ratio is the left pane's width fraction.
 *
 * Pointer capture keeps move events on the handle for mouse, touch, and pen.
 * pointerup, pointercancel, and window blur share one cleanup so a cancelled
 * gesture cannot leave a stuck cursor.
 */
export function useSplitPane(defaultRatio = 0.5, minRatio = 0.25, maxRatio = 0.75, onCommit?: (ratio: number) => void) {
  const [ratio, setRatio] = useState(defaultRatio);
  const [isDragging, setIsDragging] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);
  // Latest ratio during a drag, read on release so the committed value (which
  // we persist) is the final position rather than a stale render snapshot.
  const ratioRef = useRef(defaultRatio);
  const endDragRef = useRef<(() => void) | null>(null);

  useEffect(() => () => endDragRef.current?.(), []);

  const handlePointerDown = useCallback(
    (e: React.PointerEvent) => {
      if (e.button !== 0) return;
      // Keep mouse text selection from starting. touch-action: none on the
      // handle covers scrolling for touch.
      e.preventDefault();
      const target = e.currentTarget;
      const pointerId = e.pointerId;
      if (typeof target.setPointerCapture === 'function') {
        target.setPointerCapture(pointerId);
      }
      setIsDragging(true);
      document.body.style.cursor = 'col-resize';
      document.body.style.userSelect = 'none';

      const handlePointerMove = (moveEvent: Event) => {
        if (!containerRef.current) return;
        const rect = containerRef.current.getBoundingClientRect();
        const x = (moveEvent as PointerEvent).clientX - rect.left;
        const newRatio = Math.min(maxRatio, Math.max(minRatio, x / rect.width));
        ratioRef.current = newRatio;
        setRatio(newRatio);
      };

      let settled = false;
      const cleanup = () => {
        if (settled) return;
        settled = true;
        endDragRef.current = null;
        setIsDragging(false);
        document.body.style.cursor = '';
        document.body.style.userSelect = '';
        target.removeEventListener('pointermove', handlePointerMove);
        target.removeEventListener('pointerup', cleanup);
        target.removeEventListener('pointercancel', cleanup);
        window.removeEventListener('blur', cleanup);
        releasePointerCapture(target, pointerId);
        onCommit?.(ratioRef.current);
      };

      endDragRef.current = cleanup;
      target.addEventListener('pointermove', handlePointerMove);
      target.addEventListener('pointerup', cleanup);
      target.addEventListener('pointercancel', cleanup);
      window.addEventListener('blur', cleanup);
    },
    [minRatio, maxRatio, onCommit],
  );

  return { ratio, containerRef, handlePointerDown, isDragging };
}
