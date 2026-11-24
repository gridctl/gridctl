import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import '@testing-library/jest-dom';
import { MemoryRouter, Route, Routes } from 'react-router';
import { DetachedTracesPage } from '../pages/DetachedTracesPage';
import { useAuthStore } from '../stores/useAuthStore';

vi.mock('../hooks/useBroadcastChannel', () => ({
  useDetachedWindowSync: vi.fn(),
  useBroadcastChannel: () => ({ postMessage: vi.fn() }),
}));

vi.mock('../lib/api', async (importActual) => {
  const actual = await importActual<typeof import('../lib/api')>();
  return {
    ...actual,
    fetchMCPServers: vi.fn(),
    fetchTraces: vi.fn(),
    fetchTraceDetail: vi.fn(),
    fetchTraceOTLP: vi.fn(),
  };
});

import { fetchMCPServers, fetchTraces } from '../lib/api';

beforeEach(() => {
  localStorage.clear();
  useAuthStore.setState({ authRequired: false });
  vi.mocked(fetchMCPServers).mockResolvedValue([]);
  vi.mocked(fetchTraces).mockResolvedValue({
    traces: [],
    total: 0,
    tracingEnabled: true,
    bufferSize: 0,
    bufferCapacity: 1000,
  } as never);
});

describe('DetachedTracesPage', () => {
  it('renders the notifications region', () => {
    render(
      <MemoryRouter initialEntries={['/traces-window']}>
        <Routes>
          <Route path="/traces-window" element={<DetachedTracesPage />} />
        </Routes>
      </MemoryRouter>,
    );
    expect(screen.getByRole('region', { name: 'Notifications' })).toBeInTheDocument();
  });
});
