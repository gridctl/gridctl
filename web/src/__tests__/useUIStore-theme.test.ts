import { describe, it, expect, beforeEach } from 'vitest';
import { act } from '@testing-library/react';
import { useUIStore } from '../stores/useUIStore';
import { DEFAULT_THEME_MODE } from '../themes/types';

describe('useUIStore theme slice', () => {
  beforeEach(() => {
    useUIStore.setState({ themeMode: DEFAULT_THEME_MODE });
  });

  it('defaults themeMode to system', () => {
    expect(useUIStore.getState().themeMode).toBe('system');
  });

  it('setThemeMode updates state', () => {
    act(() => useUIStore.getState().setThemeMode('light'));
    expect(useUIStore.getState().themeMode).toBe('light');
    act(() => useUIStore.getState().setThemeMode('dark'));
    expect(useUIStore.getState().themeMode).toBe('dark');
  });

  it('persists themeMode under the partialized state', () => {
    act(() => useUIStore.getState().setThemeMode('light'));
    const raw = localStorage.getItem('gridctl-ui-storage');
    expect(raw).toBeTruthy();
    const persisted = JSON.parse(raw as string);
    expect(persisted.state.themeMode).toBe('light');
  });
});
