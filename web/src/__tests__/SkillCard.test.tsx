import { describe, it, expect, beforeEach } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, cleanup, within } from '@testing-library/react';
import { SkillCard } from '../components/registry/SkillCard';
import type { AgentSkill, SkillSourceStatus } from '../types';

beforeEach(() => {
  cleanup();
});

function skill(overrides: Partial<AgentSkill> = {}): AgentSkill {
  return {
    name: 'branch-fork',
    description: 'Create a feature branch',
    state: 'active',
    dir: 'git-workflow/branch-fork',
    body: '',
    fileCount: 3,
    acceptanceCriteria: ['a', 'b', 'c', 'd'],
    ...overrides,
  } as AgentSkill;
}

const noop = () => {};

function renderCard(s: AgentSkill, source?: SkillSourceStatus) {
  return render(
    <SkillCard
      skill={s}
      onEnable={noop}
      onDisable={noop}
      onEdit={noop}
      onDelete={noop}
      source={source}
    />,
  );
}

describe('SkillCard', () => {
  it('shows a category label and metadata line for a skill with files and criteria', () => {
    renderCard(skill());
    // Category renders title-cased text (CSS uppercases it visually).
    expect(screen.getByText('Git Workflow')).toBeInTheDocument();
    expect(screen.getByText('3 files · 4 criteria')).toBeInTheDocument();
  });

  it('omits file/criteria segments when there are none, leaving an empty slot', () => {
    renderCard(skill({ fileCount: 0, acceptanceCriteria: [] }));
    expect(screen.queryByText(/\d+\s+files?/)).not.toBeInTheDocument();
    expect(screen.queryByText(/criteri/)).not.toBeInTheDocument();
    // The fixed-height metadata slot is still present (only the category text).
    const meta = screen.getByTestId('skill-meta');
    expect(meta).toBeInTheDocument();
    expect(meta).toHaveTextContent('Git Workflow');
  });

  it('renders a completely empty metadata slot for a skill with no category or summary', () => {
    renderCard(skill({ dir: undefined, fileCount: 0, acceptanceCriteria: [] }));
    expect(screen.queryByText('Git Workflow')).not.toBeInTheDocument();
    // Slot still exists (reserves card height) but carries no visible text.
    expect(screen.getByTestId('skill-meta').textContent).toBe('');
  });

  it('omits the category when the skill has no dir', () => {
    renderCard(skill({ dir: undefined }));
    expect(screen.queryByText('Git Workflow')).not.toBeInTheDocument();
    // Summary still renders.
    expect(screen.getByText('3 files · 4 criteria')).toBeInTheDocument();
  });

  it('keeps the StateBadge rendered in the header', () => {
    renderCard(skill({ state: 'draft' }));
    expect(screen.getByText('draft')).toBeInTheDocument();
  });

  it('still renders the #719 imported-source icon when a source is provided', () => {
    const source: SkillSourceStatus = {
      name: 'acme-skills',
      repo: 'acme/skills',
      autoUpdate: false,
      updateInterval: '',
      updateAvailable: false,
      skills: [],
    };
    renderCard(skill(), source);
    expect(screen.getByLabelText('Imported from acme/skills')).toBeInTheDocument();
    // State badge remains alongside it.
    expect(screen.getByText('active')).toBeInTheDocument();
  });

  it('uses singular nouns for a count of one', () => {
    renderCard(skill({ fileCount: 1, acceptanceCriteria: ['only'] }));
    const meta = screen.getByTestId('skill-meta');
    expect(within(meta).getByText('1 file · 1 criterion')).toBeInTheDocument();
  });
});
