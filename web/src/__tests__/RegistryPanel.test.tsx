import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';

// Mock external dependencies
vi.mock('../stores/useUIStore', () => ({
  useUIStore: vi.fn((selector) => selector({
    setSidebarOpen: vi.fn(),
    registryDetached: false,
    editorDetached: false,
  })),
}));

vi.mock('../stores/useStackStore', () => ({
  useStackStore: vi.fn((selector) => selector({
    selectNode: vi.fn(),
  })),
}));

vi.mock('../hooks/useWindowManager', () => ({
  useWindowManager: () => ({
    openDetachedWindow: vi.fn(),
  }),
}));

vi.mock('../components/ui/PopoutButton', () => ({
  PopoutButton: ({ onClick, disabled }: { onClick: () => void; disabled?: boolean }) => (
    <button data-testid="popout-button" onClick={onClick} disabled={disabled}>
      Popout
    </button>
  ),
}));

vi.mock('../components/registry/SkillEditor', () => ({
  SkillEditor: ({ isOpen }: { isOpen: boolean }) => (
    isOpen ? <div data-testid="skill-editor" /> : null
  ),
}));

vi.mock('../components/ui/Toast', () => ({
  showToast: vi.fn(),
}));

vi.mock('../lib/api', () => ({
  fetchRegistryStatus: vi.fn(),
  fetchRegistrySkills: vi.fn(),
  deleteRegistrySkill: vi.fn(),
  activateRegistrySkill: vi.fn(),
  disableRegistrySkill: vi.fn(),
  fetchSkillUpdates: vi.fn().mockResolvedValue({ available: 0, sources: [] }),
  updateSkillSource: vi.fn(),
  getSkillTestResult: vi.fn().mockResolvedValue({ skill: '', passed: 0, failed: 0, results: [], status: 'untested' }),
}));

vi.mock('../lib/workflowSync', () => ({
  hasWorkflowBlock: vi.fn().mockReturnValue(false),
}));

import { RegistrySidebar } from '../components/registry/RegistrySidebar';
import { useRegistryStore } from '../stores/useRegistryStore';
import type { AgentSkill, RegistryStatus } from '../types';

beforeEach(() => {
  vi.clearAllMocks();
  useRegistryStore.setState({
    skills: [],
    status: null as unknown as RegistryStatus,
    isLoading: false,
    error: null,
  });
});

function makeSkill(overrides: Partial<AgentSkill> = {}): AgentSkill {
  return {
    name: 'test-skill',
    description: 'A test skill',
    state: 'active',
    body: '# Test Skill\nSome content',
    fileCount: 0,
    ...overrides,
  };
}

describe('RegistrySidebar', () => {
  it('shows empty state when no skills registered', () => {
    render(<RegistrySidebar />);
    expect(screen.getByText('No skills registered')).toBeInTheDocument();
  });

  it('shows skill count', () => {
    useRegistryStore.setState({
      skills: [makeSkill({ name: 'skill-1' }), makeSkill({ name: 'skill-2' })],
    });
    render(<RegistrySidebar />);
    expect(screen.getByText('2 skills')).toBeInTheDocument();
  });

  it('lists skill names', () => {
    useRegistryStore.setState({
      skills: [
        makeSkill({ name: 'deploy-tool' }),
        makeSkill({ name: 'lint-checker' }),
      ],
    });
    render(<RegistrySidebar />);
    expect(screen.getByText('deploy-tool')).toBeInTheDocument();
    expect(screen.getByText('lint-checker')).toBeInTheDocument();
  });

  it('displays state badge for each skill', () => {
    useRegistryStore.setState({
      skills: [makeSkill({ name: 'active-skill', state: 'active' })],
    });
    render(<RegistrySidebar />);
    expect(screen.getByText('active')).toBeInTheDocument();
  });

  it('filters skills by search query', () => {
    useRegistryStore.setState({
      skills: [
        makeSkill({ name: 'deploy-tool', description: 'Deploy things' }),
        makeSkill({ name: 'lint-checker', description: 'Check linting' }),
      ],
    });
    render(<RegistrySidebar />);

    const searchInput = screen.getByPlaceholderText('Search skills...');
    fireEvent.change(searchInput, { target: { value: 'deploy' } });

    expect(screen.getByText('deploy-tool')).toBeInTheDocument();
    expect(screen.queryByText('lint-checker')).not.toBeInTheDocument();
  });

  it('shows filtered count during search', () => {
    useRegistryStore.setState({
      skills: [
        makeSkill({ name: 'deploy-tool' }),
        makeSkill({ name: 'lint-checker' }),
        makeSkill({ name: 'test-runner' }),
      ],
    });
    render(<RegistrySidebar />);

    const searchInput = screen.getByPlaceholderText('Search skills...');
    fireEvent.change(searchInput, { target: { value: 'deploy' } });

    expect(screen.getByText('1 of 3 skills')).toBeInTheDocument();
  });

  it('shows "No matching skills" when search has no results', () => {
    useRegistryStore.setState({
      skills: [makeSkill({ name: 'deploy-tool' })],
    });
    render(<RegistrySidebar />);

    const searchInput = screen.getByPlaceholderText('Search skills...');
    fireEvent.change(searchInput, { target: { value: 'nonexistent' } });

    expect(screen.getByText('No matching skills')).toBeInTheDocument();
  });

  it('shows delete confirmation when delete is clicked', () => {
    useRegistryStore.setState({
      skills: [makeSkill({ name: 'my-skill' })],
    });
    render(<RegistrySidebar />);

    // Expand the skill item
    fireEvent.click(screen.getByText('my-skill'));

    // Click delete button in expanded actions
    const deleteButtons = screen.getAllByText('Delete');
    fireEvent.click(deleteButtons[0]);

    expect(screen.getByText('This action cannot be undone.')).toBeInTheDocument();
  });

  it('cancels delete confirmation', () => {
    useRegistryStore.setState({
      skills: [makeSkill({ name: 'my-skill' })],
    });
    render(<RegistrySidebar />);

    // Expand and click delete
    fireEvent.click(screen.getByText('my-skill'));
    fireEvent.click(screen.getByText('Delete'));

    // Click cancel in the confirmation overlay
    fireEvent.click(screen.getByText('Cancel'));

    // Confirmation message should be gone
    expect(screen.queryByText('This action cannot be undone.')).not.toBeInTheDocument();
  });

  it('renders New and Import buttons', () => {
    render(<RegistrySidebar />);
    expect(screen.getByText('New')).toBeInTheDocument();
    expect(screen.getByText('Import')).toBeInTheDocument();
  });

  it('shows status footer with totals', () => {
    useRegistryStore.setState({
      skills: [makeSkill()],
      status: { totalSkills: 5, activeSkills: 3 },
    });
    render(<RegistrySidebar />);
    expect(screen.getByText('5 total')).toBeInTheDocument();
    expect(screen.getByText('3 active')).toBeInTheDocument();
  });

  it('shows header with Registry title when not embedded', () => {
    render(<RegistrySidebar />);
    expect(screen.getByText('Registry')).toBeInTheDocument();
  });

  it('hides header when embedded', () => {
    render(<RegistrySidebar embedded />);
    expect(screen.queryByText('Registry')).not.toBeInTheDocument();
  });
});
