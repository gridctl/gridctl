import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';
import { LibraryWorkspace } from '../components/workspaces/LibraryWorkspace';
import { useRegistryStore } from '../stores/useRegistryStore';
import { showToast } from '../components/ui/Toast';
import { CommandRegistryProvider } from '../hooks/useCommandRegistry';
import type { AgentSkill } from '../types';

vi.mock('../components/ui/Toast', () => ({
  showToast: vi.fn(),
  ToastContainer: () => null,
}));

vi.mock('../lib/api', () => ({
  fetchRegistryStatus: vi.fn().mockResolvedValue({ totalSkills: 0, activeSkills: 0 }),
  fetchRegistrySkills: vi.fn().mockResolvedValue([]),
  activateRegistrySkill: vi.fn().mockResolvedValue(undefined),
  disableRegistrySkill: vi.fn().mockResolvedValue(undefined),
  deleteRegistrySkill: vi.fn().mockResolvedValue(undefined),
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
    useRegistryStore.setState({ skills: SAMPLE_SKILLS, status: { totalSkills: 3, activeSkills: 1 } });
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
});
