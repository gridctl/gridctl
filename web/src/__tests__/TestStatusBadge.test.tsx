import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, cleanup, fireEvent } from '@testing-library/react';
import { TestStatusBadge } from '../components/registry/TestStatusBadge';
import type { SkillTestResult } from '../types';

beforeEach(() => {
  cleanup();
});

function result(overrides: Partial<SkillTestResult> = {}): SkillTestResult {
  return {
    status: 'tested',
    passed: 0,
    failed: 0,
    skipped: 0,
    results: [],
    executedAt: '2025-01-01T00:00:00Z',
    ...overrides,
  } as SkillTestResult;
}

describe('TestStatusBadge', () => {
  it('renders untested state when testResult is null', () => {
    render(<TestStatusBadge testResult={null} />);
    expect(screen.getByText('untested')).toBeInTheDocument();
  });

  it('renders failing state with count in card density', () => {
    render(<TestStatusBadge testResult={result({ passed: 2, failed: 3 })} density="card" />);
    expect(screen.getByText('3 failing')).toBeInTheDocument();
  });

  it('renders passing state with count in card density', () => {
    render(<TestStatusBadge testResult={result({ passed: 5, failed: 0 })} density="card" />);
    expect(screen.getByText('5 passing')).toBeInTheDocument();
  });

  it('renders compact labels without counts', () => {
    render(<TestStatusBadge testResult={result({ passed: 5, failed: 0 })} density="compact" />);
    expect(screen.getByText('passing')).toBeInTheDocument();
    expect(screen.queryByText(/5 passing/)).not.toBeInTheDocument();
  });

  it('renders as a button when onClick is provided', () => {
    const onClick = vi.fn();
    render(<TestStatusBadge testResult={null} onClick={onClick} />);
    const btn = screen.getByRole('button');
    fireEvent.click(btn);
    expect(onClick).toHaveBeenCalledTimes(1);
  });

  it('renders as a passive span when onClick is absent', () => {
    const { container } = render(<TestStatusBadge testResult={null} />);
    expect(container.querySelector('button')).toBeNull();
    expect(container.querySelector('span')).not.toBeNull();
  });

  it('stops click propagation when clicked', () => {
    const outer = vi.fn();
    const inner = vi.fn();
    render(
      <div onClick={outer}>
        <TestStatusBadge testResult={null} onClick={inner} />
      </div>,
    );
    fireEvent.click(screen.getByRole('button'));
    expect(inner).toHaveBeenCalledTimes(1);
    expect(outer).not.toHaveBeenCalled();
  });
});
