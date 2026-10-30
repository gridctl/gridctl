import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import '@testing-library/jest-dom';
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { ToastContainer, showToast } from '../components/ui/Toast';

describe('ToastContainer', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    screen.queryAllByRole('button', { name: 'Dismiss' }).forEach((button) => {
      fireEvent.click(button);
    });
    cleanup();
    vi.useRealTimers();
  });

  it('announces through a polite live region', () => {
    render(<ToastContainer />);
    const region = screen.getByRole('region', { name: 'Notifications' });
    expect(region).toHaveAttribute('aria-live', 'polite');
    expect(region).toHaveAttribute('aria-atomic', 'false');
  });

  it('keeps an error alert until it is dismissed', () => {
    render(<ToastContainer />);
    act(() => {
      showToast('error', 'x');
    });
    expect(screen.getByRole('alert')).toHaveTextContent('x');
    act(() => {
      vi.advanceTimersByTime(10_000);
    });
    expect(screen.getByRole('alert')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Dismiss' }));
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('removes a timed status toast after 3 seconds', () => {
    render(<ToastContainer />);
    act(() => {
      showToast('success', 'y');
    });
    expect(screen.getByRole('status')).toHaveTextContent('y');
    act(() => {
      vi.advanceTimersByTime(2999);
    });
    expect(screen.getByRole('status')).toBeInTheDocument();
    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
  });

  it('persists a success toast that has an action', () => {
    render(<ToastContainer />);
    act(() => {
      showToast('success', 'saved', { action: { label: 'View', onClick: () => undefined } });
    });
    act(() => {
      vi.advanceTimersByTime(10_000);
    });
    expect(screen.getByRole('status')).toHaveTextContent('saved');
    expect(screen.getByRole('button', { name: 'View' })).toHaveAttribute('type', 'button');
  });

  it('pauses a timed toast while the pointer is over it', () => {
    render(<ToastContainer />);
    act(() => {
      showToast('success', 'hold');
    });
    act(() => {
      vi.advanceTimersByTime(2000);
    });
    fireEvent.pointerEnter(screen.getByRole('status'));
    act(() => {
      vi.advanceTimersByTime(2000);
    });
    expect(screen.getByRole('status')).toBeInTheDocument();
    fireEvent.pointerLeave(screen.getByRole('status'));
    act(() => {
      vi.advanceTimersByTime(999);
    });
    expect(screen.getByRole('status')).toBeInTheDocument();
    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
  });

  it('coalesces identical messages and caps distinct errors at five', () => {
    render(<ToastContainer />);
    act(() => {
      for (let i = 0; i < 6; i += 1) showToast('error', 'same');
    });
    expect(screen.getAllByRole('alert')).toHaveLength(1);

    fireEvent.click(screen.getByRole('button', { name: 'Dismiss' }));
    act(() => {
      for (let i = 0; i < 6; i += 1) showToast('error', `err-${i}`);
    });
    expect(screen.getAllByRole('alert')).toHaveLength(5);
    expect(screen.queryByText('err-0')).not.toBeInTheDocument();
    expect(screen.getByText('err-5')).toBeInTheDocument();
  });
});
