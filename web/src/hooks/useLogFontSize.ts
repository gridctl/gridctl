import { useState, useCallback, useEffect, type RefObject } from 'react';

const STORAGE_KEY = 'gridctl-log-font-size';
const DEFAULT_FONT_SIZE = 11;
const MIN_FONT_SIZE = 8;
const MAX_FONT_SIZE = 22;
const STEP = 1;

function loadFontSize(): number {
  try {
    const stored = localStorage.getItem(STORAGE_KEY);
    if (stored) {
      const parsed = Number(stored);
      if (!isNaN(parsed) && parsed >= MIN_FONT_SIZE && parsed <= MAX_FONT_SIZE) {
        return parsed;
      }
    }
  } catch {
    // Private browsing or quota exceeded
  }
  return DEFAULT_FONT_SIZE;
}

function saveFontSize(size: number): void {
  try {
    localStorage.setItem(STORAGE_KEY, String(size));
  } catch {
    // Ignore storage errors
  }
}

/**
 * Manages log font size with persistence and Ctrl+Scroll zoom.
 * Attach the returned containerRef to the scrollable log container
 * to enable Ctrl+Scroll zoom within that area.
 */
export function useLogFontSize(containerRef?: RefObject<HTMLElement | null>) {
  const [fontSize, setFontSize] = useState(loadFontSize);

  const clamp = (size: number) => Math.min(MAX_FONT_SIZE, Math.max(MIN_FONT_SIZE, size));

  const zoomIn = useCallback(() => {
    setFontSize((prev) => {
      const next = clamp(prev + STEP);
      saveFontSize(next);
      return next;
    });
  }, []);

  const zoomOut = useCallback(() => {
    setFontSize((prev) => {
      const next = clamp(prev - STEP);
      saveFontSize(next);
      return next;
    });
  }, []);

  const resetZoom = useCallback(() => {
    setFontSize(DEFAULT_FONT_SIZE);
    saveFontSize(DEFAULT_FONT_SIZE);
  }, []);

  // Ctrl+Scroll zoom within the log container
  useEffect(() => {
    const el = containerRef?.current;
    if (!el) return;

    const handleWheel = (e: WheelEvent) => {
      if (!e.ctrlKey && !e.metaKey) return;
      e.preventDefault();
      if (e.deltaY < 0) {
        zoomIn();
      } else if (e.deltaY > 0) {
        zoomOut();
      }
    };

    el.addEventListener('wheel', handleWheel, { passive: false });
    return () => el.removeEventListener('wheel', handleWheel);
  }, [containerRef, zoomIn, zoomOut]);

  return {
    fontSize,
    zoomIn,
    zoomOut,
    resetZoom,
    isMin: fontSize <= MIN_FONT_SIZE,
    isMax: fontSize >= MAX_FONT_SIZE,
    isDefault: fontSize === DEFAULT_FONT_SIZE,
  };
}
