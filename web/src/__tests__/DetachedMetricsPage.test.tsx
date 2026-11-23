import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import { MemoryRouter } from 'react-router';
import { DetachedMetricsPage } from '../pages/DetachedMetricsPage';
import type { TokenMetricsResponse } from '../types';

vi.mock('../hooks/useBroadcastChannel', () => ({
  useDetachedWindowSync: vi.fn(),
  useBroadcastChannel: () => ({ postMessage: vi.fn() }),
}));

vi.mock('../lib/api', async (importActual) => {
  const actual = await importActual<typeof import('../lib/api')>();
  return {
    ...actual,
    fetchTokenMetrics: vi.fn(),
    fetchStatus: vi.fn(),
    fetchToolUsage: vi.fn(),
    fetchLimits: vi.fn(),
  };
});

import { fetchLimits, fetchStatus, fetchTokenMetrics, fetchToolUsage } from '../lib/api';

const seeded: TokenMetricsResponse = {
  range: '30m',
  interval: '1m',
  data_points: [
    { timestamp: '2026-01-01T00:00:00Z', input_tokens: 1000, output_tokens: 400, total_tokens: 1400 },
    { timestamp: '2026-01-01T00:01:00Z', input_tokens: 234, output_tokens: 167, total_tokens: 401 },
  ],
  per_server: {},
};

function renderPage() {
  return render(
    <MemoryRouter>
      <DetachedMetricsPage />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(fetchStatus).mockResolvedValue({ gateway: { name: 'g', version: 'v' }, 'mcp-servers': [] } as never);
  vi.mocked(fetchToolUsage).mockResolvedValue({ servers: {} } as never);
  vi.mocked(fetchLimits).mockResolvedValue({ configured: false } as never);
});

describe('DetachedMetricsPage refresh states', () => {
  it('keeps the dashboard under a banner when a refresh fails', async () => {
    vi.mocked(fetchTokenMetrics).mockImplementation((range = '30m') => Promise.resolve({ ...seeded, range }));
    renderPage();
    expect(await screen.findByText('1.8k')).toBeInTheDocument();

    vi.mocked(fetchTokenMetrics).mockRejectedValue(new Error('Gateway connection unavailable'));
    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('Gateway connection unavailable');
    expect(alert).toHaveTextContent('Last updated');
    expect(screen.getByText('1.8k')).toBeInTheDocument();
    expect(screen.queryByText('Metrics unavailable')).not.toBeInTheDocument();

    vi.mocked(fetchTokenMetrics).mockImplementation((range = '30m') => Promise.resolve({ ...seeded, range }));
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    await waitFor(() => {
      expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    });
  });

  it('shows the failed state when the first load fails', async () => {
    vi.mocked(fetchTokenMetrics).mockRejectedValue(new Error('Gateway connection unavailable'));
    renderPage();
    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('Metrics unavailable');
    expect(alert).toHaveTextContent('Gateway connection unavailable');
    expect(screen.getByRole('button', { name: 'Retry' })).toBeInTheDocument();
  });
});
