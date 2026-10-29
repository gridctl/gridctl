import { describe, it, expect, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, fireEvent } from '@testing-library/react';
import { Layers } from 'lucide-react';
import { EmptyState, FailedState } from '../components/ui/EmptyState';

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
});
