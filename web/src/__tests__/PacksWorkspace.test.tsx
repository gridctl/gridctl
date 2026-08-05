import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, cleanup, fireEvent, waitFor, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { CommandRegistryProvider } from '../hooks/useCommandRegistry';
import { PacksWorkspace } from '../components/registry/packs/PacksWorkspace';
import { PackImportWizard } from '../components/wizard/steps/PackImportWizard';
import { describeApplyDoc, groupPackRows, packNeedsAttention, sortPacks } from '../components/registry/packs/packModel';
import { isRegistryKind } from '../lib/registryKind';
import { StatePill } from '../components/ui/StatePill';
import { SourceGroupHeader } from '../components/registry/SourceGroupHeader';
import { useRegistryStore } from '../stores/useRegistryStore';
import { HTTPError } from '../lib/api';
import type { PackDetail, PackListItem, PackPreview } from '../lib/api';

vi.mock('../lib/api', async () => {
  const actual = await vi.importActual<typeof import('../lib/api')>('../lib/api');
  return {
    ...actual,
    fetchPacks: vi.fn(),
    fetchPackDetail: vi.fn(),
    addPack: vi.fn(),
    previewPack: vi.fn(),
    applyPack: vi.fn(),
    removePack: vi.fn(),
  };
});

import {
  addPack,
  applyPack,
  fetchPackDetail,
  fetchPacks,
  previewPack,
  removePack,
} from '../lib/api';

function listItem(overrides: Partial<PackListItem> = {}): PackListItem {
  return {
    name: 'team-pack',
    version: '1.0.0',
    description: 'Team conventions in one repo',
    origin: { source: 'team-pack', repo: 'https://github.com/acme/team-pack' },
    counts: { skills: 2, agents: 1, rules: 1, wiring: false },
    applied: true,
    needs_attention: false,
    ...overrides,
  };
}

const cleanDetail: PackDetail = {
  info: listItem(),
  rows: [
    { kind: 'skill', name: 'alpha', client: 'claude-code', state: 'in-sync' },
    { kind: 'rule', name: 'team-style', client: 'claude-code', state: 'in-sync' },
  ],
  needs_attention: false,
};

function renderWorkspace(url = '/library?kind=pack') {
  return render(
    <MemoryRouter initialEntries={[url]}>
      <CommandRegistryProvider>
        <PacksWorkspace onKindChange={() => {}} />
      </CommandRegistryProvider>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  cleanup();
  vi.clearAllMocks();
  useRegistryStore.setState({ packs: null });
  vi.mocked(fetchPacks).mockResolvedValue([]);
  vi.mocked(fetchPackDetail).mockResolvedValue(cleanDetail);
});

describe('registryKind', () => {
  it('accepts the three catalog kinds and nothing else', () => {
    expect(isRegistryKind('skill')).toBe(true);
    expect(isRegistryKind('agent')).toBe(true);
    expect(isRegistryKind('pack')).toBe(true);
    expect(isRegistryKind('bundle')).toBe(false);
    expect(isRegistryKind(null)).toBe(false);
  });
});

