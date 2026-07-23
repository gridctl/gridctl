/**
 * Placement math for canvas-anchored popovers that open to the right of
 * their anchor (the tool detail card). Pure geometry so it can be
 * unit-tested without React Flow or a layout engine: callers pass the
 * rendered card's bounding rect and the canvas container's rect, both in
 * screen space, so zoom scaling is already baked in and no viewport math is
 * needed here.
 */

// "Bounds" rather than "EdgeRect": inside lib/graph, "edge" already means a
// graph edge. DOMRect satisfies this structurally.
export interface HorizontalBounds {
  right: number;
  width: number;
}

/**
 * Screen-space distance by which a right-opening card overruns the
 * container's right edge, including a small breathing margin; zero when the
 * card fits. Zero-size rects mean the environment cannot measure (jsdom, or
 * an unmounted element); report no overrun rather than guessing.
 */
export function horizontalOverflow(
  card: HorizontalBounds,
  container: HorizontalBounds,
  margin = 8,
): number {
  if (card.width <= 0 || container.width <= 0) return 0;
  return Math.max(0, card.right + margin - container.right);
}
