const REDUCED_MOTION_QUERY = '(prefers-reduced-motion: reduce)';

// Read at call time so a preference change applies to the next animation
// without a reload. jsdom and older embeds have no matchMedia.
export function prefersReducedMotion(): boolean {
  return (
    typeof window !== 'undefined' &&
    typeof window.matchMedia === 'function' &&
    window.matchMedia(REDUCED_MOTION_QUERY).matches
  );
}

// Viewport animation length in ms: 0 (an instant move) under reduced motion.
export function motionDuration(ms: number): number {
  return prefersReducedMotion() ? 0 : ms;
}
