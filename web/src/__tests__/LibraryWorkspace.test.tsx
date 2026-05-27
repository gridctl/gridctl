import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';
import { LibraryWorkspace } from '../components/workspaces/LibraryWorkspace';
import { useRegistryStore } from '../stores/useRegistryStore';
import { showToast } from '../components/ui/Toast';
import { CommandRegistryProvider } from '../hooks/useCommandRegistry';
import type { AgentSkill, SkillSourceStatus } from '../types';

vi.mock('../components/ui/Toast', () => ({
  showToast: vi.fn(),
  ToastContainer: () => null,
}));

vi.mock('../lib/api', () => ({
  fetchRegistryStatus: vi.fn().mockResolvedValue({ totalSkills: 0, activeSkills: 0 }),
  fetchRegistrySkills: vi.fn().mockResolvedValue([]),
  fetchSkillSources: vi.fn().mockResolvedValue([]),
  updateSkillSource: vi.fn().mockResolvedValue({ source: 'acme-skills', results: [] }),
  activateRegistrySkill: vi.fn().mockResolvedValue(undefined),
  disableRegistrySkill: vi.fn().mockResolvedValue(undefined),
  deleteRegistrySkill: vi.fn().mockResolvedValue(undefined),
  // Used by SkillFileTree (mounted only on the inspector's Files tab).
  fetchSkillFiles: vi.fn().mockResolvedValue([]),
}));

// SkillEditor is heavy and unrelated to the workspace's URL-state behavior.
// Stub so we can detect "mounted with this skill".
vi.mock('../components/registry/SkillEditor', () => ({
  SkillEditor: ({ isOpen, skill, onClose }: { isOpen: boolean; skill?: AgentSkill; onClose: () => void }) =>
    isOpen ? (
      <div data-testid="skill-editor">
        <span data-testid="editing-skill-name">{skill?.name ?? ''}</span>
        <button onClick={onClose}>close-editor</button>
      </div>
    ) : null,
}));

const SAMPLE_SKILLS: AgentSkill[] = [
  // @ts-expect-error partial AgentSkill is fine for the test
  { name: 'incident-triage', description: 'triage incidents', state: 'active', dir: 'ops', fileCount: 1 },
  // @ts-expect-error partial AgentSkill is fine for the test
  { name: 'draft-summarizer', description: 'summarize drafts', state: 'draft', dir: 'tools', fileCount: 1 },
  // @ts-expect-error partial AgentSkill is fine for the test
  { name: 'disabled-skill', description: 'paused', state: 'disabled', dir: 'archive', fileCount: 1 },
];

// One imported source that owns `draft-summarizer`; the other two skills are
// local ("My Skills").
const SAMPLE_SOURCES: SkillSourceStatus[] = [
  {
    name: 'acme-skills',
    repo: 'https://github.com/acme/skills',
    commitSha: 'abcdef1234567',
    autoUpdate: false,
    updateInterval: '',
    updateAvailable: false,
    skills: [{ name: 'draft-summarizer', description: 'summarize drafts', state: 'draft', isRemote: true }],
  },
];

function LocationProbe({ onChange }: { onChange: (search: string) => void }) {
  const location = useLocation();
  onChange(location.search);
  return null;
}

function renderAt(path: string, onLocationChange?: (search: string) => void) {
  return render(
    <CommandRegistryProvider>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/library" element={
            <>
              <LibraryWorkspace />
              {onLocationChange && <LocationProbe onChange={onLocationChange} />}
            </>
          } />
          <Route path="/library/:skillName" element={
            <>
              <LibraryWorkspace />
              {onLocationChange && <LocationProbe onChange={onLocationChange} />}
            </>
          } />
        </Routes>
      </MemoryRouter>
    </CommandRegistryProvider>,
  );
}

