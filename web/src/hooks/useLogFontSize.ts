import { type RefObject } from 'react';
import { LOG_TEXT_ZOOM } from '../lib/textZoomDefaults';
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
    storageKey: LOG_TEXT_ZOOM.name,
    defaultSize: LOG_TEXT_ZOOM.defaultSize,
    minSize: LOG_TEXT_ZOOM.minSize,
    maxSize: LOG_TEXT_ZOOM.maxSize,
    step: LOG_TEXT_ZOOM.step,
    containerRef,
  });

  return { fontSize, zoomIn, zoomOut, resetZoom, isMin, isMax, isDefault };
}
