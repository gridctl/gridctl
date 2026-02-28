import { type RefObject } from 'react';
import { useTextZoom } from './useTextZoom';

/**
 * Manages log font size with persistence and Ctrl+Scroll zoom.
 * Attach the returned containerRef to the scrollable log container
 * to enable Ctrl+Scroll zoom within that area.
 *
 * Delegates to useTextZoom internally.
 */
export function useLogFontSize(containerRef?: RefObject<HTMLElement | null>) {
  const { fontSize, zoomIn, zoomOut, resetZoom, isMin, isMax, isDefault } = useTextZoom({
    storageKey: 'gridctl-log-font-size',
    defaultSize: 11,
    minSize: 8,
    maxSize: 22,
    containerRef,
  });

  return { fontSize, zoomIn, zoomOut, resetZoom, isMin, isMax, isDefault };
}
