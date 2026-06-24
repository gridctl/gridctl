import { useSyncExternalStore } from 'react';
import { readThemeColors, type ThemeColors } from './readThemeColors';
import { getResolvedTheme, THEME_CHANGE_EVENT } from './applyTheme';
import type { ResolvedTheme } from './types';

// Subscribe React components to live theme colors / the resolved theme. Both
// re-render on the THEME_CHANGE_EVENT fired by applyTheme().

function subscribe(cb: () => void): () => void {
  window.addEventListener(THEME_CHANGE_EVENT, cb);
  return () => window.removeEventListener(THEME_CHANGE_EVENT, cb);
}

let colorsCache: ThemeColors | null = null;
let colorsToken = 0; // bumped on theme change so getSnapshot returns a stable ref per theme

function ensureCache(): ThemeColors {
  if (!colorsCache) {
    colorsCache = readThemeColors();
  }
  return colorsCache;
}

// Invalidate the memo whenever the theme changes.
if (typeof window !== 'undefined') {
  window.addEventListener(THEME_CHANGE_EVENT, () => {
    colorsCache = readThemeColors();
    colorsToken++;
  });
}

/** Live theme colors (lib/constants COLORS shape), re-read on theme change. */
export function useThemeColors(): ThemeColors {
  return useSyncExternalStore(subscribe, ensureCache, () => ({ ...readThemeColors() }));
}

/** The concrete resolved theme ('light' | 'dark'), re-evaluated on change. */
export function useResolvedTheme(): ResolvedTheme {
  return useSyncExternalStore(
    subscribe,
    () => getResolvedTheme(),
    () => 'dark' as ResolvedTheme,
  );
}

export { colorsToken };
