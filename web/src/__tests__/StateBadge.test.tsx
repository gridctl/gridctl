import { describe, it, expect, beforeEach } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, cleanup } from '@testing-library/react';
import { StateBadge } from '../components/registry/StateBadge';

beforeEach(() => {
  cleanup();
});

describe('StateBadge', () => {
  it('renders the state label', () => {
    render(<StateBadge state="active" />);
    expect(screen.getByText('active')).toBeInTheDocument();
  });

  it('applies active color tokens', () => {
    const { container } = render(<StateBadge state="active" />);
    const badge = container.firstChild as HTMLElement;
    expect(badge.className).toContain('text-emerald-400');
    expect(badge.className).toContain('border-emerald-400/25');
  });

  it('applies draft color tokens', () => {
    const { container } = render(<StateBadge state="draft" />);
    const badge = container.firstChild as HTMLElement;
    expect(badge.className).toContain('text-amber-400');
  });

  it('applies disabled color tokens', () => {
    const { container } = render(<StateBadge state="disabled" />);
    const badge = container.firstChild as HTMLElement;
    expect(badge.className).toContain('text-text-muted');
  });

  it('merges an additional className', () => {
    const { container } = render(<StateBadge state="active" className="extra-class" />);
    expect((container.firstChild as HTMLElement).className).toContain('extra-class');
  });
});
