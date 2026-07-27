import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { SkillDetailPanel } from '../components/registry/SkillDetailPanel';
import { fetchRegistrySkill } from '../lib/api';
import type { AgentSkill } from '../types';

vi.mock('../lib/api', () => ({
  fetchRegistrySkill: vi.fn(),
}));

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
  beforeEach(() => {
    vi.clearAllMocks();
  });

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

  it('shows the text-size control only on the rendered Instructions view', () => {
    renderPanel();
    // Not on Overview.
    expect(screen.queryByTitle(/increase font size/i)).not.toBeInTheDocument();
    // Present on the rendered Instructions tab.
    fireEvent.click(screen.getByRole('tab', { name: 'Instructions' }));
    expect(screen.getByTitle(/increase font size/i)).toBeInTheDocument();
    // Hidden when viewing raw source (where it would do nothing).
    fireEvent.click(screen.getByRole('button', { name: /view source/i }));
    expect(screen.queryByTitle(/increase font size/i)).not.toBeInTheDocument();
  });

  // The registry list no longer carries Markdown bodies, so a skill selected
  // from the catalog arrives with `body` undefined and the panel must fetch it.
  describe('lazy instructions', () => {
    // A list-sourced skill: everything the catalog needs, no body.
    const LIST_SKILL: AgentSkill = { ...SKILL, body: undefined };

    it('fetches nothing until the Instructions tab is opened', () => {
      renderPanel({ skill: LIST_SKILL });
      expect(fetchRegistrySkill).not.toHaveBeenCalled();
    });

    it('issues exactly one single-skill fetch on tab open and renders the body', async () => {
      vi.mocked(fetchRegistrySkill).mockResolvedValue({ ...SKILL, body: '# Triage\n\nFetched runbook.' });
      renderPanel({ skill: LIST_SKILL });

      fireEvent.click(screen.getByRole('tab', { name: 'Instructions' }));
      expect(await screen.findByText('Fetched runbook.')).toBeInTheDocument();
      expect(fetchRegistrySkill).toHaveBeenCalledTimes(1);
      expect(fetchRegistrySkill).toHaveBeenCalledWith('incident-triage');
    });

    it('does not fetch when the skill already carries a body', async () => {
      renderPanel();
      fireEvent.click(screen.getByRole('tab', { name: 'Instructions' }));
      expect(await screen.findByText('Follow the runbook.')).toBeInTheDocument();
      expect(fetchRegistrySkill).not.toHaveBeenCalled();
    });

    it('surfaces a load failure instead of rendering an empty instructions pane', async () => {
      vi.mocked(fetchRegistrySkill).mockRejectedValue(new Error('registry offline'));
      renderPanel({ skill: LIST_SKILL });

      fireEvent.click(screen.getByRole('tab', { name: 'Instructions' }));
      await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent(/registry offline/i));
      // The "no instructions" hint would be a lie here, so it must not appear.
      expect(screen.queryByText(/this skill has no instructions/i)).not.toBeInTheDocument();
    });

    it('renders the honest empty hint when the fetched body really is empty', async () => {
      vi.mocked(fetchRegistrySkill).mockResolvedValue({ ...SKILL, body: '' });
      renderPanel({ skill: LIST_SKILL });

      fireEvent.click(screen.getByRole('tab', { name: 'Instructions' }));
      expect(await screen.findByText(/this skill has no instructions/i)).toBeInTheDocument();
    });
  });
});
