import { useCallback, useRef, useState } from 'react';

/**
 * Draggable horizontal split-pane state, extracted from SkillEditor so the
 * Global Context editor shares the exact same resize behavior. The
 * container element receives `containerRef`; the divider receives
 * `handleMouseDown`. Ratio is the left pane's width fraction.
 */
export function useSplitPane(defaultRatio = 0.5, minRatio = 0.25, maxRatio = 0.75, onCommit?: (ratio: number) => void) {
  const [ratio, setRatio] = useState(defaultRatio);
  const [isDragging, setIsDragging] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);
  // Latest ratio during a drag, read on mouse-up so the committed value (which
  // we persist) is the final position rather than a stale render snapshot.
  const ratioRef = useRef(defaultRatio);

  const handleMouseDown = useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault();
      setIsDragging(true);
      document.body.style.cursor = 'col-resize';
      document.body.style.userSelect = 'none';

      const handleMouseMove = (moveEvent: MouseEvent) => {
        if (!containerRef.current) return;
        const rect = containerRef.current.getBoundingClientRect();
        const x = moveEvent.clientX - rect.left;
        const newRatio = Math.min(maxRatio, Math.max(minRatio, x / rect.width));
        ratioRef.current = newRatio;
        setRatio(newRatio);
      };

      const handleMouseUp = () => {
        setIsDragging(false);
        document.body.style.cursor = '';
        document.body.style.userSelect = '';
        document.removeEventListener('mousemove', handleMouseMove);
        document.removeEventListener('mouseup', handleMouseUp);
        onCommit?.(ratioRef.current);
      };

      document.addEventListener('mousemove', handleMouseMove);
      document.addEventListener('mouseup', handleMouseUp);
    },
    [minRatio, maxRatio, onCommit],
  );

  return { ratio, containerRef, handleMouseDown, isDragging };
}