describe('packModel', () => {
  it('treats never-applied and colliding packs as attention', () => {
    expect(packNeedsAttention(listItem())).toBe(false);
    expect(packNeedsAttention(listItem({ applied: false }))).toBe(true);
    expect(packNeedsAttention(listItem({ collision: true }))).toBe(true);
    expect(packNeedsAttention(listItem({ needs_attention: true }))).toBe(true);
  });

  it('sorts attention-first, then by name', () => {
    const sorted = sortPacks([
      listItem({ name: 'zeta' }),
      listItem({ name: 'beta', applied: false }),
      listItem({ name: 'alpha' }),
    ]);
    expect(sorted.map((p) => p.name)).toEqual(['beta', 'alpha', 'zeta']);
  });

  it('groups rows by kind in manifest order, attention-first within groups', () => {
    const groups = groupPackRows([
      { kind: 'wiring', name: 'gridctl', client: 'claude', state: 'in-sync' },
      { kind: 'skill', name: 'ok', client: 'claude', state: 'in-sync' },
      { kind: 'skill', name: 'bad', client: 'claude', state: 'drifted' },
      { kind: 'unresolved', name: 'ghost', state: 'unresolved' },
    ]);
    expect(groups.map((g) => g.kind)).toEqual(['skill', 'wiring', 'unresolved']);
    expect(groups[0].rows.map((r) => r.name)).toEqual(['bad', 'ok']);
  });

  it('grades a partial apply as a warning naming skipped kinds, never success', () => {
    const outcome = describeApplyDoc({
      pack: 'team-pack',
      applied: 2,
      total: 4,
      rows: [
        { kind: 'skill', name: 'a', action: 'synced' },
        { kind: 'skill', name: 'b', action: 'skipped-drift' },
        { kind: 'agent', name: 'c', action: 'synced' },
        { kind: 'agent', name: 'd', action: 'skipped-foreign-pack' },
      ],
    });
    expect(outcome.kind).toBe('warning');
    expect(outcome.message).toContain('Applied 2/4');
    expect(outcome.message).toContain('1 skill');
    expect(outcome.driftedSkips).toHaveLength(1);
    expect(outcome.foreignSkips).toHaveLength(1);

    const clean = describeApplyDoc({
      pack: 'team-pack',
      applied: 3,
      total: 3,
      rows: [{ kind: 'skill', name: 'a', action: 'synced' }],
    });
    expect(clean.kind).toBe('success');
  });
});

describe('StatePill unresolved', () => {
  it('renders the amber unresolved state with its text as the accessible name', () => {
    render(<StatePill state="unresolved" />);
    expect(screen.getByText('unresolved')).toBeInTheDocument();
  });
});

