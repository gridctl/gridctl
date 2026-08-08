import { describe, it, expect, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { SkillDetailPanel } from '../components/registry/SkillDetailPanel';
import { AgentDetailPanel } from '../components/registry/agents/AgentDetailPanel';
import type { AgentSkill, ModelPreference, RegistryAgent } from '../types';

vi.mock('../lib/api', () => ({
  fetchRegistrySkill: vi.fn(),
  fetchRegistryAgent: vi.fn(),
  fetchSkillFiles: vi.fn().mockResolvedValue([]),
  updateSkillSource: vi.fn().mockResolvedValue({ source: 'acme', results: [] }),
}));

vi.mock('../components/ui/Toast', () => ({
  showToast: vi.fn(),
  ToastContainer: () => null,
}));

vi.mock('../components/registry/SkillFileTree', () => ({
  SkillFileTree: () => <div data-testid="file-tree" />,
}));

const PREF: ModelPreference = {
  declared: { value: 'opus', sourceKey: 'model' },
  resolved: { value: 'haiku', resolution: 'override' },
  honor: { 'claude-code': 'honored', antigravity: 'ignored' },
};

const SKILL: AgentSkill = {
  name: 'incident-triage',
  description: 'Triage incidents quickly',
  state: 'active',
  body: '# Triage',
  fileCount: 2,
};

const AGENT: RegistryAgent = {
  name: 'reviewer',
  description: 'Reviews things',
  extra: [
    { key: 'tools', value: 'Read, Bash' },
    { key: 'model', value: 'opus' },
  ],
};

function noop() {}

function renderSkillPanel(skill: AgentSkill) {
  return render(
    <SkillDetailPanel skill={skill} onClose={noop} onEdit={noop} onToggle={noop} onDelete={noop} />,
  );
}

function renderAgentPanel(agent: RegistryAgent) {
  return render(
    <MemoryRouter>
      <AgentDetailPanel
        agent={agent}
        statuses={[]}
        onClose={noop}
        onEdit={noop}
        onDelete={noop}
        onRefresh={noop}
      />
    </MemoryRouter>,
  );
}

describe('SkillDetailPanel model preference', () => {
  it('renders the section, chip, and honor rows when a preference exists', () => {
    renderSkillPanel({ ...SKILL, modelPreference: PREF });
    expect(screen.getByText('Model preference')).toBeInTheDocument();
    // The resolved value wins the chip with policy provenance.
    const chip = screen.getByTestId('model-chip');
    expect(chip).toHaveTextContent('haiku');
    expect(chip).toHaveTextContent('policy');
    expect(screen.getByText('opus (model)')).toBeInTheDocument();
    expect(screen.getByText('haiku (policy override)')).toBeInTheDocument();
    expect(screen.getByTestId('model-honor-list')).toBeInTheDocument();
  });

  it('renders neither section nor chip when the object is absent (older backend)', () => {
    renderSkillPanel(SKILL);
    expect(screen.queryByText('Model preference')).not.toBeInTheDocument();
    expect(screen.queryByTestId('model-chip')).not.toBeInTheDocument();
  });
});

describe('AgentDetailPanel model preference', () => {
  it('upgrades the raw model row to the typed section when the view exists', () => {
    renderAgentPanel({ ...AGENT, modelPreference: PREF });
    expect(screen.getByText('Model preference')).toBeInTheDocument();
    expect(screen.getByTestId('model-honor-list')).toBeInTheDocument();
    // Header + section chips both render.
    expect(screen.getAllByTestId('model-chip').length).toBeGreaterThanOrEqual(1);
    // The raw model row is suppressed; tools stays in Portable frontmatter.
    expect(screen.getByText('tools')).toBeInTheDocument();
    expect(screen.queryByText('model')).not.toBeInTheDocument();
    // Declared shows its source key, matching the skill inspector.
    expect(screen.getByText('(model)')).toBeInTheDocument();
  });

  it('falls back to the raw model frontmatter row without the typed view', () => {
    renderAgentPanel(AGENT);
    expect(screen.queryByText('Model preference')).not.toBeInTheDocument();
    expect(screen.queryByTestId('model-chip')).not.toBeInTheDocument();
    // Raw row present.
    expect(screen.getByText('model')).toBeInTheDocument();
    expect(screen.getByText('opus')).toBeInTheDocument();
  });
});
