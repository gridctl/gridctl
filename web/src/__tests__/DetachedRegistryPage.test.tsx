import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, fireEvent, within } from '@testing-library/react';
import '@testing-library/jest-dom';
import { MemoryRouter, Route, Routes } from 'react-router';
import type { AgentSkill } from '../types';

vi.mock('../hooks/useBroadcastChannel', () => ({
  useDetachedWindowSync: vi.fn(),
  useBroadcastChannel: () => ({ postMessage: vi.fn() }),
}));

vi.mock('../lib/api', async (importActual) => {
  const actual = await importActual<typeof import('../lib/api')>();
  return {
    ...actual,
    fetchRegistryStatus: vi.fn(),
    fetchRegistrySkills: vi.fn(),
  };
});

import { DetachedRegistryPage } from '../pages/DetachedRegistryPage';
import { fetchRegistrySkills, fetchRegistryStatus } from '../lib/api';
import { useAuthStore } from '../stores/useAuthStore';

const SKILL: AgentSkill = {
  name: 'incident-triage',
  description: 'Triage incidents quickly',
  state: 'active',
  fileCount: 1,
  dir: 'ops/incident-triage',
} as AgentSkill;

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/library-window']}>
      <Routes>
        <Route path="/library-window" element={<DetachedRegistryPage />} />
      </Routes>
    </MemoryRouter>,
  );
}

const main = () => screen.getByRole('main');

beforeEach(() => {
  useAuthStore.setState({ authRequired: false });
  vi.mocked(fetchRegistryStatus).mockResolvedValue({ totalSkills: 0, activeSkills: 0 });
});

afterEach(() => {
  vi.clearAllMocks();
});

describe('DetachedRegistryPage', () => {
  it('renders the notifications region', async () => {
    vi.mocked(fetchRegistrySkills).mockResolvedValue([]);
    renderPage();
    expect(screen.getByRole('region', { name: 'Notifications' })).toBeInTheDocument();
    await screen.findByText('No skills registered');
  });

  it('shows Library unavailable with a working Retry when the first load fails', async () => {
    vi.mocked(fetchRegistrySkills).mockRejectedValue(new Error('registry down'));
    renderPage();
    const alert = await within(main()).findByRole('alert');
    expect(alert).toHaveTextContent('Library unavailable');
    expect(alert).toHaveTextContent('registry down');
    expect(screen.queryByText('No skills registered')).not.toBeInTheDocument();

    vi.mocked(fetchRegistrySkills).mockResolvedValue([SKILL]);
    fireEvent.click(within(alert).getByRole('button', { name: 'Retry' }));
    expect(await screen.findByText('incident-triage')).toBeInTheDocument();
    expect(within(main()).queryByRole('alert')).not.toBeInTheDocument();
    expect(screen.queryByText('Library unavailable')).not.toBeInTheDocument();
  });

  it('shows No skills registered for a loaded empty registry', async () => {
    vi.mocked(fetchRegistrySkills).mockResolvedValue([]);
    renderPage();
    expect(await screen.findByText('No skills registered')).toBeInTheDocument();
    expect(within(main()).queryByRole('alert')).not.toBeInTheDocument();
  });

  it('keeps loaded skills under a failure row when a refresh fails', async () => {
    vi.mocked(fetchRegistrySkills).mockResolvedValue([SKILL]);
    renderPage();
    expect(await screen.findByText('incident-triage')).toBeInTheDocument();

    vi.mocked(fetchRegistrySkills).mockRejectedValue(new Error('registry down'));
    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));
    const alert = await within(main()).findByRole('alert');
    expect(alert).toHaveTextContent('registry down');
    expect(within(alert).getByRole('button', { name: 'Retry' })).toBeInTheDocument();
    expect(screen.getByText('incident-triage')).toBeInTheDocument();
    expect(screen.queryByText('Library unavailable')).not.toBeInTheDocument();
    expect(screen.queryByText('No skills registered')).not.toBeInTheDocument();
  });
});