describe('PacksWorkspace', () => {
  it('shows loading, never the empty teaching state, while packs are null', () => {
    vi.mocked(fetchPacks).mockReturnValue(new Promise(() => {}));
    renderWorkspace();
    expect(screen.getByText('Loading packs…')).toBeInTheDocument();
    expect(screen.queryByText('No packs imported')).not.toBeInTheDocument();
  });

  it('teaches the pack convention in the empty state', async () => {
    renderWorkspace();
    expect(await screen.findByText('No packs imported')).toBeInTheDocument();
    expect(screen.getByText(/gridctl-pack\.yaml/)).toBeInTheDocument();
    expect(screen.getByText(/portable-pack/)).toBeInTheDocument();
    expect(screen.getByText(/gridctl pack add/)).toBeInTheDocument();
  });

  it('lists packs attention-first with a not-applied badge', async () => {
    vi.mocked(fetchPacks).mockResolvedValue([
      listItem({ name: 'applied-pack' }),
      listItem({ name: 'new-pack', applied: false }),
    ]);
    renderWorkspace();
    const list = await screen.findByLabelText('Installed packs');
    const items = within(list).getAllByRole('button');
    expect(items[0]).toHaveTextContent('new-pack');
    expect(items[0]).toHaveTextContent('Not applied');
  });

  it('deep-links to a pack and toasts-and-clears on a miss', async () => {
    vi.mocked(fetchPacks).mockResolvedValue([listItem()]);
    renderWorkspace('/library?kind=pack&selected=ghost');
    await waitFor(() => {
      expect(fetchPacks).toHaveBeenCalled();
    });
    await waitFor(() => {
      expect(screen.queryByText('Loading packs…')).not.toBeInTheDocument();
    });
    // The miss clears the selection; the detail pane returns to its
    // empty prompt rather than loading "ghost".
    expect(await screen.findByText('Select a pack to inspect its resources.')).toBeInTheDocument();
  });

  it('renders detail rows with remediation text visible', async () => {
    vi.mocked(fetchPacks).mockResolvedValue([listItem()]);
    vi.mocked(fetchPackDetail).mockResolvedValue({
      info: listItem(),
      rows: [
        {
          kind: 'skill',
          name: 'alpha',
          client: 'claude-code',
          state: 'drifted',
          detail: 'hand-edited since sync',
          remediation: 'adopt the edit or force overwrite',
        },
        { kind: 'rule', name: 'team-style', client: 'claude-code', state: 'stale' },
        { kind: 'unresolved', name: 'ghost', state: 'unresolved' },
      ],
      needs_attention: true,
    });
    renderWorkspace('/library?kind=pack&selected=team-pack');

    expect(await screen.findByText('Adopt the edit or force overwrite')).toBeInTheDocument();
    expect(screen.getByText('unresolved')).toBeInTheDocument();
    // Rule rows render as text, not links.
    expect(screen.getByText('team-style').closest('button')).toBeNull();
    // Skill rows deep-link.
    expect(screen.getByTitle('Open alpha')).toBeInTheDocument();
  });

  it('renders a collision 409 as a detail banner naming both repos', async () => {
    vi.mocked(fetchPacks).mockResolvedValue([listItem({ collision: true })]);
    vi.mocked(fetchPackDetail).mockRejectedValue(
      new HTTPError(409, 'pack name "team-pack" is claimed by multiple sources (repoA, repoB)'),
    );
    renderWorkspace('/library?kind=pack&selected=team-pack');

    expect(await screen.findByText('Pack name collision')).toBeInTheDocument();
    expect(screen.getByText(/repoA, repoB/)).toBeInTheDocument();
  });

  it('applies with a graded toast and offers the force follow-up on drift skips', async () => {
    vi.mocked(fetchPacks).mockResolvedValue([listItem()]);
    vi.mocked(applyPack).mockResolvedValue({
      pack: 'team-pack',
      applied: 1,
      total: 2,
      rows: [
        { kind: 'skill', name: 'alpha', client: 'claude-code', action: 'synced' },
        { kind: 'skill', name: 'beta', client: 'claude-code', action: 'skipped-drift' },
      ],
    });
    renderWorkspace('/library?kind=pack&selected=team-pack');
    fireEvent.click(await screen.findByText('Apply'));

    // The force follow-up dialog names the drifted skip; overwriting
    // re-posts with force.
    expect(await screen.findByText('Some resources kept local edits')).toBeInTheDocument();
    fireEvent.click(screen.getByText('Overwrite local edits'));
    await waitFor(() => {
      expect(applyPack).toHaveBeenCalledWith('team-pack', { force: true });
    });
  });

  it('previews the remove cascade with two lists and force as a separate confirm', async () => {
    vi.mocked(fetchPacks).mockResolvedValue([listItem()]);
    vi.mocked(removePack).mockResolvedValue({
      pack: 'team-pack',
      dry_run: true,
      rows: [
        { kind: 'skill', name: 'alpha', action: 'would-remove' },
        {
          kind: 'agent',
          name: 'reviewer',
          action: 'skipped-drift',
          remediation: "keep the edit with 'adopt' before removing, or force removal",
        },
      ],
    });
    renderWorkspace('/library?kind=pack&selected=team-pack');
    fireEvent.click(await screen.findByText('Remove'));

    expect(await screen.findByText(/1 resource(s)? will be removed/)).toBeInTheDocument();
    expect(screen.getByText(/1 kept \(locally edited\)/)).toBeInTheDocument();
    expect(screen.getByText(/keep the edit with 'adopt'/)).toBeInTheDocument();

    // Force is behind its own confirm, never the default action.
    fireEvent.click(screen.getByText('Remove and overwrite local edits'));
    expect(await screen.findByText(/deletes the locally edited projections too/)).toBeInTheDocument();

    // The plain remove keeps edits; a trimmed record must not toast "removed".
    vi.mocked(removePack).mockResolvedValue({
      pack: 'team-pack',
      rows: [{ kind: 'skill', name: 'alpha', action: 'removed' }],
      kept: ['agent/reviewer'],
    });
    fireEvent.click(screen.getByText('Back'));
    fireEvent.click(screen.getByText('Remove (keep local edits)'));
    await waitFor(() => {
      expect(removePack).toHaveBeenCalledWith('team-pack', undefined);
    });
  });
});

