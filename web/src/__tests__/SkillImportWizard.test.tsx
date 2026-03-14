import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import { BrowseStep } from '../components/wizard/steps/BrowseStep';
import { SkillImportWizard } from '../components/wizard/steps/SkillImportWizard';
import type { SkillPreview } from '../types';

// Mock API calls
vi.mock('../lib/api', () => ({
  fetchSkillSources: vi.fn().mockResolvedValue([]),
  previewSkillSource: vi.fn(),
  addSkillSource: vi.fn(),
  fetchRegistrySkills: vi.fn().mockResolvedValue([]),
  fetchRegistryStatus: vi.fn().mockResolvedValue({}),
}));

vi.mock('../stores/useRegistryStore', () => ({
  useRegistryStore: Object.assign(() => ({}), {
    getState: () => ({
      setStatus: vi.fn(),
      setSkills: vi.fn(),
    }),
  }),
}));

vi.mock('../../ui/Toast', () => ({
  showToast: vi.fn(),
}));

function makePreview(overrides?: Partial<SkillPreview>): SkillPreview {
  return {
    name: 'test-skill',
    description: 'A test skill',
    body: '# Test\nSome content',
    valid: true,
    exists: false,
    errors: [],
    warnings: [],
    findings: [],
    ...overrides,
  };
}

describe('SkillImportWizard', () => {
  it('renders step indicator with 4 steps', () => {
    render(<SkillImportWizard />);
    expect(screen.getByText('Add Source')).toBeInTheDocument();
    expect(screen.getByText('Browse & Select')).toBeInTheDocument();
    expect(screen.getByText('Configure')).toBeInTheDocument();
    expect(screen.getByText('Review & Install')).toBeInTheDocument();
  });

  it('starts on the Add Source step', () => {
    render(<SkillImportWizard />);
    expect(screen.getByText('Import from Git')).toBeInTheDocument();
    expect(screen.getByPlaceholderText('https://github.com/owner/repo')).toBeInTheDocument();
  });

  it('shows optional ref and path fields', () => {
    render(<SkillImportWizard />);
    expect(screen.getByPlaceholderText('main, v1.0, ^1.2')).toBeInTheDocument();
    expect(screen.getByPlaceholderText('skills/')).toBeInTheDocument();
  });

  it('renders Scan for Skills button', () => {
    render(<SkillImportWizard />);
    expect(screen.getByText('Scan for Skills')).toBeInTheDocument();
  });
});

describe('BrowseStep', () => {
  let onSelectionChange: (selected: Set<string>) => void;

  beforeEach(() => {
    onSelectionChange = vi.fn<(selected: Set<string>) => void>();
  });

  it('renders skill count header', () => {
    const previews = [makePreview(), makePreview({ name: 'other-skill' })];
    render(
      <BrowseStep
        previews={previews}
        selected={new Set()}
        onSelectionChange={onSelectionChange}
      />,
    );
    expect(screen.getByText('2 skills found')).toBeInTheDocument();
  });

  it('shows Select All button', () => {
    render(
      <BrowseStep
        previews={[makePreview()]}
        selected={new Set()}
        onSelectionChange={onSelectionChange}
      />,
    );
    expect(screen.getByText('Select All')).toBeInTheDocument();
  });

  it('displays skill names in the list', () => {
    const previews = [
      makePreview({ name: 'deploy-helper' }),
      makePreview({ name: 'build-runner' }),
    ];
    render(
      <BrowseStep
        previews={previews}
        selected={new Set()}
        onSelectionChange={onSelectionChange}
      />,
    );
    // Name appears in both list and preview panel
    expect(screen.getAllByText('deploy-helper').length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText('build-runner').length).toBeGreaterThanOrEqual(1);
  });

  it('shows invalid badge for invalid skills', () => {
    const previews = [makePreview({ name: 'bad-skill', valid: false })];
    render(
      <BrowseStep
        previews={previews}
        selected={new Set()}
        onSelectionChange={onSelectionChange}
      />,
    );
    // "invalid" appears in both list badge and preview header
    expect(screen.getAllByText('invalid').length).toBeGreaterThanOrEqual(1);
  });

  it('shows exists badge for existing skills', () => {
    const previews = [makePreview({ name: 'old-skill', exists: true })];
    render(
      <BrowseStep
        previews={previews}
        selected={new Set()}
        onSelectionChange={onSelectionChange}
      />,
    );
    expect(screen.getAllByText('exists').length).toBeGreaterThanOrEqual(1);
  });

  it('displays preview panel for active skill', () => {
    const previews = [makePreview({ name: 'my-skill', description: 'Does things' })];
    render(
      <BrowseStep
        previews={previews}
        selected={new Set()}
        onSelectionChange={onSelectionChange}
      />,
    );
    // First skill is auto-selected as active preview; name appears in list + preview
    expect(screen.getAllByText('my-skill').length).toBeGreaterThanOrEqual(2);
    expect(screen.getAllByText('Does things').length).toBeGreaterThanOrEqual(1);
  });

  it('shows body preview content', () => {
    const previews = [makePreview({ body: '# My Skill\nSome instructions here' })];
    render(
      <BrowseStep
        previews={previews}
        selected={new Set()}
        onSelectionChange={onSelectionChange}
      />,
    );
    expect(screen.getByText(/Some instructions here/)).toBeInTheDocument();
  });

  it('calls onSelectionChange when select all is clicked', () => {
    const previews = [
      makePreview({ name: 'skill-a' }),
      makePreview({ name: 'skill-b' }),
    ];
    render(
      <BrowseStep
        previews={previews}
        selected={new Set()}
        onSelectionChange={onSelectionChange}
      />,
    );
    fireEvent.click(screen.getByText('Select All'));
    expect(onSelectionChange).toHaveBeenCalledWith(new Set(['skill-a', 'skill-b']));
  });

  it('excludes invalid and existing skills from select all', () => {
    const previews = [
      makePreview({ name: 'valid-skill' }),
      makePreview({ name: 'invalid-skill', valid: false }),
      makePreview({ name: 'existing-skill', exists: true }),
    ];
    render(
      <BrowseStep
        previews={previews}
        selected={new Set()}
        onSelectionChange={onSelectionChange}
      />,
    );
    fireEvent.click(screen.getByText('Select All'));
    expect(onSelectionChange).toHaveBeenCalledWith(new Set(['valid-skill']));
  });

  it('shows valid and selected counts', () => {
    const previews = [
      makePreview({ name: 'a' }),
      makePreview({ name: 'b', valid: false }),
    ];
    render(
      <BrowseStep
        previews={previews}
        selected={new Set(['a'])}
        onSelectionChange={onSelectionChange}
      />,
    );
    expect(screen.getByText('1 valid, 1 selected')).toBeInTheDocument();
  });

  it('shows validation errors in preview', () => {
    const previews = [makePreview({ errors: ['Missing name field', 'Invalid step'] })];
    render(
      <BrowseStep
        previews={previews}
        selected={new Set()}
        onSelectionChange={onSelectionChange}
      />,
    );
    expect(screen.getByText('Missing name field')).toBeInTheDocument();
    expect(screen.getByText('Invalid step')).toBeInTheDocument();
  });

  it('renders empty preview message when no skills', () => {
    render(
      <BrowseStep
        previews={[]}
        selected={new Set()}
        onSelectionChange={onSelectionChange}
      />,
    );
    expect(screen.getByText('Select a skill to preview')).toBeInTheDocument();
  });
});
