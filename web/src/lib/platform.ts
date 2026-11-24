// Shortcut tables store these tokens; labels resolve them at render time.
export const MOD_KEY = 'Mod';
export const SHIFT_KEY = 'Shift';

interface NavigatorWithUAData {
  userAgentData?: { platform?: string };
}

/** True on macOS and iOS. Read on every call so tests can stub the platform. */
export function isMacPlatform(): boolean {
  if (typeof navigator === 'undefined') return false;
  const platform =
    (navigator as Navigator & NavigatorWithUAData).userAgentData?.platform ??
    navigator.platform ??
    '';
  return /mac|iphone|ipad|ipod/i.test(platform);
}

export function displayKey(key: string): string {
  if (key === MOD_KEY) return isMacPlatform() ? '⌘' : 'Ctrl';
  if (key === SHIFT_KEY) return isMacPlatform() ? '⇧' : 'Shift';
  return key;
}

/** ⌘⇧R on macOS, Ctrl+Shift+R elsewhere. */
export function formatShortcut(keys: string[]): string {
  return keys.map(displayKey).join(isMacPlatform() ? '' : '+');
}
