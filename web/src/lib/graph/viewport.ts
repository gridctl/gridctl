/**
 * Flow-to-screen viewport math for revealing part of the stack canvas.
 *
 * `rect` is in flow coordinates, `viewport` is React Flow's `{ x, y, zoom }`
 * transform, and `size` is the canvas in screen pixels. A flow point lands on
 * screen at `flow * zoom + viewport.x` (and `.y`).
 */

import type { Viewport } from '@xyflow/react';

export interface FlowRect {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface CanvasSize {
  width: number;
  height: number;
}

/** Breathing room kept between a revealed rect and the canvas edge, in px. */
export const REVEAL_MARGIN = 24;

/** Whether `rect` lies fully inside the canvas, inset by `margin` px. */
export function isRectVisible(
  rect: FlowRect,
  viewport: Viewport,
  size: CanvasSize,
  margin = 0,
): boolean {
  const left = rect.x * viewport.zoom + viewport.x;
  const top = rect.y * viewport.zoom + viewport.y;
  const right = left + rect.width * viewport.zoom;
  const bottom = top + rect.height * viewport.zoom;
  return (
    left >= margin &&
    top >= margin &&
    right <= size.width - margin &&
    bottom <= size.height - margin
  );
}

// Translation along one axis that moves [start, end] inside [0, extent] with
// `margin` px to spare, or 0 when it is already inside.
function axisShift(start: number, end: number, extent: number, margin: number): number {
  const room = Math.min(margin, (extent - (end - start)) / 2);
  if (start < room) return room - start;
  if (end > extent - room) return extent - room - end;
  return 0;
}

/**
 * The viewport at the same zoom that brings `rect` fully into the canvas by
 * the minimum translation, keeping up to `margin` px from the edge it crossed.
 * Returns null when `rect` is already fully visible, or when it is larger
 * than the canvas at this zoom (the caller decides how to zoom out).
 */
export function revealViewport(
  rect: FlowRect,
  viewport: Viewport,
  size: CanvasSize,
  margin = REVEAL_MARGIN,
): Viewport | null {
  if (isRectVisible(rect, viewport, size)) return null;
  const width = rect.width * viewport.zoom;
  const height = rect.height * viewport.zoom;
  if (width > size.width || height > size.height) return null;
  const left = rect.x * viewport.zoom + viewport.x;
  const top = rect.y * viewport.zoom + viewport.y;
  return {
    x: viewport.x + axisShift(left, left + width, size.width, margin),
    y: viewport.y + axisShift(top, top + height, size.height, margin),
    zoom: viewport.zoom,
  };
}

/** The smallest rect containing every input rect, or null for none. */
export function unionRect(rects: FlowRect[]): FlowRect | null {
  if (rects.length === 0) return null;
  let minX = Infinity;
  let minY = Infinity;
  let maxX = -Infinity;
  let maxY = -Infinity;
  for (const r of rects) {
    minX = Math.min(minX, r.x);
    minY = Math.min(minY, r.y);
    maxX = Math.max(maxX, r.x + r.width);
    maxY = Math.max(maxY, r.y + r.height);
  }
  return { x: minX, y: minY, width: maxX - minX, height: maxY - minY };
}
