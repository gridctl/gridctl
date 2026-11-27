import { useState, useEffect, useLayoutEffect, type RefObject } from 'react';

export type LayoutSize = 'small' | 'medium' | 'large';

/**
 * Tracks the width of a container element using ResizeObserver.
 * Returns the current width and a layout size classification.
 *
 * The first measurement is taken synchronously in a layout effect, so a
 * narrow container never paints one frame at the unmeasured width.
 *
 * - small: < 600px (hide toolbox, compact inspector)
 * - medium: 600-900px (collapsed toolbox, min-width inspector)
 * - large: > 900px (full layout)
 */
export function useContainerWidth(ref: RefObject<HTMLElement | null>) {
  const [width, setWidth] = useState(0);

  useLayoutEffect(() => {
    if (!ref.current) return;
    setWidth(ref.current.getBoundingClientRect().width);
  }, [ref]);

  useEffect(() => {
    if (!ref.current) return;
    const observer = new ResizeObserver(([entry]) => {
      setWidth(entry.contentRect.width);
    });
    observer.observe(ref.current);
    return () => observer.disconnect();
  }, [ref]);

  const layoutSize: LayoutSize =
    width > 900 ? 'large' :
    width > 600 ? 'medium' :
    'small';

  return { width, layoutSize };
}
