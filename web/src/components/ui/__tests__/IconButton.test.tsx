import { describe, it, expect, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, fireEvent } from '@testing-library/react';
import { Trash2 } from 'lucide-react';
import { IconButton } from '../IconButton';

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

  it('leaves the button nameless when neither tooltip nor ariaLabel is given', () => {
    // Deliberate: a nameless icon button is a call-site bug that should stay
    // visible to an audit rather than be masked by a derived name.
    render(<IconButton icon={Trash2} />);
    expect(screen.getByRole('button')).not.toHaveAttribute('aria-label');
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

  it('renders aria-pressed for stateful toggles', () => {
    render(<IconButton icon={Trash2} tooltip="Compact rows" pressed />);
    expect(screen.getByRole('button', { name: 'Compact rows' })).toHaveAttribute('aria-pressed', 'true');
  });
});
