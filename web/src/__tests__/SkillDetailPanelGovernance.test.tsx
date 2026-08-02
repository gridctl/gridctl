import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, fireEvent } from '@testing-library/react';
import { SkillDetailPanel } from '../components/registry/SkillDetailPanel';
import type { AgentSkill } from '../types';

vi.mock('../lib/api', () => ({
  fetchRegistrySkill: vi.fn(),
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

const SKILL: AgentSkill = {
  name: 'incident-triage',
  description: 'Triage incidents quickly',
  state: 'active',
  body: '# Triage',
  fileCount: 2,
  governance: {
    source: 'git',
    origin: { repo: 'https://github.com/acme/skills.git', ref: 'main', commitSha: 'abc123' },
    pinStatus: 'drift',
    findingsCount: 2,
    maxFindingSeverity: 'warn',
    policyDenied: true,
    policyRule: '*triage*',
  },
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

describe('SkillDetailPanel governance', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('shows the policy chip beside an unchanged state badge', () => {
    renderPanel();
    expect(screen.getByText('Blocked by policy')).toBeInTheDocument();
    // The lifecycle badge is untouched by the policy verdict.
    expect(screen.getByText('active')).toBeInTheDocument();
  });

  it('renders the governance section with factual origin and pin state', () => {
    renderPanel();
    expect(screen.getByText('Governance')).toBeInTheDocument();
    expect(
      screen.getByText('Imported: https://github.com/acme/skills.git@main'),
    ).toBeInTheDocument();
    expect(screen.getByText('Pin drift')).toBeInTheDocument();
    expect(screen.getByText('2 advisory findings (warn)')).toBeInTheDocument();
    expect(screen.getByText('Blocked by policy (rule: *triage*)')).toBeInTheDocument();
  });

  it('deep-links pin drift review through the callback prop', () => {
    const onOpenPinDrift = vi.fn();
    renderPanel({ onOpenPinDrift });
    fireEvent.click(screen.getByRole('button', { name: 'Review in Pins' }));
    expect(onOpenPinDrift).toHaveBeenCalledWith('incident-triage');
  });

  it('omits the governance section entirely for ungoverned skills', () => {
    renderPanel({ skill: { ...SKILL, governance: undefined } });
    expect(screen.queryByText('Governance')).not.toBeInTheDocument();
    expect(screen.queryByText('Blocked by policy')).not.toBeInTheDocument();
  });
});
