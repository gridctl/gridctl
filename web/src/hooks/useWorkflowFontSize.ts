import { type RefObject } from 'react';
import { useTextZoom } from './useTextZoom';

/**
 * Manages workflow result font size with persistence and Ctrl+Scroll zoom.
 * Delegates to useTextZoom internally.
 */
export function useWorkflowFontSize(containerRef?: RefObject<HTMLElement | null>) {
  const { fontSize, zoomIn, zoomOut, resetZoom, isMin, isMax, isDefault } = useTextZoom({
    storageKey: 'gridctl-workflow-font-size',
    defaultSize: 12,
    minSize: 8,
    maxSize: 20,
    containerRef,
  });

  return { fontSize, zoomIn, zoomOut, resetZoom, isMin, isMax, isDefault };
}
