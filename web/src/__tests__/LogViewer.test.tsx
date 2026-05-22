import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react';
import '@testing-library/jest-dom';

vi.mock('../lib/api', () => ({
  fetchServerLogs: vi.fn(),
}));

vi.mock('../lib/constants', () => ({
  POLLING: { LOGS: 2000 },
}));

vi.mock('../components/ui/IconButton', () => ({
  IconButton: ({ onClick, tooltip }: { onClick: () => void; tooltip: string }) => (
    <button onClick={onClick} title={tooltip}>
      {tooltip}
    </button>
  ),
}));

import { LogViewer } from '../components/ui/LogViewer';
import { fetchServerLogs } from '../lib/api';

beforeEach(() => {
  vi.clearAllMocks();
});

describe('LogViewer', () => {
  it('renders agent name in header', () => {
    vi.mocked(fetchServerLogs).mockReturnValue(new Promise(() => {}));
    render(<LogViewer agentName="test-agent" onClose={vi.fn()} />);
    expect(screen.getByText('Logs: test-agent')).toBeInTheDocument();
  });

  it('shows loading state initially', () => {
    vi.mocked(fetchServerLogs).mockReturnValue(new Promise(() => {}));
    render(<LogViewer agentName="test-agent" onClose={vi.fn()} />);
    expect(screen.getByText('Loading logs...')).toBeInTheDocument();
  });

  it('renders log entries after fetch', async () => {
    vi.mocked(fetchServerLogs).mockResolvedValue([
      'INFO starting server',
      'WARN slow query detected',
      'ERROR connection failed',
    ]);

    render(<LogViewer agentName="test-agent" onClose={vi.fn()} />);

    await waitFor(() => {
      expect(screen.getByText('INFO starting server')).toBeInTheDocument();
      expect(screen.getByText('WARN slow query detected')).toBeInTheDocument();
      expect(screen.getByText('ERROR connection failed')).toBeInTheDocument();
    });
  });

  it('shows empty state when no logs', async () => {
    vi.mocked(fetchServerLogs).mockResolvedValue([]);
    render(<LogViewer agentName="test-agent" onClose={vi.fn()} />);

    await waitFor(() => {
      expect(screen.getByText('No logs available')).toBeInTheDocument();
    });
  });

  it('shows error message on fetch failure', async () => {
    vi.mocked(fetchServerLogs).mockRejectedValue(new Error('Network error'));
    render(<LogViewer agentName="test-agent" onClose={vi.fn()} />);

    await waitFor(() => {
      expect(screen.getByText('Error: Network error')).toBeInTheDocument();
    });
  });

  it('calls onClose when close button clicked', async () => {
    vi.mocked(fetchServerLogs).mockResolvedValue([]);
    const onClose = vi.fn();
    render(<LogViewer agentName="test-agent" onClose={onClose} />);

    await waitFor(() => {
      expect(screen.getByText('No logs available')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTitle('Close'));
    expect(onClose).toHaveBeenCalledOnce();
  });

  it('toggles pause/resume', async () => {
    vi.mocked(fetchServerLogs).mockResolvedValue(['log line']);
    render(<LogViewer agentName="test-agent" onClose={vi.fn()} />);

    await waitFor(() => {
      expect(screen.getByText('log line')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTitle('Pause'));
    expect(screen.getByText('Paused')).toBeInTheDocument();
  });

  it('fetches logs with correct agent name and limit', async () => {
    vi.mocked(fetchServerLogs).mockResolvedValue([]);
    render(<LogViewer agentName="my-agent" onClose={vi.fn()} />);

    await waitFor(() => {
      expect(fetchServerLogs).toHaveBeenCalledWith('my-agent', 500);
    });
  });

  it('polls for new logs on interval', async () => {
    vi.useFakeTimers();
    vi.mocked(fetchServerLogs).mockResolvedValue(['initial log']);

    await act(async () => {
      render(<LogViewer agentName="test-agent" onClose={vi.fn()} />);
    });

    // Initial fetch
    expect(fetchServerLogs).toHaveBeenCalledTimes(1);

    // Advance past one polling interval and flush promises
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2000);
    });

    expect(fetchServerLogs).toHaveBeenCalledTimes(2);

    vi.useRealTimers();
  });
});
