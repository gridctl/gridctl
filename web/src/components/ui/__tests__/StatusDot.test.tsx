import { describe, it, expect } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen } from '@testing-library/react';
import { StatusDot, STATUS_SHAPES } from '../StatusDot';
import { NODE_STATUS_LABELS } from '../../../lib/nodeStatus';
import type { NodeStatus } from '../../../types';

const STATUSES = Object.keys(NODE_STATUS_LABELS) as NodeStatus[];

describe('StatusDot', () => {
  it('names running and needs-auth and exposes a title', () => {
    const { rerender } = render(<StatusDot status="running" />);
    const running = screen.getByRole('img', { name: 'running' });
    expect(running).toHaveAttribute('title', 'running');

    rerender(<StatusDot status="needs-auth" />);
    const needsAuth = screen.getByRole('img', { name: 'needs auth' });
    expect(needsAuth).toHaveAttribute('title', 'needs auth');
  });

  it('gives every status its own shape', () => {
    expect(Object.keys(STATUS_SHAPES).sort()).toEqual([...STATUSES].sort());
    expect(new Set(Object.values(STATUS_SHAPES)).size).toBe(STATUSES.length);
  });

  it.each(STATUSES)('renders the %s shape with its label', (status) => {
    render(<StatusDot status={status} />);
    const dot = screen.getByRole('img', { name: NODE_STATUS_LABELS[status] });
    expect(dot).toHaveAttribute('data-shape', STATUS_SHAPES[status]);
    expect(dot).toHaveAttribute('title', NODE_STATUS_LABELS[status]);
  });

  it('uses the label override for the name and title', () => {
    render(<StatusDot status="stopped" label="disabled" />);
    const dot = screen.getByRole('img', { name: 'disabled' });
    expect(dot).toHaveAttribute('title', 'disabled');
    expect(dot).toHaveAttribute('data-shape', STATUS_SHAPES.stopped);
  });

  it('pulses the error shape only when pulse is on', () => {
    const { rerender } = render(<StatusDot status="error" />);
    const shape = () => screen.getByRole('img', { name: 'error' }).querySelector('svg');
    expect(shape()).toHaveClass('animate-pulse', 'motion-reduce:animate-none');

    rerender(<StatusDot status="error" pulse={false} />);
    expect(shape()).not.toHaveClass('animate-pulse');
  });

  it.each(STATUSES)('does not animate %s when pulse is false', (status) => {
    const { container } = render(<StatusDot status={status} pulse={false} />);
    expect(container.querySelector('[class*="animate-"]')).toBeNull();
  });
});
