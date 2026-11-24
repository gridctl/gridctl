import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, fireEvent, within, waitFor, act } from '@testing-library/react';
import '@testing-library/jest-dom';
import { MemoryRouter, Route, Routes } from 'react-router';
import type { AgentSkill, GatewayStatus, SkillSourceStatus } from '../types';
import type { SharedStatus } from '../hooks/useSharedStatus';

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
    fetchSkillSources: vi.fn(),
    fetchRegistrySkill: vi.fn(),
    validateSkill: vi.fn().mockResolvedValue({ valid: true, errors: [], warnings: [] }),
  };
});

const shared: { current: SharedStatus } = {
  current: { status: null, source: null, settled: false, error: null, refresh: vi.fn() },
};

vi.mock('../hooks/useSharedStatus', () => ({
  useSharedStatus: () => shared.current,
}));

import { DetachedRegistryPage } from '../pages/DetachedRegistryPage';
import { fetchRegistrySkill, fetchRegistrySkills, fetchRegistryStatus, fetchSkillSources } from '../lib/api';
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
  vi.mocked(fetchSkillSources).mockResolvedValue([]);
  shared.current = {
    status: { 'mcp-servers': [] } as unknown as GatewayStatus,
    source: 'self',
    settled: true,
    error: null,
    refresh: vi.fn().mockResolvedValue(undefined),
  };
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

  it('keeps the loading skeleton when the first load is cancelled', async () => {
    vi.mocked(fetchRegistrySkills).mockRejectedValue(
      Object.assign(new Error('aborted'), { name: 'AbortError' }),
    );
    renderPage();
    await waitFor(() => expect(fetchRegistrySkills).toHaveBeenCalled());
    await act(async () => {
      await Promise.resolve();
    });
    expect(within(main()).queryByRole('alert')).not.toBeInTheDocument();
    expect(screen.queryByText('No skills registered')).not.toBeInTheDocument();
    expect(screen.queryByText('Library unavailable')).not.toBeInTheDocument();
    expect(main().querySelector('.animate-pulse')).not.toBeNull();

    vi.mocked(fetchRegistrySkills).mockResolvedValue([SKILL]);
    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));
    expect(await screen.findByText('incident-triage')).toBeInTheDocument();
  });

  it('renders in the frame with its title, a main landmark, and one notifications region', async () => {
    document.title = '';
    vi.mocked(fetchRegistrySkills).mockResolvedValue([]);
    renderPage();
    expect(document.title).toBe('Gridctl - Library');
    expect(screen.getAllByRole('region', { name: 'Notifications' })).toHaveLength(1);
    expect(screen.getAllByRole('main')).toHaveLength(1);
    expect(screen.getByRole('contentinfo')).toHaveTextContent('Detached Window');
    await screen.findByText('No skills registered');
  });

  it('shows the disconnected banner over loaded skills when status fails', async () => {
    shared.current = { ...shared.current, error: 'Status failed: down.' };
    vi.mocked(fetchRegistrySkills).mockResolvedValue([SKILL]);
    renderPage();
    expect(await screen.findByText('incident-triage')).toBeInTheDocument();
    const alerts = screen.getAllByRole('alert');
    expect(alerts).toHaveLength(1);
    expect(alerts[0]).toHaveTextContent('Status failed: down.');
  });

  it('shows one alert when both the registry load and status fail', async () => {
    shared.current = { ...shared.current, status: null, error: 'Status failed: down.' };
    vi.mocked(fetchRegistrySkills).mockRejectedValue(new Error('registry down'));
    renderPage();
    await screen.findByText('Library unavailable');
    expect(screen.getAllByRole('alert')).toHaveLength(1);
    expect(screen.queryByText('Status failed: down.')).not.toBeInTheDocument();
  });

  it('zooms through the Library preference, not the log font size', async () => {
    localStorage.clear();
    vi.mocked(fetchRegistrySkills).mockResolvedValue([]);
    renderPage();
    await screen.findByText('No skills registered');
    fireEvent.click(screen.getByRole('button', { name: 'Increase font size' }));
    expect(localStorage.getItem('gridctl-pref-library-zoom')).toBe('14');
    expect(localStorage.getItem('gridctl-pref-log-font-size')).toBeNull();
    localStorage.clear();
  });

  it('shows provenance in the editor for an imported skill', async () => {
    const source = {
      name: 'team',
      repo: 'https://github.com/example/skills',
      autoUpdate: false,
      updateInterval: '',
      skills: [{ name: 'incident-triage' }],
      updateAvailable: false,
    } as SkillSourceStatus;
    vi.mocked(fetchSkillSources).mockResolvedValue([source]);
    vi.mocked(fetchRegistrySkills).mockResolvedValue([SKILL]);
    vi.mocked(fetchRegistrySkill).mockResolvedValue({ ...SKILL, body: '# Triage' } as AgentSkill);
    renderPage();
    expect(await screen.findByText('incident-triage')).toBeInTheDocument();
    await waitFor(() => expect(fetchSkillSources).toHaveBeenCalled());
    await act(async () => { await Promise.resolve(); });
    fireEvent.click(screen.getByRole('button', { name: /^Edit/ }));
    expect(await screen.findByText(/Tracked from/)).toBeInTheDocument();
  });

  it('still renders the grid when the sources fetch fails', async () => {
    vi.mocked(fetchSkillSources).mockRejectedValue(new Error('sources down'));
    vi.mocked(fetchRegistrySkills).mockResolvedValue([SKILL]);
    renderPage();
    expect(await screen.findByText('incident-triage')).toBeInTheDocument();
    expect(within(main()).queryByRole('alert')).not.toBeInTheDocument();
  });
});
