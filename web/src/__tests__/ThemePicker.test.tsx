import { describe, it, expect, beforeEach } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, fireEvent, within, cleanup } from '@testing-library/react';
import { ThemePicker } from '../components/shell/ThemePicker';
import { useUIStore } from '../stores/useUIStore';

describe('ThemePicker', () => {
  beforeEach(() => {
    cleanup();
    useUIStore.setState({ themeMode: 'system' });
    document.documentElement.dataset.theme = 'dark';
  });

  function openPopover() {
    fireEvent.click(screen.getByRole('button', { name: /appearance/i }));
  }

  it('exposes a radiogroup with Light/Dark/System options', () => {
    render(<ThemePicker />);
    openPopover();
    const group = screen.getByRole('radiogroup', { name: /theme/i });
    const radios = within(group).getAllByRole('radio');
    expect(radios).toHaveLength(3);
    expect(within(group).getByRole('radio', { name: 'Light' })).toBeInTheDocument();
    expect(within(group).getByRole('radio', { name: 'Dark' })).toBeInTheDocument();
    expect(within(group).getByRole('radio', { name: 'System' })).toBeInTheDocument();
  });

  it('marks the active mode with aria-checked', () => {
    render(<ThemePicker />);
    openPopover();
    expect(screen.getByRole('radio', { name: 'System' })).toHaveAttribute('aria-checked', 'true');
    expect(screen.getByRole('radio', { name: 'Light' })).toHaveAttribute('aria-checked', 'false');
  });

  it('switching to Light updates the store and aria-checked', () => {
    render(<ThemePicker />);
    openPopover();
    fireEvent.click(screen.getByRole('radio', { name: 'Light' }));
    expect(useUIStore.getState().themeMode).toBe('light');
    expect(screen.getByRole('radio', { name: 'Light' })).toHaveAttribute('aria-checked', 'true');
  });

  it('shows the live system-resolution line only while System is selected', () => {
    render(<ThemePicker />);
    openPopover();
    expect(screen.getByText(/following system/i)).toBeInTheDocument();
    fireEvent.click(screen.getByRole('radio', { name: 'Dark' }));
    expect(screen.queryByText(/following system/i)).not.toBeInTheDocument();
  });

  describe('arrow keys', () => {
    function press(key: string) {
      fireEvent.keyDown(document.activeElement!, { key });
    }

    function expectSelected(label: 'Light' | 'Dark' | 'System') {
      expect(useUIStore.getState().themeMode).toBe(label.toLowerCase());
      const radio = screen.getByRole('radio', { name: label });
      expect(radio).toHaveAttribute('aria-checked', 'true');
      expect(radio).toHaveFocus();
      const tabStops = screen.getAllByRole('radio').filter((r) => r.getAttribute('tabindex') === '0');
      expect(tabStops).toEqual([radio]);
    }

    function startOnLight() {
      useUIStore.setState({ themeMode: 'light' });
      render(<ThemePicker />);
      openPopover();
      screen.getByRole('radio', { name: 'Light' }).focus();
    }

    it('ArrowRight moves focus and selection forward and wraps', () => {
      startOnLight();
      press('ArrowRight');
      expectSelected('Dark');
      press('ArrowRight');
      expectSelected('System');
      press('ArrowRight');
      expectSelected('Light');
    });

    it('ArrowLeft moves focus and selection backward and wraps', () => {
      startOnLight();
      press('ArrowLeft');
      expectSelected('System');
      press('ArrowLeft');
      expectSelected('Dark');
    });

    it('ArrowDown matches ArrowRight and ArrowUp matches ArrowLeft', () => {
      startOnLight();
      press('ArrowDown');
      expectSelected('Dark');
      press('ArrowDown');
      expectSelected('System');
      press('ArrowUp');
      expectSelected('Dark');
      press('ArrowUp');
      expectSelected('Light');
      press('ArrowUp');
      expectSelected('System');
    });
  });
});
