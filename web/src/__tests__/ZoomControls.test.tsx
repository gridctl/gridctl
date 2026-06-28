import { describe, it, expect, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, fireEvent } from '@testing-library/react';
import { ZoomControls } from '../components/ui/ZoomControls';

function setup(overrides = {}) {
  const props = {
    fontSize: 13,
    onZoomIn: vi.fn(),
    onZoomOut: vi.fn(),
    onReset: vi.fn(),
    isMin: false,
    isMax: false,
    isDefault: false,
    ...overrides,
  };
  render(<ZoomControls {...props} />);
  return props;
}

describe('ZoomControls', () => {
  it('shows the current font size in px', () => {
    setup({ fontSize: 16 });
    expect(screen.getByText('16px')).toBeInTheDocument();
  });

  it('calls the handlers for decrease, reset, and increase', () => {
    const props = setup();
    fireEvent.click(screen.getByTitle(/decrease font size/i));
    fireEvent.click(screen.getByTitle(/reset font size/i));
    fireEvent.click(screen.getByTitle(/increase font size/i));
    expect(props.onZoomOut).toHaveBeenCalledTimes(1);
    expect(props.onReset).toHaveBeenCalledTimes(1);
    expect(props.onZoomIn).toHaveBeenCalledTimes(1);
  });

  it('disables decrease at min and increase at max', () => {
    setup({ isMin: true, isMax: true });
    expect(screen.getByTitle(/decrease font size/i)).toBeDisabled();
    expect(screen.getByTitle(/increase font size/i)).toBeDisabled();
  });

  it('disables reset when already at the default size', () => {
    setup({ isDefault: true });
    expect(screen.getByTitle(/reset font size/i)).toBeDisabled();
  });
});
