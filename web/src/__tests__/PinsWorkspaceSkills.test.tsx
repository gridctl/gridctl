import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import '@testing-library/jest-dom';
import { MemoryRouter, useLocation } from 'react-router';
import { CommandRegistryProvider } from '../hooks/useCommandRegistry';
import { PinsWorkspace } from '../components/workspaces/PinsWorkspace';
import { usePinsStore } from '../stores/usePinsStore';
import { useUIStore } from '../stores/useUIStore';
import * as api from '../lib/api';
import { HTTPError, type ServerPins, type SkillPin, type SkillPinsDiff } from '../lib/api';

vi.mock('../components/ui/Toast', () => ({ showToast: vi.fn(), copyWithToast: vi.fn() }));

function serverPins(status: ServerPins['status']): ServerPins {
  return {
    server_hash: 'h2:abc',
    pinned_at: '2026-07-01T00:00:00Z',
    last_verified_at: '2026-07-15T00:00:00Z',
    tool_count: 0,
    status,
    tools: {},
  };
}

function skillPin(status: SkillPin['status'], overrides: Partial<SkillPin> = {}): SkillPin {
  return {
    skill_hash: 's1:abc',
    files: [{ path: 'scripts/run.sh', digest: 's1:def' }],
    source: 'local',
    pinned_at: '2026-07-01T00:00:00Z',
    last_verified_at: '2026-07-15T00:00:00Z',
    status,
    ...overrides,
  };
}

const triageDiff: SkillPinsDiff = {
  skill: 'incident-triage',
  status: 'drift',
  composite_hash: 'reviewed-composite',
  old_document: '---\nname: incident-triage\n---\n\nOld body line.\n',
  new_document: '---\nname: incident-triage\n---\n\nNew body line.\n',
  added_files: ['references/new.md'],
  removed_files: [],
  modified_files: ['scripts/run.sh'],
  findings: [],
};

// Rendered into the DOM (not a module variable) so react-hooks/globals
// stays happy; tests read it via the location testid.
function LocationProbe() {
  const location = useLocation();
  return <div data-testid="location">{location.pathname + location.search}</div>;
}

function lastLocation(): string {
  return screen.getByTestId('location').textContent ?? '';
}

function renderWorkspace(initialEntry = '/pins') {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <CommandRegistryProvider>
        <PinsWorkspace />
        <LocationProbe />
      </CommandRegistryProvider>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  usePinsStore.setState({
    pins: { github: serverPins('pinned') },
    skillPins: {
      'incident-triage': skillPin('drift'),
      'release-notes': skillPin('pinned'),
    },
  });
  vi.spyOn(api, 'fetchSkillPinDiff').mockResolvedValue(triageDiff);
});

afterEach(() => {
  usePinsStore.setState({ pins: null, skillPins: null });
  useUIStore.setState({ pinsPrefs: { attentionOnly: null, findingsOnly: false } });
  vi.restoreAllMocks();
});