describe('PackImportWizard', () => {
  const previewResult: PackPreview = {
    pack: 'team-pack',
    version: '1.0.0',
    wiring: false,
    skills: [{ kind: 'skill', name: 'alpha' }],
    agents: [{ kind: 'agent', name: 'reviewer' }],
    rules: [
      {
        kind: 'rule',
        name: 'danger',
        findings: [{ description: 'piped shell download' }],
        blocking: true,
      },
    ],
    unresolved: ['ghost'],
  };

  function renderWizard() {
    return render(
      <MemoryRouter>
        <PackImportWizard />
      </MemoryRouter>,
    );
  }

  it('previews, gates on the pack-wide trust ack, and installs', async () => {
    vi.mocked(previewPack).mockResolvedValue(previewResult);
    vi.mocked(addPack).mockResolvedValue({
      doc: { pack: 'team-pack', skills: ['alpha'], agents: ['reviewer'], wiring: false },
      notes: [],
    });
    renderWizard();

    fireEvent.change(screen.getByLabelText('Pack repository URL'), {
      target: { value: 'https://github.com/acme/team-pack' },
    });
    fireEvent.click(screen.getByText('Preview pack'));

    // Read-only review: resolved names, unresolved called out, no checkboxes
    // besides the single trust ack.
    await screen.findByText(/The manifest selects these resources/);
    expect(previewPack).toHaveBeenCalledWith({ repo: 'https://github.com/acme/team-pack' });
    expect(screen.getByText(/ghost/)).toBeInTheDocument();
    expect(screen.getByText('piped shell download')).toBeInTheDocument();

    // The trust gate blocks install until acknowledged.
    const install = screen.getByText('Import pack');
    expect(install.closest('button')).toBeDisabled();
    fireEvent.click(screen.getByRole('checkbox'));
    expect(install.closest('button')).not.toBeDisabled();

    fireEvent.click(install);
    await waitFor(() => {
      expect(addPack).toHaveBeenCalledWith({
        repo: 'https://github.com/acme/team-pack',
        ref: undefined,
        path: undefined,
        trust: true,
      });
    });

    // Success step offers Apply now and View pack, Apply emphasized.
    expect(await screen.findByText('Pack imported')).toBeInTheDocument();
    expect(screen.getByText('Apply now')).toBeInTheDocument();
    expect(screen.getByText('View pack')).toBeInTheDocument();
  });

  it('applies from the success step with one extra POST', async () => {
    vi.mocked(previewPack).mockResolvedValue({ ...previewResult, rules: [] });
    vi.mocked(addPack).mockResolvedValue({
      doc: { pack: 'team-pack', skills: ['alpha'], agents: [], wiring: false },
      notes: [],
    });
    vi.mocked(applyPack).mockResolvedValue({
      pack: 'team-pack',
      applied: 1,
      total: 1,
      rows: [{ kind: 'skill', name: 'alpha', action: 'synced' }],
    });
    renderWizard();

    fireEvent.change(screen.getByLabelText('Pack repository URL'), {
      target: { value: 'https://github.com/acme/team-pack' },
    });
    fireEvent.click(screen.getByText('Preview pack'));
    fireEvent.click(await screen.findByText('Import pack'));
    fireEvent.click(await screen.findByText('Apply now'));

    await waitFor(() => {
      expect(applyPack).toHaveBeenCalledWith('team-pack');
    });
  });

  it('renders the manifest-missing 422 message inline', async () => {
    vi.mocked(previewPack).mockRejectedValue(
      new HTTPError(422, 'no gridctl-pack.yaml found at the repository root'),
    );
    renderWizard();
    fireEvent.change(screen.getByLabelText('Pack repository URL'), {
      target: { value: 'https://github.com/acme/not-a-pack' },
    });
    fireEvent.click(screen.getByText('Preview pack'));

    expect(await screen.findByRole('alert')).toHaveTextContent('no gridctl-pack.yaml');
  });
});

describe('reverse ownership chip', () => {
  const source = {
    name: 'team-pack',
    repo: 'https://github.com/acme/team-pack',
    skillCount: 2,
  } as never;

  it('links a pack-owned source group to the pack detail', () => {
    useRegistryStore.setState({ packs: [listItem()] });
    render(
      <MemoryRouter>
        <SourceGroupHeader source={source} count={2} hasSearch={false} isActive={false} onToggle={() => {}} />
      </MemoryRouter>,
    );
    expect(screen.getByText('pack: team-pack')).toBeInTheDocument();
  });

  it('renders no chip when the source has no pack (or packs are unloaded)', () => {
    useRegistryStore.setState({ packs: null });
    render(
      <MemoryRouter>
        <SourceGroupHeader source={source} count={2} hasSearch={false} isActive={false} onToggle={() => {}} />
      </MemoryRouter>,
    );
    expect(screen.queryByText(/^pack:/)).not.toBeInTheDocument();
  });
});
