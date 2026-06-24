import type { ThemeMode, ResolvedTheme } from './types';

// Single source of truth for turning a ThemeMode into the concrete theme on the
// DOM. Shared by the React runtime (themes/useThemeSync) and mirrored by the
// inline boot script in index.html (kept in sync by hand — see THEME_STORAGE_KEY).

const DARK_QUERY = '(prefers-color-scheme: dark)';

/** Custom event fired after the DOM theme changes, so non-React consumers
 *  (React Flow color cache, CodeMirror) can re-read CSS variables. */
export const THEME_CHANGE_EVENT = 'gridctl:themechange';

export function systemPrefersDark(): boolean {
  return (
    typeof window !== 'undefined' &&
    typeof window.matchMedia === 'function' &&
    window.matchMedia(DARK_QUERY).matches
  );
}

export function resolveTheme(mode: ThemeMode): ResolvedTheme {
  if (mode === 'system') return systemPrefersDark() ? 'dark' : 'light';
  return mode;
}

export function getResolvedTheme(): ResolvedTheme {
  return document.documentElement.dataset.theme === 'light' ? 'light' : 'dark';
}

let transitionTimer: ReturnType<typeof setTimeout> | undefined;

/**
 * Apply a theme mode to <html>: set data-theme + color-scheme and notify
 * non-React consumers. `animate` adds a brief color-only crossfade (suppressed
 * for reduced-motion via the CSS media query).
 */
export function applyTheme(mode: ThemeMode, opts?: { animate?: boolean }): ResolvedTheme {
  const resolved = resolveTheme(mode);
  const root = document.documentElement;

  if (opts?.animate) {
    root.classList.add('theme-transitioning');
    if (transitionTimer) clearTimeout(transitionTimer);
    transitionTimer = setTimeout(() => root.classList.remove('theme-transitioning'), 220);
  }

  root.dataset.theme = resolved;
  root.style.colorScheme = resolved;

  window.dispatchEvent(
    new CustomEvent(THEME_CHANGE_EVENT, { detail: { mode, resolved } }),
  );
  return resolved;
}

/**
 * Subscribe to OS color-scheme changes. Caller should only keep the
 * subscription active while the mode is 'system'.
 */
export function subscribeSystemTheme(cb: () => void): () => void {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') {
    return () => {};
  }
  const mq = window.matchMedia(DARK_QUERY);
  const handler = () => cb();
  mq.addEventListener('change', handler);
  return () => mq.removeEventListener('change', handler);
}
