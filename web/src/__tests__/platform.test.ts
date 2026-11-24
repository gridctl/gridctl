import { afterEach, describe, expect, it, vi } from 'vitest';
import { displayKey, formatShortcut, isMacPlatform, MOD_KEY, SHIFT_KEY } from '../lib/platform';

function stubPlatform(value: string) {
  vi.spyOn(window.navigator, 'platform', 'get').mockReturnValue(value);
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe('isMacPlatform', () => {
  it('is false for the default test platform', () => {
    expect(isMacPlatform()).toBe(false);
  });

  it('is true for MacIntel and iPhone', () => {
    stubPlatform('MacIntel');
    expect(isMacPlatform()).toBe(true);
    stubPlatform('iPhone');
    expect(isMacPlatform()).toBe(true);
  });

  it('is false for Windows and Linux', () => {
    stubPlatform('Win32');
    expect(isMacPlatform()).toBe(false);
    stubPlatform('Linux x86_64');
    expect(isMacPlatform()).toBe(false);
  });

  it('prefers userAgentData.platform when present', () => {
    stubPlatform('Win32');
    Object.defineProperty(window.navigator, 'userAgentData', {
      value: { platform: 'macOS' },
      configurable: true,
    });
    try {
      expect(isMacPlatform()).toBe(true);
    } finally {
      delete (window.navigator as { userAgentData?: unknown }).userAgentData;
    }
  });

  it('falls back to navigator.platform when userAgentData.platform is empty', () => {
    stubPlatform('MacIntel');
    Object.defineProperty(window.navigator, 'userAgentData', {
      value: { platform: '' },
      configurable: true,
    });
    try {
      expect(isMacPlatform()).toBe(true);
    } finally {
      delete (window.navigator as { userAgentData?: unknown }).userAgentData;
    }
  });
});

describe('displayKey and formatShortcut', () => {
  it('render Ctrl and Shift off macOS', () => {
    expect(displayKey(MOD_KEY)).toBe('Ctrl');
    expect(displayKey(SHIFT_KEY)).toBe('Shift');
    expect(displayKey('K')).toBe('K');
    expect(formatShortcut([MOD_KEY, 'K'])).toBe('Ctrl+K');
    expect(formatShortcut([MOD_KEY, SHIFT_KEY, 'R'])).toBe('Ctrl+Shift+R');
  });

  it('render ⌘ and ⇧ on macOS', () => {
    stubPlatform('MacIntel');
    expect(displayKey(MOD_KEY)).toBe('⌘');
    expect(displayKey(SHIFT_KEY)).toBe('⇧');
    expect(displayKey('Enter')).toBe('Enter');
    expect(formatShortcut([MOD_KEY, 'K'])).toBe('⌘K');
    expect(formatShortcut([MOD_KEY, SHIFT_KEY, 'R'])).toBe('⌘⇧R');
  });
});
