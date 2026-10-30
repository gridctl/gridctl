/** Release a pointer capture without throwing if the gesture already ended. */
export function releasePointerCapture(target: Element, pointerId: number) {
  if (typeof target.releasePointerCapture !== 'function') return;
  try {
    if (typeof target.hasPointerCapture === 'function' && !target.hasPointerCapture(pointerId)) {
      return;
    }
    target.releasePointerCapture(pointerId);
  } catch {
    // Already released after blur or a cancelled gesture.
  }
}
