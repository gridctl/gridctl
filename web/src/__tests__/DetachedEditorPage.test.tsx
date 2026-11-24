import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { DetachedEditorPage } from '../pages/DetachedEditorPage';
import { fetchRegistrySkill, fetchSkillSources, updateRegistrySkill } from '../lib/api';
import { useSharedStatus } from '../hooks/useSharedStatus';
import { useAuthStore } from '../stores/useAuthStore';
import type { AgentSkill, SkillSourceStatus } from '../types';

vi.mock('../hooks/useBroadcastChannel', () => ({
  useDetachedWindowSync: vi.fn(),
}));

vi.mock('../lib/api', () => ({
  fetchRegistrySkill: vi.fn(),
  fetchRegistryStatus: vi.fn().mockResolvedValue({ totalSkills: 1, activeSkills: 1 }),
  fetchRegistrySkills: vi.fn().mockResolvedValue([]),
  updateRegistrySkill: vi.fn().mockResolvedValue({}),
  createRegistrySkill: vi.fn().mockResolvedValue({}),
  validateSkill: vi.fn().mockResolvedValue({ valid: true, errors: [], warnings: [] }),
  resetSkill: vi.fn().mockResolvedValue({}),
  detachSkill: vi.fn().mockResolvedValue({}),
  fetchSkillSources: vi.fn().mockResolvedValue([]),
}));

vi.mock('../hooks/useSharedStatus', () => ({
  useSharedStatus: vi.fn(),
}));

const SKILL: AgentSkill = {
  name: 'incident-triage',
  description: 'Triage incidents quickly',
  state: 'active',
  fileCount: 2,
  dir: 'ops/incident-triage',
  body: '# Triage\n\nReal instructions.',
};

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/editor?type=skill&name=incident-triage']}>
      <Routes>
        <Route path="/editor" element={<DetachedEditorPage />} />
      </Routes>
    </MemoryRouter>,
  );
}

const bodyField = () => screen.getByRole('textbox', { name: 'Skill instructions' });

// jsdom's window.close() does not fire beforeunload. The browser does, and it
// does so before close() returns. The spy reproduces that order.
function closeThatUnloads() {
  const prevented: boolean[] = [];
  const close = vi.spyOn(window, 'close').mockImplementation(() => {
    const event = new Event('beforeunload', { cancelable: true });
    window.dispatchEvent(event);
    prevented.push(event.defaultPrevented);
  });
  return { close, prevented };
}

describe('DetachedEditorPage unload guard', () => {
  beforeEach(() => {
    useAuthStore.setState({ authRequired: false });
    vi.mocked(fetchRegistrySkill).mockResolvedValue(SKILL);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('does not prompt to leave when discard closes the window', async () => {
    renderPage();
    await waitFor(() => expect(bodyField()).toHaveValue('# Triage\n\nReal instructions.'));
    expect(screen.getByRole('region', { name: 'Notifications' })).toBeInTheDocument();

    fireEvent.change(bodyField(), { target: { value: '# Edited\n' } });
    const dirty = new Event('beforeunload', { cancelable: true });
    window.dispatchEvent(dirty);
    expect(dirty.defaultPrevented).toBe(true);

    const { close, prevented } = closeThatUnloads();
    fireEvent.keyDown(document, { key: 'Escape' });
    fireEvent.click(screen.getByRole('button', { name: 'Discard' }));

    expect(close).toHaveBeenCalledTimes(1);
    expect(prevented).toEqual([false]);
  });

  it('does not prompt to leave when save closes the window', async () => {
    renderPage();
    await waitFor(() => expect(bodyField()).toHaveValue('# Triage\n\nReal instructions.'));

    fireEvent.change(bodyField(), { target: { value: '# Edited\n' } });
    const dirty = new Event('beforeunload', { cancelable: true });
    window.dispatchEvent(dirty);
    expect(dirty.defaultPrevented).toBe(true);

    const { close, prevented } = closeThatUnloads();
    fireEvent.click(screen.getByRole('button', { name: /^save$/i }));

    await waitFor(() => expect(updateRegistrySkill).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(close).toHaveBeenCalledTimes(1));
    expect(prevented).toEqual([false]);
  });
});

describe('DetachedEditorPage frame', () => {
  beforeEach(() => {
    useAuthStore.setState({ authRequired: false });
    vi.mocked(fetchRegistrySkill).mockResolvedValue(SKILL);
    vi.mocked(fetchSkillSources).mockResolvedValue([]);
    document.title = '';
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  it('renders one notifications region while loading and titles the window with the skill name', async () => {
    let resolve: (s: AgentSkill) => void = () => {};
    vi.mocked(fetchRegistrySkill).mockReturnValue(new Promise((r) => { resolve = r; }));
    renderPage();
    expect(screen.getByText('Loading skill...')).toBeInTheDocument();
    expect(screen.getAllByRole('region', { name: 'Notifications' })).toHaveLength(1);
    expect(document.title).toBe('Gridctl - Editor: incident-triage');
    resolve(SKILL);
    await waitFor(() => expect(bodyField()).toBeInTheDocument());
    expect(screen.getAllByRole('region', { name: 'Notifications' })).toHaveLength(1);
  });

  it('renders the notifications region in the error state', async () => {
    vi.mocked(fetchRegistrySkill).mockRejectedValue(new Error('skill missing'));
    renderPage();
    expect(await screen.findByText('skill missing')).toBeInTheDocument();
    expect(screen.getAllByRole('region', { name: 'Notifications' })).toHaveLength(1);
  });

  it('renders no footer and does not poll status', async () => {
    renderPage();
    await waitFor(() => expect(bodyField()).toBeInTheDocument());
    expect(screen.queryByRole('contentinfo')).not.toBeInTheDocument();
    expect(screen.queryByText('Detached Editor')).not.toBeInTheDocument();
    expect(useSharedStatus).not.toHaveBeenCalled();
  });

  it('shows provenance for an imported skill', async () => {
    const source: SkillSourceStatus = {
      name: 'team',
      repo: 'https://github.com/example/skills',
      autoUpdate: false,
      updateInterval: '',
      skills: [{ name: 'incident-triage' }],
      updateAvailable: false,
    } as SkillSourceStatus;
    vi.mocked(fetchSkillSources).mockResolvedValue([source]);
    renderPage();
    expect(await screen.findByText(/Tracked from/)).toBeInTheDocument();
  });

  it('opens the editor without provenance when the sources fetch fails', async () => {
    vi.mocked(fetchSkillSources).mockRejectedValue(new Error('sources down'));
    renderPage();
    await waitFor(() => expect(bodyField()).toHaveValue('# Triage\n\nReal instructions.'));
    await waitFor(() => expect(fetchSkillSources).toHaveBeenCalled());
    expect(screen.queryByText(/Tracked from/)).not.toBeInTheDocument();
  });
});
