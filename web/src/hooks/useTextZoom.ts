import { useCallback, useEffect, useRef, type RefObject } from 'react';
import { usePref, writePref } from '../lib/preferences';

interface TextZoomOptions {
  /** Preference name. The hook prefixes it; do not pass a raw storage key. */
  storageKey: string;
  defaultSize: number;
  minSize?: number;
  maxSize?: number;
  step?: number;
  /** External container ref for Ctrl+Scroll. If not provided, uses internal ref via containerProps. */
  containerRef?: RefObject<HTMLElement | null>;
}

interface TextZoomResult {
  fontSize: number;
  zoomIn: () => void;
  zoomOut: () => void;
  resetZoom: () => void;
  isMin: boolean;
  isMax: boolean;
  isDefault: boolean;
  /** Spread onto the zoomable container: ref + style with CSS custom property */
  containerProps: {
    ref: RefObject<HTMLDivElement | null>;
    style: React.CSSProperties;
  };
}

function parseSize(stored: string | null, defaultSize: number, min: number, max: number): number {
  if (stored) {
    const parsed = Number(stored);
    if (!isNaN(parsed) && parsed >= min && parsed <= max) return parsed;
  }
  return defaultSize;
}

export function useTextZoom(options: TextZoomOptions): TextZoomResult {
  const {
    storageKey,
    defaultSize,
    minSize = 8,
    maxSize = 22,
    step = 1,
    containerRef: externalRef,
  } = options;

  const internalRef = useRef<HTMLDivElement>(null);
  const stored = usePref(storageKey);
  const fontSize = parseSize(stored, defaultSize, minSize, maxSize);

  const clamp = useCallback(
    (size: number) => Math.min(maxSize, Math.max(minSize, size)),
    [minSize, maxSize],
  );

  const write = useCallback((size: number) => {
    writePref(storageKey, String(size));
  }, [storageKey]);

  const zoomIn = useCallback(() => {
    write(clamp(fontSize + step));
  }, [fontSize, step, clamp, write]);

  const zoomOut = useCallback(() => {
    write(clamp(fontSize - step));
  }, [fontSize, step, clamp, write]);

  const resetZoom = useCallback(() => {
    write(defaultSize);
  }, [write, defaultSize]);

  // Ctrl+Scroll zoom - attach to external ref if provided, else internal ref
  useEffect(() => {
    const el = externalRef?.current ?? internalRef.current;
    if (!el) return;

    const handleWheel = (e: WheelEvent) => {
      if (!e.ctrlKey && !e.metaKey) return;
      e.preventDefault();
      if (e.deltaY < 0) zoomIn();
      else if (e.deltaY > 0) zoomOut();
    };

    el.addEventListener('wheel', handleWheel, { passive: false });
    return () => el.removeEventListener('wheel', handleWheel);
  }, [externalRef, zoomIn, zoomOut]);

  return {
    fontSize,
    zoomIn,
    zoomOut,
    resetZoom,
    isMin: fontSize <= minSize,
    isMax: fontSize >= maxSize,
    isDefault: fontSize === defaultSize,
    containerProps: {
      ref: internalRef,
      style: { '--text-zoom-size': `${fontSize}px` } as React.CSSProperties,
    },
  };
}