describe('LibraryWorkspace', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    // Reset sources to null so provenance grouping is off by default; tests that
    // exercise grouping opt in explicitly.
    useRegistryStore.setState({ skills: SAMPLE_SKILLS, status: { totalSkills: 3, activeSkills: 1 }, sources: null });
  });

  it('renders the skill grid with all skills when filter is all', () => {
    renderAt('/library');
    expect(screen.getByText('incident-triage')).toBeInTheDocument();
    expect(screen.getByText('draft-summarizer')).toBeInTheDocument();
    expect(screen.getByText('disabled-skill')).toBeInTheDocument();
  });

  it('restores search query from URL ?q= on initial render', () => {
    renderAt('/library?q=incident');
    const input = screen.getByLabelText('Filter skills') as HTMLInputElement;
    expect(input.value).toBe('incident');
  });

  it('restores filter from URL ?filter= on initial render', () => {
    renderAt('/library?filter=draft');
    expect(screen.getByText('draft-summarizer')).toBeInTheDocument();
    expect(screen.queryByText('incident-triage')).not.toBeInTheDocument();
  });

  it('updates the URL when the user types in the search box', async () => {
    let currentSearch = '';
    renderAt('/library', (s) => { currentSearch = s; });
    const input = screen.getByLabelText('Filter skills') as HTMLInputElement;
    fireEvent.change(input, { target: { value: 'incident' } });
    await waitFor(() => {
      expect(currentSearch).toContain('q=incident');
    });
  });

  it('mounts the editor for /library/:skillName when the skill exists', async () => {
    renderAt('/library/incident-triage');
    await waitFor(() => {
      expect(screen.getByTestId('skill-editor')).toBeInTheDocument();
    });
    expect(screen.getByTestId('editing-skill-name').textContent).toBe('incident-triage');
  });

  it('toasts when /library/:skillName names an unknown skill', async () => {
    renderAt('/library/never-existed');
    await waitFor(() => {
      expect(showToast).toHaveBeenCalledWith('error', expect.stringContaining('never-existed'));
    });
  });

  it('does not fire the not-found toast while the registry is still loading', () => {
    useRegistryStore.setState({ skills: null });
    renderAt('/library/never-existed');
    expect(showToast).not.toHaveBeenCalled();
  });

  describe('KPI summary header', () => {
    it('renders a clickable KPI card per state with search-aware counts', () => {
      renderAt('/library');
      // SAMPLE_SKILLS: 3 total, 1 active, 1 draft, 1 disabled.
      expect(screen.getByRole('button', { name: 'Total (3)' })).toBeInTheDocument();
      expect(screen.getByRole('button', { name: 'Active (1)' })).toBeInTheDocument();
      expect(screen.getByRole('button', { name: 'Draft (1)' })).toBeInTheDocument();
      expect(screen.getByRole('button', { name: 'Disabled (1)' })).toBeInTheDocument();
    });

    it('applies the matching ?filter when a KPI card is clicked', async () => {
      let currentSearch = '';
      renderAt('/library', (s) => { currentSearch = s; });

      fireEvent.click(screen.getByRole('button', { name: 'Draft (1)' }));
      await waitFor(() => expect(currentSearch).toContain('filter=draft'));
      // Only the draft skill remains visible.
      expect(screen.getByText('draft-summarizer')).toBeInTheDocument();
      expect(screen.queryByText('incident-triage')).not.toBeInTheDocument();
    });

    it('clears the filter when Total is clicked (counts stay search-aware, not tab-aware)', async () => {
      let currentSearch = '?filter=draft';
      renderAt('/library?filter=draft', (s) => { currentSearch = s; });

      // Counts come from the unfiltered search results, so Total is still 3.
      fireEvent.click(screen.getByRole('button', { name: 'Total (3)' }));
      await waitFor(() => expect(currentSearch).not.toContain('filter='));
      expect(screen.getByText('incident-triage')).toBeInTheDocument();
    });

    it('marks the KPI card matching ?filter as active', () => {
      renderAt('/library?filter=active');
      expect(screen.getByRole('button', { name: 'Active (1)' })).toHaveAttribute('aria-pressed', 'true');
      expect(screen.getByRole('button', { name: 'Total (3)' })).toHaveAttribute('aria-pressed', 'false');
    });
  });

  describe('inspector pane', () => {
    it('shows the empty state when nothing is selected', () => {
      renderAt('/library');
      expect(screen.getByText(/select a skill to inspect/i)).toBeInTheDocument();
    });

    it('opens the inspector and sets ?selected= when a card is clicked', async () => {
      let currentSearch = '';
      renderAt('/library', (s) => { currentSearch = s; });

      fireEvent.click(screen.getByText('incident-triage'));
      await waitFor(() => expect(currentSearch).toContain('selected=incident-triage'));
      // The inspector header (h2) carries the selected skill's name.
      expect(screen.getByRole('heading', { name: 'incident-triage' })).toBeInTheDocument();
    });

    it('restores the inspector from ?selected= on initial render', () => {
      renderAt('/library?selected=draft-summarizer');
      expect(screen.getByRole('heading', { name: 'draft-summarizer' })).toBeInTheDocument();
    });

    it('ignores an unknown ?selected= gracefully (empty state)', () => {
      renderAt('/library?selected=never-existed');
      expect(screen.getByText(/select a skill to inspect/i)).toBeInTheDocument();
      expect(screen.queryByRole('heading', { name: 'never-existed' })).not.toBeInTheDocument();
    });

    it('clears the selection and ?selected= when the inspector is closed', async () => {
      let currentSearch = '?selected=incident-triage';
      renderAt('/library?selected=incident-triage', (s) => { currentSearch = s; });

      expect(screen.getByRole('heading', { name: 'incident-triage' })).toBeInTheDocument();
      fireEvent.click(screen.getByRole('button', { name: /close inspector/i }));
      await waitFor(() => expect(currentSearch).not.toContain('selected='));
      expect(screen.getByText(/select a skill to inspect/i)).toBeInTheDocument();
    });

    it('promotes to the editor when the inspector Edit button is clicked', async () => {
      renderAt('/library?selected=incident-triage');
      fireEvent.click(screen.getByRole('button', { name: /^edit$/i }));
      await waitFor(() => expect(screen.getByTestId('skill-editor')).toBeInTheDocument());
      expect(screen.getByTestId('editing-skill-name').textContent).toBe('incident-triage');
    });
  });

  describe('provenance grouping', () => {
    it('shows no grouping control or "My Skills" header when there are no sources', () => {
      renderAt('/library');
      expect(screen.queryByRole('group', { name: 'Group skills by' })).not.toBeInTheDocument();
      expect(screen.queryByText('My Skills')).not.toBeInTheDocument();
      // All skills still render (category/flat behavior is unchanged).
      expect(screen.getByText('incident-triage')).toBeInTheDocument();
      expect(screen.getByText('draft-summarizer')).toBeInTheDocument();
    });

    it('defaults to source grouping with "My Skills" + per-source sections when sources exist', () => {
      useRegistryStore.setState({ sources: SAMPLE_SOURCES });
      renderAt('/library');
      expect(screen.getByRole('group', { name: 'Group skills by' })).toBeInTheDocument();
      expect(screen.getByText('My Skills')).toBeInTheDocument();
      expect(screen.getByText('acme/skills')).toBeInTheDocument();
      // Everything is still visible — grouping organizes, it does not hide.
      expect(screen.getByText('incident-triage')).toBeInTheDocument();
      expect(screen.getByText('draft-summarizer')).toBeInTheDocument();
      expect(screen.getByText('disabled-skill')).toBeInTheDocument();
    });

    it('shows the short commit SHA in the source header', () => {
      useRegistryStore.setState({ sources: SAMPLE_SOURCES });
      renderAt('/library');
      expect(screen.getByText('abcdef1')).toBeInTheDocument();
    });

    it('reflects the Group by selection in ?group= (and omits the default)', async () => {
      useRegistryStore.setState({ sources: SAMPLE_SOURCES });
      let currentSearch = '';
      renderAt('/library', (s) => { currentSearch = s; });

      fireEvent.click(screen.getByRole('button', { name: 'None' }));
      await waitFor(() => expect(currentSearch).toContain('group=none'));

      fireEvent.click(screen.getByRole('button', { name: 'Source' }));
      await waitFor(() => expect(currentSearch).not.toContain('group='));
    });

    it('restores grouping from ?group=none on initial render', () => {
      useRegistryStore.setState({ sources: SAMPLE_SOURCES });
      renderAt('/library?group=none');
      // Flat mode → no group headers.
      expect(screen.queryByText('My Skills')).not.toBeInTheDocument();
      expect(screen.queryByText('acme/skills')).not.toBeInTheDocument();
      expect(screen.getByText('draft-summarizer')).toBeInTheDocument();
    });

    it('isolates a source when its header is clicked and sets ?source=', async () => {
      useRegistryStore.setState({ sources: SAMPLE_SOURCES });
      let currentSearch = '';
      renderAt('/library', (s) => { currentSearch = s; });

      fireEvent.click(screen.getByText('acme/skills'));
      await waitFor(() => expect(currentSearch).toContain('source=acme-skills'));
      // Only the imported skill remains; local skills are hidden.
      expect(screen.getByText('draft-summarizer')).toBeInTheDocument();
      expect(screen.queryByText('incident-triage')).not.toBeInTheDocument();
    });

    it('restores the isolate from ?source= and clears it via the source facet chip', async () => {
      useRegistryStore.setState({ sources: SAMPLE_SOURCES });
      let currentSearch = '?source=acme-skills';
      renderAt('/library?source=acme-skills', (s) => { currentSearch = s; });

      expect(screen.queryByText('incident-triage')).not.toBeInTheDocument();
      // The source facet chip is now the persistent clear control.
      fireEvent.click(screen.getByRole('button', { name: /clear source filter/i }));
      await waitFor(() => expect(currentSearch).not.toContain('source='));
      expect(screen.getByText('incident-triage')).toBeInTheDocument();
    });

    it('isolates "My Skills" with ?source=local', async () => {
      useRegistryStore.setState({ sources: SAMPLE_SOURCES });
      let currentSearch = '';
      renderAt('/library', (s) => { currentSearch = s; });

      fireEvent.click(screen.getByText('My Skills'));
      await waitFor(() => expect(currentSearch).toContain('source=local'));
      expect(screen.getByText('incident-triage')).toBeInTheDocument();
      expect(screen.queryByText('draft-summarizer')).not.toBeInTheDocument();
    });
  });

  describe('sort control', () => {
    it('round-trips the sort axis through ?sort (default name omitted)', async () => {
      let currentSearch = '';
      renderAt('/library', (s) => { currentSearch = s; });

      fireEvent.click(screen.getByRole('button', { name: 'State' }));
      await waitFor(() => expect(currentSearch).toContain('sort=state'));

      fireEvent.click(screen.getByRole('button', { name: 'Name' }));
      await waitFor(() => expect(currentSearch).not.toContain('sort='));
    });

    it('sorts by name (alphabetical) by default', () => {
      // group=none → flat list, so DOM order equals sort order.
      renderAt('/library?group=none');
      const disabled = screen.getByText('disabled-skill');
      const incident = screen.getByText('incident-triage');
      // 'd' sorts before 'i', so disabled-skill renders first.
      expect(disabled.compareDocumentPosition(incident) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    });

    it('reorders by state when ?sort=state (active before disabled)', () => {
      renderAt('/library?group=none&sort=state');
      const active = screen.getByText('incident-triage');
      const disabled = screen.getByText('disabled-skill');
      expect(active.compareDocumentPosition(disabled) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    });
  });

  describe('facet chips', () => {
    it('renders no facet strip when nothing is active', () => {
      renderAt('/library');
      expect(screen.queryByRole('group', { name: 'Active filters' })).not.toBeInTheDocument();
    });

    it('shows a search chip that clears ?q', async () => {
      let currentSearch = '?q=incident';
      renderAt('/library?q=incident', (s) => { currentSearch = s; });
      fireEvent.click(screen.getByRole('button', { name: /clear search filter/i }));
      await waitFor(() => expect(currentSearch).not.toContain('q='));
    });

    it('shows a state chip that clears ?filter', async () => {
      let currentSearch = '?filter=draft';
      renderAt('/library?filter=draft', (s) => { currentSearch = s; });
      fireEvent.click(screen.getByRole('button', { name: /clear state filter/i }));
      await waitFor(() => expect(currentSearch).not.toContain('filter='));
    });

    it('shows a sort chip that resets sort to default', async () => {
      let currentSearch = '?sort=files';
      renderAt('/library?sort=files', (s) => { currentSearch = s; });
      fireEvent.click(screen.getByRole('button', { name: /clear sort filter/i }));
      await waitFor(() => expect(currentSearch).not.toContain('sort='));
    });

    it('clears multiple facets at once with Clear all', async () => {
      let currentSearch = '?q=incident&filter=active';
      renderAt('/library?q=incident&filter=active', (s) => { currentSearch = s; });
      fireEvent.click(screen.getByRole('button', { name: /clear all filters/i }));
      await waitFor(() => {
        expect(currentSearch).not.toContain('q=');
        expect(currentSearch).not.toContain('filter=');
      });
    });
  });
});
