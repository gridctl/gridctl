import { useEffect, useRef } from 'react';
import { useUIStore } from '../stores/useUIStore';
import { useBroadcastChannel } from '../hooks/useBroadcastChannel';
import { applyTheme, subscribeSystemTheme } from './applyTheme';
import { isThemeMode, type ThemeMode } from './types';

/**
 * Drives the live theme from useUIStore.themeMode. Mount once near the route
 * root (AppRoutes) so it covers the main shell and every detached window.
 *
 * - applies the theme to <html> whenever the mode changes (with crossfade);
 * - while in 'system', tracks the OS scheme live;
 * - broadcasts user changes on the shared channel and applies peer changes, so
 *   the main window and all detached popouts stay in sync without a reload.
 *
 * The initial paint is handled by the inline boot script in index.html (anti-
 * FOUC); the first run here re-asserts the same value without animating.
 */
export function useThemeSync(): void {
  const themeMode = useUIStore((s) => s.themeMode);
  const setThemeMode = useUIStore((s) => s.setThemeMode);

  const firstRun = useRef(true);
  const fromPeer = useRef(false);

  const { postMessage } = useBroadcastChannel({
    onMessage: (msg) => {
      if (msg.type !== 'THEME_CHANGE') return;
      const next = (msg.payload as { themeMode?: unknown } | undefined)?.themeMode;
      if (isThemeMode(next) && next !== useUIStore.getState().themeMode) {
        fromPeer.current = true;
        setThemeMode(next as ThemeMode);
      }
    },
  });

  useEffect(() => {
    applyTheme(themeMode, { animate: !firstRun.current });
    if (firstRun.current) {
      firstRun.current = false;
      return;
    }
    if (fromPeer.current) {
      fromPeer.current = false;
      return;
    }
    postMessage({ type: 'THEME_CHANGE', payload: { themeMode }, source: 'main' });
  }, [themeMode, postMessage]);

  useEffect(() => {
    if (themeMode !== 'system') return;
    return subscribeSystemTheme(() => applyTheme('system', { animate: true }));
  }, [themeMode]);
}