describe('PinsWorkspace skills kind', () => {
  it('toggles to the skill kind and lists skills drift-first', async () => {
    renderWorkspace();

    fireEvent.click(screen.getByRole('button', { name: 'Skills' }));

    await waitFor(() => {
      expect(lastLocation()).toContain('kind=skill');
    });
    const rows = screen.getAllByRole('button', { name: /^(incident-triage|release-notes)/ });
    expect(within(rows[0]).getByText('incident-triage')).toBeInTheDocument();
    expect(screen.getByText(/2 skills pinned · 1 drifted/)).toBeInTheDocument();
  });

  it('lands on a deep link without rewriting it to the server kind', async () => {
    renderWorkspace('/pins?kind=skill&skill=incident-triage&view=drift');

    // Both the detail title and the drift section carry the "Pin drift"
    // wording; either proves the skill pane landed.
    expect((await screen.findAllByText('Pin drift')).length).toBeGreaterThan(0);
    expect(lastLocation()).toContain('kind=skill');
    expect(lastLocation()).toContain('skill=incident-triage');
    expect(lastLocation()).not.toContain('server=');
  });

  it('renders the prose diff, file summary, and approves with the composite hash', async () => {
    const approve = vi.spyOn(api, 'approveSkillPin').mockResolvedValue(undefined);
    vi.spyOn(api, 'fetchSkillPins').mockResolvedValue({
      'incident-triage': skillPin('pinned'),
      'release-notes': skillPin('pinned'),
    });
    renderWorkspace('/pins?kind=skill&skill=incident-triage');

    // Semantic summary + per-file change list + prose diff lines.
    expect(
      await screen.findByText('SKILL.md changed · 1 supporting file added · 1 supporting file modified'),
    ).toBeInTheDocument();
    expect(screen.getByText('references/new.md')).toBeInTheDocument();
    expect(screen.getByText(/Old body line\./)).toBeInTheDocument();
    expect(screen.getByText(/New body line\./)).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Approve' }));
    await waitFor(() => {
      expect(approve).toHaveBeenCalledWith('incident-triage', 'reviewed-composite', undefined);
    });
  });

  it('requires a reason before approving over advisory findings', async () => {
    vi.spyOn(api, 'fetchSkillPinDiff').mockResolvedValue({
      ...triageDiff,
      findings: [
        {
          code: 'P001',
          severity: 'warn',
          confidence: 'high',
          field: 'body',
          message: 'hidden-instruction phrasing',
        },
      ],
    });
    const approve = vi.spyOn(api, 'approveSkillPin').mockResolvedValue(undefined);
    vi.spyOn(api, 'fetchSkillPins').mockResolvedValue({});
    renderWorkspace('/pins?kind=skill&skill=incident-triage');

    const approveBtn = await screen.findByRole('button', { name: 'Approve' });
    expect(approveBtn).toBeDisabled();

    fireEvent.change(screen.getByLabelText('Approval reason'), {
      target: { value: 'training material' },
    });
    expect(approveBtn).toBeEnabled();
    fireEvent.click(approveBtn);
    await waitFor(() => {
      expect(approve).toHaveBeenCalledWith(
        'incident-triage',
        'reviewed-composite',
        'training material',
      );
    });
  });

  it('reloads the diff for re-review on a stale-hash 409', async () => {
    const fetchDiff = vi.spyOn(api, 'fetchSkillPinDiff').mockResolvedValue(triageDiff);
    vi.spyOn(api, 'approveSkillPin').mockRejectedValue(
      new HTTPError(409, 'Skill content changed since the reviewed diff'),
    );
    renderWorkspace('/pins?kind=skill&skill=incident-triage');

    fireEvent.click(await screen.findByRole('button', { name: 'Approve' }));
    await waitFor(() => {
      // Initial load + the forced re-review reload.
      expect(fetchDiff).toHaveBeenCalledTimes(2);
    });
  });

  it('resets a skill pin through the confirm dialog', async () => {
    const reset = vi.spyOn(api, 'resetSkillPin').mockResolvedValue(undefined);
    vi.spyOn(api, 'fetchSkillPins').mockResolvedValue({});
    renderWorkspace('/pins?kind=skill&skill=release-notes');

    fireEvent.click(await screen.findByRole('button', { name: 'Reset pin for release-notes' }));
    // ConfirmDialog's confirm button carries the same label.
    const dialogs = screen.getAllByRole('button', { name: 'Reset pin for release-notes' });
    fireEvent.click(dialogs[dialogs.length - 1]);
    await waitFor(() => {
      expect(reset).toHaveBeenCalledWith('release-notes');
    });
  });

  it('groups shared-origin drifted skills under a source header', () => {
    usePinsStore.setState({
      skillPins: {
        alpha: skillPin('drift', {
          source: 'git',
          origin: { repo: 'https://github.com/acme/skills.git', ref: 'main' },
        }),
        beta: skillPin('drift', {
          source: 'git',
          origin: { repo: 'https://github.com/acme/skills.git', ref: 'main' },
        }),
        gamma: skillPin('drift'),
      },
    });
    renderWorkspace('/pins?kind=skill');

    expect(screen.getByTitle('2 drifted skills from this source')).toBeInTheDocument();
    expect(screen.getByText('https://github.com/acme/skills.git')).toBeInTheDocument();
  });

  it('keeps the server kind untouched by default', () => {
    renderWorkspace();
    expect(screen.getByText(/1 server pinned/)).toBeInTheDocument();
    expect(lastLocation()).not.toContain('kind=');
  });
});
