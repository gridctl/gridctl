// Theme picker types. `mode` is what the user selects and we persist; `system`
// resolves to a concrete theme via prefers-color-scheme at apply time.
export type ThemeMode = 'light' | 'dark' | 'system';
export type ResolvedTheme = 'light' | 'dark';

export const THEME_MODES: ThemeMode[] = ['light', 'dark', 'system'];

export const DEFAULT_THEME_MODE: ThemeMode = 'system';

export function isThemeMode(v: unknown): v is ThemeMode {
  return v === 'light' || v === 'dark' || v === 'system';
}
