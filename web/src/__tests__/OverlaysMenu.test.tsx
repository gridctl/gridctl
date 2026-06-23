import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import { OverlaysMenu } from '../components/graph/OverlaysMenu';
import { useUIStore } from '../stores/useUIStore';

describe('OverlaysMenu', () => {
  beforeEach(() => {
    useUIStore.setState({ showHeatMap: false, showSpecMode: false });
  });

  it('hides the menu until the trigger is clicked', () => {
    render(<OverlaysMenu />);
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();

    fireEvent.click(screen.getByTitle('Overlays'));
    expect(screen.getByRole('menu')).toBeInTheDocument();
    expect(screen.getByText('Token heat map')).toBeInTheDocument();
    expect(screen.getByText('Spec mode')).toBeInTheDocument();
  });

  it('toggles token heat from the menu', () => {
    render(<OverlaysMenu />);
    fireEvent.click(screen.getByTitle('Overlays'));

    const item = screen.getByRole('menuitemcheckbox', { name: /token heat map/i });
    expect(item).toHaveAttribute('aria-checked', 'false');

    fireEvent.click(item);
    expect(useUIStore.getState().showHeatMap).toBe(true);
    expect(item).toHaveAttribute('aria-checked', 'true');
  });

  it('reflects spec mode state via aria-checked', () => {
    useUIStore.setState({ showSpecMode: true });
    render(<OverlaysMenu />);
    fireEvent.click(screen.getByTitle('Overlays'));

    expect(screen.getByRole('menuitemcheckbox', { name: /spec mode/i })).toHaveAttribute(
      'aria-checked',
      'true',
    );
  });

  it('closes the menu on Escape', () => {
    render(<OverlaysMenu />);
    fireEvent.click(screen.getByTitle('Overlays'));
    expect(screen.getByRole('menu')).toBeInTheDocument();

    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
  });
});
