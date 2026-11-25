import { describe, it, expect } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen } from '@testing-library/react';
import { WorkspaceHeader } from '../WorkspaceHeader';

describe('WorkspaceHeader', () => {
  it('renders one visible h1 with the title and id', () => {
    const { container } = render(<WorkspaceHeader title="Metrics" headingId="h-metrics" />);
    const headings = container.querySelectorAll('h1');
    expect(headings).toHaveLength(1);
    const h1 = screen.getByRole('heading', { level: 1, name: 'Metrics' });
    expect(h1).toHaveAttribute('id', 'h-metrics');
    expect(h1.className).not.toMatch(/sr-only/);
    expect(h1.className).not.toMatch(/uppercase/);
    expect(h1.className).not.toMatch(/tracking-/);
    expect(h1).toHaveClass('text-sm', 'font-semibold', 'text-text-primary', 'whitespace-nowrap');
    expect(container.querySelector('header')).toContainElement(h1);
  });

  it('renders the slots when provided', () => {
    render(
      <WorkspaceHeader
        title="Library"
        summary="12 skills"
        switcher={<div data-testid="switcher" />}
        actions={<button type="button">Act</button>}
      >
        <div data-testid="extra-row" />
      </WorkspaceHeader>,
    );
    const summary = screen.getByTestId('workspace-header-summary');
    expect(summary).toHaveTextContent('12 skills');
    expect(summary).toHaveClass('text-xs', 'text-text-muted', 'truncate', 'min-w-0', 'flex-1');
    expect(screen.getByTestId('switcher')).toBeInTheDocument();
    const action = screen.getByRole('button', { name: 'Act' });
    expect(action.parentElement).toHaveClass('ml-auto', 'flex-shrink-0');
    expect(screen.getByTestId('extra-row')).toBeInTheDocument();
  });

  it('omits empty region wrappers', () => {
    const { container } = render(<WorkspaceHeader title="Logs" />);
    const row = container.querySelector('header > div')!;
    expect(row.children).toHaveLength(1);
    expect(row.firstElementChild?.tagName).toBe('H1');
    expect(screen.queryByTestId('workspace-header-summary')).toBeNull();
    expect(container.querySelector('.ml-auto')).toBeNull();
    expect(container.querySelector('header')!.children).toHaveLength(1);
  });

  it('switches the vertical padding with compact', () => {
    const { container, rerender } = render(<WorkspaceHeader title="Pins" />);
    const header = container.querySelector('header')!;
    expect(header).toHaveClass('py-3');
    expect(header).not.toHaveClass('py-2');
    rerender(<WorkspaceHeader title="Pins" compact />);
    expect(header).toHaveClass('py-2');
    expect(header).not.toHaveClass('py-3');
  });

  it('merges className onto the root', () => {
    const { container } = render(<WorkspaceHeader title="Tools" className="extra" />);
    expect(container.querySelector('header')).toHaveClass('extra', 'border-b');
  });
});
