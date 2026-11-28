import { describe, it, expect, beforeEach, vi } from 'vitest';
import { act, render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import '@testing-library/jest-dom';
import { MetricsControls } from '../components/metrics/MetricsControls';
import { HTTPError } from '../lib/api';

vi.mock('../components/ui/Toast', () => ({ showToast: vi.fn() }));

import { showToast } from '../components/ui/Toast';

function deferred<T>() {
  let resolve!: (v: T) => void;
  const promise = new Promise<T>((r) => { resolve = r; });
  return { promise, resolve };
}

function renderControls(over: Partial<Parameters<typeof MetricsControls>[0]> = {}) {
  const props = {
    timeRange: 'live' as const,
    onTimeRange: vi.fn(),
    isPaused: false,
    onTogglePause: vi.fn(),
    onRefresh: vi.fn(() => Promise.resolve(true)),
    onClear: vi.fn(() => Promise.resolve()),
    ...over,
  };
  render(<MetricsControls {...props} />);
  return props;
}

async function confirmClear() {
  fireEvent.click(screen.getByTitle('Clear Metrics'));
  const dialog = await screen.findByRole('alertdialog', { name: /clear all metrics/i });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Clear' }));
}

const refreshIcon = () => screen.getByTitle('Refresh').querySelector('svg');
const statusRegion = () => screen.getByRole('status');

beforeEach(() => {
  vi.clearAllMocks();
});

describe('MetricsControls clear', () => {
  it('toasts a failed clear with one "Clear metrics failed" prefix', async () => {
    renderControls({ onClear: vi.fn(() => Promise.reject(new HTTPError(500, '500 Internal Server Error'))) });
    await confirmClear();
    await waitFor(() => expect(showToast).toHaveBeenCalledTimes(1));
    const [type, text] = vi.mocked(showToast).mock.calls[0];
    expect(type).toBe('error');
    expect(text.startsWith('Clear metrics failed')).toBe(true);
    expect(text.split('Clear metrics failed').length).toBe(2);
  });

  it('confirms a successful clear', async () => {
    renderControls();
    await confirmClear();
    await waitFor(() => expect(showToast).toHaveBeenCalledWith('success', 'Metrics cleared'));
  });

  it('raises no toast for a cancelled clear', async () => {
    const onClear = vi.fn(() => Promise.reject(new DOMException('aborted', 'AbortError')));
    renderControls({ onClear });
    await confirmClear();
    await waitFor(() => expect(onClear).toHaveBeenCalled());
    await act(async () => {});
    expect(showToast).not.toHaveBeenCalled();
  });

  it('ignores a second confirm while a clear is pending', async () => {
    const pending = deferred<void>();
    const onClear = vi.fn(() => pending.promise);
    renderControls({ onClear });
    await confirmClear();
    await confirmClear();
    expect(onClear).toHaveBeenCalledTimes(1);
    await act(async () => { pending.resolve(); });
    await waitFor(() => expect(showToast).toHaveBeenCalledTimes(1));
    expect(showToast).toHaveBeenCalledWith('success', 'Metrics cleared');
  });
});

describe('MetricsControls refresh', () => {
  it('spins while pending, ignores repeat clicks, and announces success after settling', async () => {
    const pending = deferred<boolean>();
    const onRefresh = vi.fn(() => pending.promise);
    renderControls({ onRefresh });
    expect(statusRegion()).toHaveTextContent('');

    const button = screen.getByTitle('Refresh');
    button.focus();
    fireEvent.click(button);
    expect(refreshIcon()).toHaveClass('animate-spin');
    expect(statusRegion()).toHaveTextContent('');
    fireEvent.click(button);
    expect(onRefresh).toHaveBeenCalledTimes(1);
    expect(button).not.toBeDisabled();
    expect(button).toHaveFocus();

    await act(async () => { pending.resolve(true); });
    expect(statusRegion()).toHaveTextContent('Metrics refreshed');
    expect(refreshIcon()).not.toHaveClass('animate-spin');
  });

  it('announces a failed refresh', async () => {
    const pending = deferred<boolean>();
    renderControls({ onRefresh: vi.fn(() => pending.promise) });
    fireEvent.click(screen.getByTitle('Refresh'));
    await act(async () => { pending.resolve(false); });
    expect(statusRegion()).toHaveTextContent('Metrics refresh failed');
  });

  it('announces a rejected refresh as failed and clears the text on a repeat', async () => {
    const onRefresh = vi.fn(() => Promise.reject(new Error('boom')));
    renderControls({ onRefresh });
    fireEvent.click(screen.getByTitle('Refresh'));
    await waitFor(() => expect(statusRegion()).toHaveTextContent('Metrics refresh failed'));

    const pending = deferred<boolean>();
    onRefresh.mockImplementationOnce(() => pending.promise as never);
    fireEvent.click(screen.getByTitle('Refresh'));
    expect(statusRegion()).toHaveTextContent('');
    await act(async () => { pending.resolve(true); });
    expect(statusRegion()).toHaveTextContent('Metrics refreshed');
  });
});
