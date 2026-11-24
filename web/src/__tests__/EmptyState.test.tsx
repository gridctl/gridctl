import { describe, it, expect, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, fireEvent } from '@testing-library/react';
import { Layers } from 'lucide-react';
import { EmptyState, FailedState, FailureBanner } from '../components/ui/EmptyState';

describe('EmptyState', () => {
  it('renders title and description', () => {
    render(<EmptyState title="No stacks" description="Create one to begin." />);
    expect(screen.getByText('No stacks')).toBeInTheDocument();
    expect(screen.getByText('Create one to begin.')).toBeInTheDocument();
  });

  it('renders the supplied icon when provided', () => {
    const { container } = render(
      <EmptyState icon={Layers} title="No stacks" />,
    );
    expect(container.querySelector('svg')).toBeInTheDocument();
  });

  it('renders the action slot', () => {
    render(
      <EmptyState
        title="No stacks"
        action={<button>Create</button>}
      />,
    );
    expect(screen.getByRole('button', { name: 'Create' })).toBeInTheDocument();
  });

  it('switches to inline layout when `inline` is set', () => {
    const { container } = render(<EmptyState title="No stacks" inline />);
    const root = container.firstElementChild;
    expect(root?.className).not.toContain('h-full');
    expect(root?.className).toContain('py-8');
  });

  it('renders a failed state with a Retry control', () => {
    const onRetry = vi.fn();
    render(<FailedState feature="Pins" message="Gateway connection unavailable" onRetry={onRetry} />);
    expect(screen.getByRole('alert')).toHaveTextContent('Pins unavailable');
    expect(screen.getByText('Gateway connection unavailable')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect(onRetry).toHaveBeenCalledOnce();
  });

  it('renders a failure banner with Retry and a last updated time', () => {
    const onRetry = vi.fn();
    const at = new Date(2026, 10, 23, 14, 5, 9);
    render(<FailureBanner message="socket closed" onRetry={onRetry} lastUpdated={at} />);
    const alert = screen.getByRole('alert');
    expect(alert).toHaveTextContent('socket closed');
    expect(alert).toHaveTextContent(`Last updated ${at.toLocaleTimeString()}`);
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect(onRetry).toHaveBeenCalledOnce();
  });

  it('accepts epoch milliseconds for lastUpdated', () => {
    const at = new Date(2026, 10, 23, 9, 0, 1).getTime();
    render(<FailureBanner message="down" onRetry={() => {}} lastUpdated={at} />);
    expect(screen.getByRole('alert')).toHaveTextContent(`Last updated ${new Date(at).toLocaleTimeString()}`);
  });

  it.each([
    ['zero', 0],
    ['null', null],
    ['absent', undefined],
  ])('omits the last updated time when lastUpdated is %s', (_label, value) => {
    render(<FailureBanner message="down" onRetry={() => {}} lastUpdated={value} />);
    expect(screen.getByRole('alert')).not.toHaveTextContent('Last updated');
  });

  it('merges a className override over the default margins', () => {
    render(<FailureBanner message="down" onRetry={() => {}} className="mx-2 mt-0" />);
    const cls = screen.getByRole('alert').className;
    expect(cls).toContain('mx-2');
    expect(cls).toContain('mt-0');
    expect(cls).not.toContain('mx-6');
    expect(cls).not.toContain('mt-4');
  });
});
