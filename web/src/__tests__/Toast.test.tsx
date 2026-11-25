import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import '@testing-library/jest-dom';
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { ToastContainer, dismissToast, showToast } from '../components/ui/Toast';
import { useFocusTrap } from '../hooks/useFocusTrap';
import { enterModal } from '../lib/modalPresence';

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

  it('keeps a new timed toast when five persistent toasts are showing', () => {
    render(<ToastContainer />);
    act(() => {
      for (let i = 0; i < 5; i += 1) showToast('error', `err-${i}`);
      showToast('success', 'Copied');
    });
    expect(screen.getAllByRole('alert')).toHaveLength(4);
    expect(screen.queryByText('err-0')).not.toBeInTheDocument();
    expect(screen.getByText('err-4')).toBeInTheDocument();
    expect(screen.getByRole('status')).toHaveTextContent('Copied');
  });

  it('drops the oldest timed toast before a persistent one', () => {
    render(<ToastContainer />);
    act(() => {
      showToast('success', 'first');
      showToast('success', 'second');
      for (let i = 0; i < 3; i += 1) showToast('error', `err-${i}`);
      showToast('warning', 'third');
    });
    expect(screen.queryByText('first')).not.toBeInTheDocument();
    expect(screen.getByText('second')).toBeInTheDocument();
    expect(screen.getByText('third')).toBeInTheDocument();
    expect(screen.getAllByRole('alert')).toHaveLength(3);
  });

  it('keeps a paused toast paused when the same message is shown again', () => {
    render(<ToastContainer />);
    act(() => {
      showToast('success', 'hold');
    });
    act(() => {
      vi.advanceTimersByTime(2000);
    });
    fireEvent.pointerEnter(screen.getByRole('status'));
    act(() => {
      showToast('success', 'hold');
      vi.advanceTimersByTime(10_000);
    });
    expect(screen.getByRole('status')).toBeInTheDocument();
    fireEvent.pointerLeave(screen.getByRole('status'));
    act(() => {
      vi.advanceTimersByTime(2999);
    });
    expect(screen.getByRole('status')).toBeInTheDocument();
    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
  });

  it('honors duration on an action toast', () => {
    render(<ToastContainer />);
    act(() => {
      showToast('warning', 'drifted', { action: { label: 'View', onClick: () => undefined }, duration: 6000 });
    });
    act(() => {
      vi.advanceTimersByTime(5999);
    });
    expect(screen.getByRole('status')).toHaveTextContent('drifted');
    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
  });

  it('keeps an error toast with a duration until it is dismissed', () => {
    render(<ToastContainer />);
    act(() => {
      showToast('error', 'saved but not loaded', { duration: 10000 });
    });
    act(() => {
      vi.advanceTimersByTime(60_000);
    });
    expect(screen.getByRole('alert')).toHaveTextContent('saved but not loaded');
  });

  it('adopts the new action when the same message repeats', () => {
    const first = vi.fn();
    const second = vi.fn();
    render(<ToastContainer />);
    act(() => {
      showToast('warning', 'drift', { action: { label: 'View', onClick: first } });
      showToast('warning', 'drift', { action: { label: 'View', onClick: second } });
    });
    expect(screen.getAllByRole('status')).toHaveLength(1);
    fireEvent.click(screen.getByRole('button', { name: 'View' }));
    expect(first).not.toHaveBeenCalled();
    expect(second).toHaveBeenCalledTimes(1);
  });

  it('replaces the detail when the same message repeats', () => {
    render(<ToastContainer />);
    act(() => {
      showToast('error', 'failed', { detail: 'first detail' });
      showToast('error', 'failed');
    });
    expect(screen.getAllByRole('alert')).toHaveLength(1);
    expect(screen.queryByRole('button', { name: 'Details' })).not.toBeInTheDocument();
  });

  function TrapHarness({ active }: { active: boolean }) {
    const ref = useFocusTrap<HTMLDivElement>({ active, restoreFocus: false });
    return (
      <>
        {active && <div ref={ref} />}
        <ToastContainer />
      </>
    );
  }

  it('holds a deferred toast while a dialog is open', () => {
    const view = render(<TrapHarness active />);
    act(() => {
      showToast('warning', 'held', { duration: 1000, deferWhileModal: true });
      showToast('success', 'shown', { duration: 100_000 });
    });
    expect(screen.queryByText('held')).not.toBeInTheDocument();
    expect(screen.getByText('shown')).toBeInTheDocument();
    act(() => {
      vi.advanceTimersByTime(5000);
    });
    view.rerender(<TrapHarness active={false} />);
    expect(screen.getByText('held')).toBeInTheDocument();
    act(() => {
      vi.advanceTimersByTime(999);
    });
    expect(screen.getByText('held')).toBeInTheDocument();
    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(screen.queryByText('held')).not.toBeInTheDocument();
  });

  it('hides a visible deferred toast when a dialog opens and pauses its timer', () => {
    const view = render(<TrapHarness active={false} />);
    act(() => {
      showToast('warning', 'pinned', { duration: 1000, deferWhileModal: true });
    });
    act(() => {
      vi.advanceTimersByTime(400);
    });
    expect(screen.getByText('pinned')).toBeInTheDocument();
    view.rerender(<TrapHarness active />);
    expect(screen.queryByText('pinned')).not.toBeInTheDocument();
    act(() => {
      vi.advanceTimersByTime(5000);
    });
    view.rerender(<TrapHarness active={false} />);
    expect(screen.getByText('pinned')).toBeInTheDocument();
    act(() => {
      vi.advanceTimersByTime(599);
    });
    expect(screen.getByText('pinned')).toBeInTheDocument();
    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(screen.queryByText('pinned')).not.toBeInTheDocument();
  });

  it('holds a deferred toast raised with no container mounted', () => {
    const release = enterModal();
    let id = '';
    act(() => {
      id = showToast('warning', 'early', { duration: 1000, deferWhileModal: true });
    });
    act(() => {
      vi.advanceTimersByTime(5000);
    });
    render(<ToastContainer />);
    expect(screen.queryByText('early')).not.toBeInTheDocument();
    act(() => release());
    expect(screen.getByText('early')).toBeInTheDocument();
    act(() => dismissToast(id));
  });

  it('shows the detail behind a Details toggle', () => {
    render(<ToastContainer />);
    act(() => {
      showToast('error', 'short', { detail: 'long text' });
    });
    const toggle = screen.getByRole('button', { name: 'Details' });
    expect(toggle).toHaveAttribute('type', 'button');
    expect(toggle).toHaveAttribute('aria-expanded', 'false');
    expect(screen.queryByText('long text')).not.toBeInTheDocument();
    expect(screen.getByText('short')).toHaveAttribute('title', 'long text');
    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByText('long text')).toBeInTheDocument();
    fireEvent.click(toggle);
    expect(screen.queryByText('long text')).not.toBeInTheDocument();
  });

  it('has no Details toggle when the detail is absent or matches the message', () => {
    render(<ToastContainer />);
    act(() => {
      showToast('error', 'same', { detail: 'same' });
      showToast('error', 'plain');
    });
    expect(screen.getAllByRole('alert')).toHaveLength(2);
    expect(screen.queryByRole('button', { name: 'Details' })).not.toBeInTheDocument();
  });
});
