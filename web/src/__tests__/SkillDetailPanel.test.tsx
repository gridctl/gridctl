import { describe, it, expect, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, fireEvent } from '@testing-library/react';
import { SkillDetailPanel } from '../components/registry/SkillDetailPanel';
import type { AgentSkill } from '../types';

// SkillFileTree fetches its own file list; stub it so the Files tab is inert.
vi.mock('../components/registry/SkillFileTree', () => ({
  SkillFileTree: ({ skillName, readOnly }: { skillName: string; readOnly?: boolean }) => (
    <div data-testid="file-tree" data-skill={skillName} data-readonly={String(!!readOnly)} />
  ),
}));

const SKILL: AgentSkill = {
  name: 'incident-triage',
  description: 'Triage incidents quickly',
  license: 'Apache-2.0',
  compatibility: 'Requires git',
  allowedTools: 'Bash(git:*) Read Write',
  metadata: { author: 'ops' },
  acceptanceCriteria: ['GIVEN an alert WHEN it is triaged THEN severity is set'],
  state: 'active',
  body: '# Triage\n\nFollow the runbook.',
  fileCount: 2,
  dir: 'ops/incident-triage',
};

function noop() {}

function renderPanel(overrides: Partial<React.ComponentProps<typeof SkillDetailPanel>> = {}) {
  return render(
    <SkillDetailPanel
      skill={SKILL}
      onClose={noop}
      onEdit={noop}
      onToggle={noop}
      onDelete={noop}
      {...overrides}
    />,
  );
}

describe('SkillDetailPanel', () => {
  it('renders an empty state when no skill is selected', () => {
    render(
      <SkillDetailPanel skill={null} onClose={noop} onEdit={noop} onToggle={noop} onDelete={noop} />,
    );
    expect(screen.getByText(/select a skill to inspect/i)).toBeInTheDocument();
  });

  it('shows the header with name, state badge, and three tabs', () => {
    renderPanel();
    expect(screen.getByRole('heading', { name: 'incident-triage' })).toBeInTheDocument();
    expect(screen.getByText('active')).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Overview' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Instructions' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Files' })).toBeInTheDocument();
  });

  it('renders Overview content: description, allowed-tools chips, and criteria', () => {
    renderPanel();
    expect(screen.getByText('Triage incidents quickly')).toBeInTheDocument();
    expect(screen.getByText('Bash(git:*)')).toBeInTheDocument();
    expect(screen.getByText('Read')).toBeInTheDocument();
    expect(screen.getByText('Write')).toBeInTheDocument();
    // GIVEN/WHEN/THEN parsed into parts.
    expect(screen.getByText('an alert')).toBeInTheDocument();
    expect(screen.getByText('it is triaged')).toBeInTheDocument();
    expect(screen.getByText('severity is set')).toBeInTheDocument();
  });

  it('shows the rendered body on the Instructions tab', () => {
    renderPanel();
    fireEvent.click(screen.getByRole('tab', { name: 'Instructions' }));
    expect(screen.getByText('Follow the runbook.')).toBeInTheDocument();
  });

  it('mounts the read-only file tree only on the Files tab', () => {
    renderPanel();
    expect(screen.queryByTestId('file-tree')).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('tab', { name: 'Files' }));
    const tree = screen.getByTestId('file-tree');
    expect(tree).toHaveAttribute('data-skill', 'incident-triage');
    expect(tree).toHaveAttribute('data-readonly', 'true');
  });

  it('moves between tabs with Left/Right arrow keys', () => {
    renderPanel();
    const overview = screen.getByRole('tab', { name: 'Overview' });
    fireEvent.keyDown(overview, { key: 'ArrowRight' });
    expect(screen.getByRole('tab', { name: 'Instructions' })).toHaveAttribute('aria-selected', 'true');
  });

  it('wires each tabpanel to its tab via aria-labelledby', () => {
    renderPanel();
    const panel = screen.getByRole('tabpanel');
    expect(panel).toHaveAttribute('aria-labelledby', 'skill-tab-overview');
  });

  it('calls onEdit when the Edit button is clicked', () => {
    const onEdit = vi.fn();
    renderPanel({ onEdit });
    fireEvent.click(screen.getByRole('button', { name: /^edit$/i }));
    expect(onEdit).toHaveBeenCalledWith(SKILL);
  });

  it('calls onToggle / onDelete from the header actions', () => {
    const onToggle = vi.fn();
    const onDelete = vi.fn();
    renderPanel({ onToggle, onDelete });
    fireEvent.click(screen.getByRole('button', { name: /disable skill/i }));
    fireEvent.click(screen.getByRole('button', { name: /delete skill/i }));
    expect(onToggle).toHaveBeenCalledWith(SKILL);
    expect(onDelete).toHaveBeenCalledWith(SKILL);
  });

  it('lists related skills and selects one on click', () => {
    const onSelectRelated = vi.fn();
    const related: AgentSkill = { ...SKILL, name: 'incident-postmortem', acceptanceCriteria: [] };
    renderPanel({ relatedSkills: [related], onSelectRelated });
    fireEvent.click(screen.getByText('incident-postmortem'));
    expect(onSelectRelated).toHaveBeenCalledWith('incident-postmortem');
  });
});
