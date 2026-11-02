import { describe, expect, it, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, fireEvent } from '@testing-library/react';
import { Switch } from '../components/ui/Switch';
import { Badge } from '../components/ui/Badge';

describe('Switch', () => {
  it('exposes a switch and reports the next value', () => {
    const onChange = vi.fn();
    render(<Switch checked={false} onChange={onChange} label="Library" />);
    const control = screen.getByRole('switch', { name: 'Library' });
    expect(control).toHaveAttribute('aria-checked', 'false');
    fireEvent.click(control);
    expect(onChange).toHaveBeenCalledWith(true);
  });

  it('does not toggle while disabled', () => {
    const onChange = vi.fn();
    render(<Switch checked={false} onChange={onChange} label="Library" disabled />);
    fireEvent.click(screen.getByRole('switch', { name: 'Library' }));
    expect(onChange).not.toHaveBeenCalled();
  });
});

describe('Badge tones', () => {
  it('renders a dot-free warning pill with the pending token', () => {
    render(<Badge tone="warning">Drifted</Badge>);
    const pill = screen.getByText('Drifted');
    expect(pill.className).toContain('status-pending');
    expect(pill.querySelector('span')).toBeNull();
  });
});
