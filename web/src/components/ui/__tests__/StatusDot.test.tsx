import { describe, it, expect } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen } from '@testing-library/react';
import { StatusDot } from '../StatusDot';

describe('StatusDot', () => {
  it('names running and needs-auth and exposes a title', () => {
    const { rerender } = render(<StatusDot status="running" />);
    const running = screen.getByRole('img', { name: 'running' });
    expect(running).toHaveAttribute('title', 'running');

    rerender(<StatusDot status="needs-auth" />);
    const needsAuth = screen.getByRole('img', { name: 'needs auth' });
    expect(needsAuth).toHaveAttribute('title', 'needs auth');
  });

  it('keeps running and error distinct under reduced motion by class', () => {
    const { rerender } = render(<StatusDot status="running" />);
    const runningDot = screen.getByRole('img', { name: 'running' }).querySelector('span');
    expect(runningDot?.className).toContain('motion-reduce:ring-1');
    expect(runningDot?.className).toContain('motion-reduce:ring-status-running');

    rerender(<StatusDot status="error" />);
    const errorDot = screen.getByRole('img', { name: 'error' }).querySelector('span');
    expect(errorDot?.className).toContain('motion-reduce:bg-transparent');
    expect(errorDot?.className).toContain('motion-reduce:ring-2');
    expect(errorDot?.className).toContain('motion-reduce:ring-status-error');
  });
});