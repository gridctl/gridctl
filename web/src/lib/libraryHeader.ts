// Row widths (px) below which the Library header moves its secondary actions
// into a "More actions" menu. Measured from the kind switch plus the full
// action cluster, with and without the "Sync sources (N updates)" pill.
export const LIBRARY_ACTIONS_COLLAPSE_BELOW = { withUpdates: 990, base: 850 } as const;

/** Width 0 or less means unmeasured, which keeps the full layout. */
export function shouldCollapseLibraryActions(width: number, hasUpdates: boolean): boolean {
  if (width <= 0) return false;
  const threshold = hasUpdates
    ? LIBRARY_ACTIONS_COLLAPSE_BELOW.withUpdates
    : LIBRARY_ACTIONS_COLLAPSE_BELOW.base;
  return width < threshold;
}
