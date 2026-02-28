import { useState, useCallback, useEffect, useRef, type RefObject } from 'react';

interface TextZoomOptions {
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

function loadSize(key: string, defaultSize: number, min: number, max: number): number {
  try {
    const stored = localStorage.getItem(key);
    if (stored) {
      const parsed = Number(stored);
      if (!isNaN(parsed) && parsed >= min && parsed <= max) return parsed;
    }
  } catch {
    // Private browsing or quota exceeded
  }
  return defaultSize;
}

function saveSize(key: string, size: number): void {
  try {
    localStorage.setItem(key, String(size));
  } catch {
    // Ignore storage errors
  }
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
  const [fontSize, setFontSize] = useState(() => loadSize(storageKey, defaultSize, minSize, maxSize));

  const clamp = useCallback(
    (size: number) => Math.min(maxSize, Math.max(minSize, size)),
    [minSize, maxSize],
  );

  const zoomIn = useCallback(() => {
    setFontSize((prev) => {
      const next = clamp(prev + step);
      saveSize(storageKey, next);
      return next;
    });
  }, [storageKey, step, clamp]);

  const zoomOut = useCallback(() => {
    setFontSize((prev) => {
      const next = clamp(prev - step);
      saveSize(storageKey, next);
      return next;
    });
  }, [storageKey, step, clamp]);

  const resetZoom = useCallback(() => {
    setFontSize(defaultSize);
    saveSize(storageKey, defaultSize);
  }, [storageKey, defaultSize]);

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
