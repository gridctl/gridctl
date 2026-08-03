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
    expect(badge.className).toContain('text-status-running');
    expect(badge.className).toContain('border-status-running/25');
  });

  it('applies draft color tokens', () => {
    const { container } = render(<StateBadge state="draft" />);
    const badge = container.firstChild as HTMLElement;
    expect(badge.className).toContain('text-status-pending');
  });

  // The badge must never carry a raw palette class: those do not re-key per
  // theme, which is how the light theme ended up with illegible amber text.
  it('uses semantic tokens rather than raw palette classes', () => {
    for (const state of ['active', 'draft', 'disabled'] as const) {
      const { container } = render(<StateBadge state={state} />);
      expect((container.firstChild as HTMLElement).className).not.toMatch(
        /\b(?:text|bg|border)-(?:amber|yellow|emerald|rose|violet|sky|teal|red|green|blue)-\d{2,3}\b/,
      );
      cleanup();
    }
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
