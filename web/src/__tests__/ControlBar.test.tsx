import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react';
import '@testing-library/jest-dom';

const restartMCPServer = vi.fn();
const showToast = vi.fn();
const requestGatewayPoll = vi.fn();

vi.mock('../lib/api', () => ({
  restartMCPServer: (...args: unknown[]) => restartMCPServer(...args),
}));

vi.mock('../components/ui/Toast', () => ({
  showToast: (...args: unknown[]) => showToast(...args),
}));

vi.mock('../hooks/usePolling', () => ({
  requestGatewayPoll: () => requestGatewayPoll(),
}));

import { ControlBar } from '../components/ui/ControlBar';
import { useStackStore } from '../stores/useStackStore';
import { resetRestartsInFlight } from '../lib/restartsInFlight';

function deferred() {
  let resolve!: () => void;
  let reject!: (err: Error) => void;
  const promise = new Promise<void>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

describe('ControlBar', () => {
  beforeEach(() => {
    restartMCPServer.mockReset();
    showToast.mockReset();
    requestGatewayPoll.mockReset();
    resetRestartsInFlight();
    useStackStore.setState({ sessions: 0 });
  });

  it('renders Restart as a secondary button', () => {
    render(<ControlBar name="a" />);
    const button = screen.getByRole('button', { name: 'Restart' });
    expect(button.className).toContain('bg-surface-elevated');
    expect(button.className).not.toContain('from-primary');
  });

  it('restarts on one click with no sessions, then toasts and polls', async () => {
    restartMCPServer.mockResolvedValue(undefined);
    render(<ControlBar name="a" />);
    fireEvent.click(screen.getByRole('button', { name: 'Restart' }));
    await waitFor(() => expect(showToast).toHaveBeenCalledTimes(1));
    expect(restartMCPServer).toHaveBeenCalledTimes(1);
    expect(restartMCPServer).toHaveBeenCalledWith('a');
    expect(showToast.mock.calls[0][0]).toBe('success');
    expect(showToast.mock.calls[0][1]).toContain('a');
    expect(requestGatewayPoll).toHaveBeenCalledTimes(1);
  });

  it('asks for confirmation when gateway sessions are active', async () => {
    restartMCPServer.mockResolvedValue(undefined);
    useStackStore.setState({ sessions: 3 });
    render(<ControlBar name="a" />);
    fireEvent.click(screen.getByRole('button', { name: 'Restart' }));
    expect(restartMCPServer).not.toHaveBeenCalled();
    expect(screen.getByText(/3 gateway sessions are active/)).toHaveTextContent('in-flight calls to a will fail');
    expect(screen.getByText(/3 gateway sessions are active/)).toHaveTextContent('Restart a?');

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(screen.queryByText(/gateway sessions are active/)).not.toBeInTheDocument();
    expect(restartMCPServer).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: 'Restart' }));
    fireEvent.click(screen.getByRole('button', { name: 'Restart' }));
    await waitFor(() => expect(restartMCPServer).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(showToast).toHaveBeenCalledWith('success', 'Restarted a'));
  });

  it('uses the singular for one session', () => {
    useStackStore.setState({ sessions: 1 });
    render(<ControlBar name="a" />);
    fireEvent.click(screen.getByRole('button', { name: 'Restart' }));
    expect(screen.getByText(/1 gateway session is active/)).toBeInTheDocument();
  });

  it('shows an inline alert on failure while mounted, without toast or poll', async () => {
    restartMCPServer.mockRejectedValue(new Error('unknown MCP server'));
    render(<ControlBar name="a" />);
    fireEvent.click(screen.getByRole('button', { name: 'Restart' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('unknown MCP server');
    expect(showToast).not.toHaveBeenCalled();
    expect(requestGatewayPoll).not.toHaveBeenCalled();
  });

  it('keeps an in-flight restart visible to a remounted control', async () => {
    const pending = deferred();
    restartMCPServer.mockReturnValue(pending.promise);
    const first = render(<ControlBar name="a" />);
    fireEvent.click(screen.getByRole('button', { name: 'Restart' }));
    first.unmount();

    render(<ControlBar name="a" />);
    const button = screen.getByRole('button', { name: 'Restarting...' });
    expect(button).toBeDisabled();
    fireEvent.click(button);
    expect(restartMCPServer).toHaveBeenCalledTimes(1);

    await act(async () => {
      pending.resolve();
      await pending.promise;
    });
    expect(screen.getByRole('button', { name: 'Restart' })).toBeEnabled();
    expect(showToast).toHaveBeenCalledWith('success', 'Restarted a');
    expect(requestGatewayPoll).toHaveBeenCalledTimes(1);
  });

  it('toasts a failure that settles after the control unmounted', async () => {
    const pending = deferred();
    restartMCPServer.mockReturnValue(pending.promise);
    const view = render(<ControlBar name="a" />);
    fireEvent.click(screen.getByRole('button', { name: 'Restart' }));
    view.unmount();

    await act(async () => {
      pending.reject(new Error('boom'));
      await pending.promise.catch(() => {});
    });
    await waitFor(() => expect(showToast).toHaveBeenCalledTimes(1));
    expect(showToast.mock.calls[0][0]).toBe('error');
    expect(showToast.mock.calls[0][1]).toContain('a');
    expect(showToast.mock.calls[0][1]).toContain('boom');
    expect(requestGatewayPoll).not.toHaveBeenCalled();
  });
});
