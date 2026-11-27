import { describe, it, expect, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, fireEvent } from '@testing-library/react';
import { createRef } from 'react';
import { Trash2 } from 'lucide-react';
import { IconButton } from '../IconButton';

const NamelessIconButton = () => (
  // @ts-expect-error a name is required
  <IconButton icon={Trash2} />
);

describe('IconButton', () => {
  it('exposes the tooltip as the accessible name', () => {
    render(<IconButton icon={Trash2} tooltip="Delete skill" />);
    expect(screen.getByRole('button', { name: 'Delete skill' })).toBeInTheDocument();
  });

  it('keeps title alongside aria-label so hover text still works', () => {
    render(<IconButton icon={Trash2} tooltip="Delete skill" />);
    const btn = screen.getByRole('button', { name: 'Delete skill' });
    expect(btn).toHaveAttribute('title', 'Delete skill');
    expect(btn).toHaveAttribute('aria-label', 'Delete skill');
  });

  it('lets an explicit ariaLabel override the tooltip', () => {
    render(<IconButton icon={Trash2} tooltip="Delete" ariaLabel="Delete this skill permanently" />);
    expect(screen.getByRole('button', { name: 'Delete this skill permanently' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Delete this skill permanently' })).toHaveAttribute('title', 'Delete');
  });

  it('renders type="button" so it cannot submit a surrounding form', () => {
    render(<IconButton icon={Trash2} tooltip="Delete skill" />);
    expect(screen.getByRole('button', { name: 'Delete skill' })).toHaveAttribute('type', 'button');
  });

  it('requires a name at the type level', () => {
    expect(NamelessIconButton).toBeTypeOf('function');
  });

  it('hides the icon from assistive tech so it cannot become the name', () => {
    const { container } = render(<IconButton icon={Trash2} tooltip="Delete skill" />);
    expect(container.querySelector('svg')).toHaveAttribute('aria-hidden', 'true');
  });

  it('fires onClick and respects disabled', () => {
    const onClick = vi.fn();
    const { rerender } = render(<IconButton icon={Trash2} tooltip="Delete skill" onClick={onClick} />);
    fireEvent.click(screen.getByRole('button', { name: 'Delete skill' }));
    expect(onClick).toHaveBeenCalledTimes(1);

    rerender(<IconButton icon={Trash2} tooltip="Delete skill" onClick={onClick} disabled />);
    fireEvent.click(screen.getByRole('button', { name: 'Delete skill' }));
    expect(onClick).toHaveBeenCalledTimes(1);
  });

  it('renders size lg as a 44px square and keeps md padding', () => {
    const { rerender } = render(<IconButton icon={Trash2} tooltip="Delete skill" size="lg" />);
    const lg = screen.getByRole('button', { name: 'Delete skill' });
    expect(lg).toHaveClass('size-11');
    expect(lg).toHaveClass('border');

    rerender(<IconButton icon={Trash2} tooltip="Delete skill" size="lg" variant="ghost" />);
    expect(screen.getByRole('button', { name: 'Delete skill' })).toHaveClass('size-11');

    rerender(<IconButton icon={Trash2} tooltip="Delete skill" size="md" />);
    expect(screen.getByRole('button', { name: 'Delete skill' })).toHaveClass('p-2');
  });

  it('spins the glyph, not the button, and marks it busy', () => {
    const { container, rerender } = render(<IconButton icon={Trash2} tooltip="Refresh" spinning />);
    const btn = screen.getByRole('button', { name: 'Refresh' });
    expect(container.querySelector('svg')).toHaveClass('animate-spin');
    expect(btn).not.toHaveClass('animate-spin');
    expect(btn).toHaveAttribute('aria-busy', 'true');

    rerender(<IconButton icon={Trash2} tooltip="Refresh" />);
    expect(container.querySelector('svg')).not.toHaveClass('animate-spin');
    expect(screen.getByRole('button', { name: 'Refresh' })).not.toHaveAttribute('aria-busy');
  });

  it('renders aria-haspopup and aria-expanded only when given', () => {
    const { rerender } = render(<IconButton icon={Trash2} tooltip="More actions" hasPopup="menu" expanded={false} />);
    const btn = screen.getByRole('button', { name: 'More actions' });
    expect(btn).toHaveAttribute('aria-haspopup', 'menu');
    expect(btn).toHaveAttribute('aria-expanded', 'false');

    rerender(<IconButton icon={Trash2} tooltip="More actions" hasPopup="menu" expanded />);
    expect(screen.getByRole('button', { name: 'More actions' })).toHaveAttribute('aria-expanded', 'true');

    rerender(<IconButton icon={Trash2} tooltip="More actions" />);
    expect(screen.getByRole('button', { name: 'More actions' })).not.toHaveAttribute('aria-haspopup');
    expect(screen.getByRole('button', { name: 'More actions' })).not.toHaveAttribute('aria-expanded');
  });

  it('renders aria-expanded from expanded alone and omits it by default', () => {
    const { rerender } = render(<IconButton icon={Trash2} tooltip="Settings" />);
    expect(screen.getByRole('button', { name: 'Settings' })).not.toHaveAttribute('aria-expanded');

    rerender(<IconButton icon={Trash2} tooltip="Settings" expanded />);
    expect(screen.getByRole('button', { name: 'Settings' })).toHaveAttribute('aria-expanded', 'true');

    rerender(<IconButton icon={Trash2} tooltip="Settings" expanded={false} />);
    expect(screen.getByRole('button', { name: 'Settings' })).toHaveAttribute('aria-expanded', 'false');
  });

  it('forwards a ref to the button element', () => {
    const ref = createRef<HTMLButtonElement>();
    render(<IconButton ref={ref} icon={Trash2} tooltip="More actions" hasPopup="menu" />);
    expect(ref.current).toBe(screen.getByRole('button', { name: 'More actions' }));
  });

  it('renders aria-pressed for stateful toggles', () => {
    render(<IconButton icon={Trash2} tooltip="Compact rows" pressed />);
    expect(screen.getByRole('button', { name: 'Compact rows' })).toHaveAttribute('aria-pressed', 'true');
  });
});
