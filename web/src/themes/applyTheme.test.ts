import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import {
  applyTheme,
  resolveTheme,
  getResolvedTheme,
  THEME_CHANGE_EVENT,
} from './applyTheme';
import { isThemeMode } from './types';

describe('resolveTheme', () => {
  it('returns explicit modes unchanged', () => {
    expect(resolveTheme('light')).toBe('light');
    expect(resolveTheme('dark')).toBe('dark');
  });

  it('resolves system via matchMedia (falls back to light when unavailable)', () => {
    // jsdom has no matchMedia by default → systemPrefersDark() is false → light.
    expect(resolveTheme('system')).toBe('light');
  });

  it('resolves system to dark when the OS prefers dark', () => {
    // jsdom has no matchMedia; stub it for this case then remove.
    const original = window.matchMedia;
    window.matchMedia = vi.fn().mockReturnValue({
      matches: true,
    }) as unknown as typeof window.matchMedia;
    expect(resolveTheme('system')).toBe('dark');
    if (original) window.matchMedia = original;
    else delete (window as { matchMedia?: unknown }).matchMedia;
  });
});

describe('applyTheme', () => {
  beforeEach(() => {
    delete document.documentElement.dataset.theme;
    document.documentElement.style.colorScheme = '';
  });

  it('sets data-theme and color-scheme on <html>', () => {
    applyTheme('light');
    expect(document.documentElement.dataset.theme).toBe('light');
    expect(document.documentElement.style.colorScheme).toBe('light');

    applyTheme('dark');
    expect(document.documentElement.dataset.theme).toBe('dark');
    expect(document.documentElement.style.colorScheme).toBe('dark');
  });

  it('returns the resolved theme', () => {
    expect(applyTheme('dark')).toBe('dark');
  });

  it('dispatches the theme-change event with mode + resolved', () => {
    const handler = vi.fn();
    window.addEventListener(THEME_CHANGE_EVENT, handler);
    applyTheme('light');
    expect(handler).toHaveBeenCalledTimes(1);
    const detail = (handler.mock.calls[0][0] as CustomEvent).detail;
    expect(detail).toEqual({ mode: 'light', resolved: 'light' });
    window.removeEventListener(THEME_CHANGE_EVENT, handler);
  });
});

describe('getResolvedTheme', () => {
  afterEach(() => {
    delete document.documentElement.dataset.theme;
  });

  it('reads light from the DOM, defaulting to dark', () => {
    document.documentElement.dataset.theme = 'light';
    expect(getResolvedTheme()).toBe('light');
    document.documentElement.dataset.theme = 'dark';
    expect(getResolvedTheme()).toBe('dark');
    delete document.documentElement.dataset.theme;
    expect(getResolvedTheme()).toBe('dark');
  });
});

describe('isThemeMode', () => {
  it('accepts valid modes and rejects others', () => {
    expect(isThemeMode('light')).toBe(true);
    expect(isThemeMode('dark')).toBe(true);
    expect(isThemeMode('system')).toBe(true);
    expect(isThemeMode('blue')).toBe(false);
    expect(isThemeMode(undefined)).toBe(false);
    expect(isThemeMode(null)).toBe(false);
  });
});
