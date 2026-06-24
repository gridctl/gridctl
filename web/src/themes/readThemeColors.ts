import { COLORS } from '../lib/constants';

// COLORS (lib/constants) holds the dark defaults as a frozen literal. React Flow
// and canvas code read colors in JS (not CSS), so they can't follow [data-theme]
// on their own. readThemeColors() reads the live computed custom properties and
// returns the same shape, falling back to the dark literal when a var is unset
// (e.g. during SSR/tests). Pair with useThemeColors() to re-read on theme change.

export type ThemeColors = { [K in keyof typeof COLORS]: string };

function read(styles: CSSStyleDeclaration, name: string, fallback: string): string {
  return styles.getPropertyValue(name).trim() || fallback;
}

export function readThemeColors(): ThemeColors {
  if (typeof window === 'undefined' || typeof getComputedStyle !== 'function') {
    return { ...COLORS };
  }
  const s = getComputedStyle(document.documentElement);
  return {
    ...COLORS,
    background: read(s, '--color-background', COLORS.background),
    surface: read(s, '--color-surface', COLORS.surface),
    surfaceElevated: read(s, '--color-surface-elevated', COLORS.surfaceElevated),
    surfaceHighlight: read(s, '--color-surface-highlight', COLORS.surfaceHighlight),
    border: read(s, '--color-border', COLORS.border),
    primary: read(s, '--color-primary', COLORS.primary),
    primaryLight: read(s, '--color-primary-light', COLORS.primaryLight),
    primaryDark: read(s, '--color-primary-dark', COLORS.primaryDark),
    secondary: read(s, '--color-secondary', COLORS.secondary),
    secondaryLight: read(s, '--color-secondary-light', COLORS.secondaryLight),
    secondaryDark: read(s, '--color-secondary-dark', COLORS.secondaryDark),
    tertiary: read(s, '--color-tertiary', COLORS.tertiary),
    tertiaryLight: read(s, '--color-tertiary-light', COLORS.tertiaryLight),
    tertiaryDark: read(s, '--color-tertiary-dark', COLORS.tertiaryDark),
    statusRunning: read(s, '--color-status-running', COLORS.statusRunning),
    statusStopped: read(s, '--color-status-stopped', COLORS.statusStopped),
    statusError: read(s, '--color-status-error', COLORS.statusError),
    statusPending: read(s, '--color-status-pending', COLORS.statusPending),
    textPrimary: read(s, '--color-text-primary', COLORS.textPrimary),
    textSecondary: read(s, '--color-text-secondary', COLORS.textSecondary),
    textMuted: read(s, '--color-text-muted', COLORS.textMuted),
    // External MCP servers share the tertiary (violet) family.
    external: read(s, '--color-tertiary', COLORS.external),
    externalLight: read(s, '--color-tertiary-light', COLORS.externalLight),
    // Edges follow border (default) and primary (animated).
    edgeDefault: read(s, '--color-border', COLORS.edgeDefault),
    edgeAnimated: read(s, '--color-primary', COLORS.edgeAnimated),
    // Transport hues track their semantic accents.
    transportHttp: read(s, '--color-secondary', COLORS.transportHttp),
    transportStdio: read(s, '--color-primary', COLORS.transportStdio),
    transportSse: read(s, '--color-tertiary', COLORS.transportSse),
  };
}
