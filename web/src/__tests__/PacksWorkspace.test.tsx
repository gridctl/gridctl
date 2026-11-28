import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useEffect } from 'react';
import '@testing-library/jest-dom';
import { render, screen, cleanup, fireEvent, waitFor, within } from '@testing-library/react';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router';
import { CommandRegistryProvider } from '../hooks/useCommandRegistry';
import { PacksWorkspace } from '../components/registry/packs/PacksWorkspace';
import { PackImportWizard } from '../components/wizard/steps/PackImportWizard';
import { describeApplyDoc, groupPackRows, packNeedsAttention, rowNeedsAttention, sortPacks } from '../components/registry/packs/packModel';
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

const visited: string[] = [];

// The still-mounted PacksWorkspace clears a selected name it cannot find, so
// the probe records every location rather than only the last one.
function LocationProbe() {
  const location = useLocation();
  const url = location.pathname + location.search;
  useEffect(() => {
    visited.push(url);
  }, [url]);
  return null;
}

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
  it('accepts the six catalog kinds and nothing else', () => {
    expect(isRegistryKind('skill')).toBe(true);
    expect(isRegistryKind('agent')).toBe(true);
    expect(isRegistryKind('hook')).toBe(true);
    expect(isRegistryKind('pack')).toBe(true);
    expect(isRegistryKind('context')).toBe(true);
    expect(isRegistryKind('proposal')).toBe(true);
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

  it('treats started and replaced as clean, and skipped stack states as attention', () => {
    expect(rowNeedsAttention({ kind: 'stack', name: 'neteng', action: 'started' })).toBe(false);
    expect(rowNeedsAttention({ kind: 'stack', name: 'neteng', action: 'replaced' })).toBe(false);
    expect(rowNeedsAttention({ kind: 'stack', name: 'neteng', action: 'would-start' })).toBe(false);
    expect(rowNeedsAttention({ kind: 'stack', name: 'neteng', action: 'would-replace' })).toBe(false);
    expect(rowNeedsAttention({ kind: 'stack', name: 'neteng', action: 'skipped-variables' })).toBe(true);
    expect(rowNeedsAttention({ kind: 'stack', name: 'neteng', action: 'skipped-running' })).toBe(true);
    expect(rowNeedsAttention({ kind: 'stack', name: 'neteng', action: 'skipped-unavailable' })).toBe(true);
    expect(rowNeedsAttention({ kind: 'stack', name: 'neteng', state: 'stale' })).toBe(true);
    expect(rowNeedsAttention({ kind: 'stack', name: 'neteng', state: 'drifted' })).toBe(true);
    expect(rowNeedsAttention({ kind: 'stack', name: 'neteng', state: 'target-missing' })).toBe(true);
    expect(rowNeedsAttention({ kind: 'stack', name: 'neteng', state: 'missing' })).toBe(false);
  });

  it('groups a stack row first', () => {
    const groups = groupPackRows([
      { kind: 'skill', name: 'ok', client: 'claude', state: 'in-sync' },
      { kind: 'stack', name: 'neteng', state: 'in-sync', remediation: "re-run 'gridctl pack apply neteng'" },
    ]);
    expect(groups.map((g) => g.kind)).toEqual(['stack', 'skill']);
    expect(groups[0].label).toBe('Stack');
    expect(groups[0].rows[0].remediation).toContain('gridctl pack apply');
  });

  it('groups fragment and lock rows between the stack and sources', () => {
    const groups = groupPackRows([
      { kind: 'source', name: 'netops', state: 'in-sync', detail: 'https://example.com/netops (abc)' },
      { kind: 'lock', name: 'gridctl-pack.lock.yaml', state: 'honored' },
      { kind: 'fragment', name: 'netops-team', state: 'in-sync', detail: 'servers.yaml (abc)', remediation: "re-run 'gridctl pack apply infra'" },
      { kind: 'stack', name: 'infra', state: 'in-sync' },
    ]);
    expect(groups.map((g) => g.kind)).toEqual(['stack', 'fragment', 'lock', 'source']);
    expect(groups[1].rows[0].detail).toBe('servers.yaml (abc)');
    expect(groups[1].rows[0].remediation).toContain('gridctl pack apply');
    expect(groups[2].rows[0].name).toBe('gridctl-pack.lock.yaml');
  });

  it('treats a missing fragment checkout as attention', () => {
    expect(rowNeedsAttention({ kind: 'fragment', name: 'netops-team', state: 'missing' })).toBe(true);
    expect(rowNeedsAttention({ kind: 'lock', name: 'gridctl-pack.lock.yaml', state: 'stale' })).toBe(true);
    expect(rowNeedsAttention({ kind: 'lock', name: 'gridctl-pack.lock.yaml', state: 'honored' })).toBe(false);
  });

  it('groups a source row after the stack row and before skills', () => {
    const groups = groupPackRows([
      { kind: 'skill', name: 'ok', client: 'claude', state: 'in-sync' },
      { kind: 'source', name: 'netops', state: 'in-sync', detail: 'https://example.com/netops@v1 (abc1234)' },
      { kind: 'stack', name: 'neteng', state: 'in-sync' },
    ]);
    expect(groups.map((g) => g.kind)).toEqual(['stack', 'source', 'skill']);
    expect(groups[1].label).toBe('Sources');
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

  it('groups hook rows after rules and before wiring, labeled Hooks', () => {
    const groups = groupPackRows([
      { kind: 'wiring', name: 'gridctl', client: 'claude', state: 'in-sync' },
      { kind: 'hook', name: 'block-force-push', client: 'claude-code', state: 'in-sync' },
      { kind: 'rule', name: 'team-style', client: 'claude-code', state: 'in-sync' },
      { kind: 'skill', name: 'ok', client: 'claude', state: 'in-sync' },
    ]);
    expect(groups.map((g) => g.kind)).toEqual(['skill', 'rule', 'hook', 'wiring']);
    expect(groups[2].label).toBe('Hooks');
  });

  it('treats a missing hook as attention and an in-sync hook as clean', () => {
    expect(rowNeedsAttention({ kind: 'hook', name: 'x', state: 'missing' })).toBe(true);
    expect(rowNeedsAttention({ kind: 'hook', name: 'x', state: 'in-sync' })).toBe(false);
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
    expect(screen.queryAllByRole('main')).toHaveLength(0);
    expect(screen.getByRole('heading', { level: 1, name: 'Library' })).toBeInTheDocument();
    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1);
    expect(screen.getByRole('heading', { level: 1, name: 'Library' }).className).not.toMatch(/sr-only|uppercase|tracking-/);
    expect(screen.queryByTestId('workspace-header-summary')).toBeNull();
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

  it('links a hook row to the hook and leaves a missing hook as text', async () => {
    visited.length = 0;
    vi.mocked(fetchPacks).mockResolvedValue([listItem()]);
    vi.mocked(fetchPackDetail).mockResolvedValue({
      info: listItem(),
      rows: [
        { kind: 'hook', name: 'block force/push', client: 'claude-code', state: 'in-sync' },
        { kind: 'hook', name: 'gone-hook', state: 'missing', detail: 'registry directory is gone' },
      ],
      needs_attention: true,
    });
    render(
      <MemoryRouter initialEntries={['/library?kind=pack&selected=team-pack']}>
        <CommandRegistryProvider>
          <PacksWorkspace onKindChange={() => {}} />
          <Routes>
            <Route path="*" element={<LocationProbe />} />
          </Routes>
        </CommandRegistryProvider>
      </MemoryRouter>,
    );

    expect(await screen.findByText('Hooks')).toBeInTheDocument();
    expect(screen.getByText('gone-hook').closest('button')).toBeNull();
    fireEvent.click(screen.getByTitle('Open block force/push'));
    expect(visited).toContain(`/library?kind=hook&selected=${encodeURIComponent('block force/push')}`);
  });

  it('counts hooks on the pack card', async () => {
    vi.mocked(fetchPacks).mockResolvedValue([
      listItem({ name: 'one-hook', counts: { skills: 2, agents: 1, rules: 1, hooks: 1, wiring: true } }),
      listItem({ name: 'two-hooks', counts: { skills: 0, agents: 0, rules: 0, hooks: 2, wiring: false } }),
      listItem({ name: 'zero-hooks', counts: { skills: 1, agents: 0, rules: 0, hooks: 0, wiring: false } }),
      listItem({ name: 'no-hooks', counts: { skills: 1, agents: 0, rules: 0, wiring: false } }),
    ]);
    renderWorkspace();
    const list = await screen.findByLabelText('Installed packs');
    const card = (name: string) => within(list).getAllByRole('button').find((b) => b.textContent?.includes(name))!;
    expect(card('one-hook')).toHaveTextContent('2 skills · 1 agent · 1 rule · 1 hook · wiring');
    expect(card('two-hooks')).toHaveTextContent('2 hooks');
    expect(card('zero-hooks')).toHaveTextContent('1 skill');
    expect(card('zero-hooks').textContent).not.toMatch(/\d hooks?/);
    expect(card('no-hooks').textContent).not.toMatch(/\d hooks?/);
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

describe('PacksWorkspace load failures', () => {
  it('shows a failed state with Retry when nothing is loaded, then recovers', async () => {
    vi.mocked(fetchPacks).mockRejectedValueOnce(new Error('offline'));
    renderWorkspace();

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('Packs unavailable');
    expect(alert).toHaveTextContent('Load packs failed: offline.');
    expect(screen.queryByText('Loading packs…')).not.toBeInTheDocument();
    expect(screen.queryByText('No packs imported')).not.toBeInTheDocument();

    vi.mocked(fetchPacks).mockResolvedValue([listItem()]);
    fireEvent.click(within(alert).getByRole('button', { name: 'Retry' }));
    expect(await screen.findByLabelText('Installed packs')).toBeInTheDocument();
    expect(screen.queryByText('Packs unavailable')).not.toBeInTheDocument();
  });

  it('keeps cached packs under a banner and does not clear a deep link', async () => {
    useRegistryStore.setState({ packs: [listItem()] });
    vi.mocked(fetchPacks).mockRejectedValue(new Error('offline'));
    renderWorkspace('/library?kind=pack&selected=team-pack');

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('Load packs failed: offline.');
    expect(within(alert).getByRole('button', { name: 'Retry' })).toBeInTheDocument();
    const list = screen.getByLabelText('Installed packs');
    expect(within(list).getByText('team-pack')).toBeInTheDocument();
    // The selection survives: the detail pane loads the pack instead of
    // returning to its empty prompt.
    expect(await screen.findByText('team-style')).toBeInTheDocument();
    expect(screen.queryByText('Select a pack to inspect its resources.')).not.toBeInTheDocument();
  });

  it('shows a failed pack detail with Retry on a non-409 error', async () => {
    vi.mocked(fetchPacks).mockResolvedValue([listItem()]);
    vi.mocked(fetchPackDetail).mockRejectedValueOnce(new HTTPError(500, 'boom'));
    renderWorkspace('/library?kind=pack&selected=team-pack');

    const failed = await screen.findByText('Pack unavailable');
    const alert = failed.closest('[role="alert"]') as HTMLElement;
    expect(alert).toHaveTextContent('Load pack failed: boom.');
    expect(screen.queryByText('Loading pack…')).not.toBeInTheDocument();
    expect(screen.queryByText('Pack name collision')).not.toBeInTheDocument();

    fireEvent.click(within(alert).getByRole('button', { name: 'Retry' }));
    expect(await screen.findByText('team-style')).toBeInTheDocument();
    expect(screen.queryByText('Pack unavailable')).not.toBeInTheDocument();
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

  it('escapes hidden characters in finding descriptions', async () => {
    vi.mocked(previewPack).mockResolvedValue({
      ...previewResult,
      rules: [],
      hooks: [
        {
          kind: 'hook',
          name: 'block-force-push',
          findings: [{ description: 'piped\u200bshell download' }],
        },
      ],
    });
    renderWizard();
    fireEvent.change(screen.getByLabelText('Pack repository URL'), {
      target: { value: 'https://github.com/acme/team-pack' },
    });
    fireEvent.click(screen.getByText('Preview pack'));
    expect(await screen.findByText(/piped\\u200bshell download/)).toBeInTheDocument();
    expect(document.body.textContent).not.toContain('\u200b');
  });

  it('renders findings through the shared detailed findings block', async () => {
    vi.mocked(previewPack).mockResolvedValue({
      ...previewResult,
      rules: [
        {
          kind: 'rule',
          name: 'danger',
          blocking: true,
          findings: [
            { stepId: 'danger.md:line-4', pattern: 'curl.*sh', description: 'piped shell download', severity: 'danger' },
          ],
        },
      ],
    });
    renderWizard();
    fireEvent.change(screen.getByLabelText('Pack repository URL'), {
      target: { value: 'https://github.com/acme/team-pack' },
    });
    fireEvent.click(screen.getByText('Preview pack'));

    const group = await screen.findByRole('group', { name: 'Security findings' });
    const list = within(group).getByLabelText('Security findings on rule/danger');
    expect(list).toHaveTextContent('piped shell download');
    expect(list).toHaveTextContent('danger.md:line-4');
    expect(list).not.toHaveTextContent('curl');
    expect(within(list).getByRole('listitem')).toHaveClass('text-status-error');
    expect(within(group).getByText('blocks import')).toBeInTheDocument();
    expect(within(group).getByRole('checkbox')).not.toBeChecked();
  });

  it('labels a resource that came from an external source', async () => {
    vi.mocked(previewPack).mockResolvedValue({
      ...previewResult,
      rules: [],
      skills: [{ kind: 'skill', name: 'alpha', source: 'netops' }],
    });
    renderWizard();
    fireEvent.change(screen.getByLabelText('Pack repository URL'), {
      target: { value: 'https://github.com/acme/team-pack' },
    });
    fireEvent.click(screen.getByText('Preview pack'));
    expect(await screen.findByText(/alpha from netops/)).toBeInTheDocument();
  });

  it('shows the lock state on the review step', async () => {
    vi.mocked(previewPack).mockResolvedValue({
      ...previewResult,
      rules: [],
      lock: { state: 'honored' },
      sources: [{ name: 'netops', repo: 'https://example.com/netops.git', skills: [], agents: [] }],
    });
    renderWizard();
    fireEvent.change(screen.getByLabelText('Pack repository URL'), {
      target: { value: 'https://github.com/acme/team-pack' },
    });
    fireEvent.click(screen.getByText('Preview pack'));
    expect(await screen.findByText('Lock: honored')).toBeInTheDocument();
  });

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

  it('lists held hooks on the done step instead of a clean import', async () => {
    vi.mocked(previewPack).mockResolvedValue({ ...previewResult, rules: [] });
    vi.mocked(addPack).mockResolvedValue({
      doc: {
        pack: 'team-pack',
        skills: ['alpha'],
        agents: [],
        wiring: false,
        held_hooks: [{ name: 'block-force-push', composite_hash: 'sha256:abc' }],
      },
      notes: [],
    });
    renderWizard();

    fireEvent.change(screen.getByLabelText('Pack repository URL'), {
      target: { value: 'https://github.com/acme/team-pack' },
    });
    fireEvent.click(screen.getByText('Preview pack'));
    fireEvent.click(await screen.findByText('Import pack'));

    expect(await screen.findByText('Pack partially imported')).toBeInTheDocument();
    expect(screen.getByText(/Held for pin approval: block-force-push/)).toBeInTheDocument();
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
    expect(screen.getByRole('button', { name: 'pack: team-pack' })).toBeInTheDocument();
  });

  it('labels a pack member source even when the packs list is unloaded', () => {
    useRegistryStore.setState({ packs: null });
    render(
      <MemoryRouter>
        <SourceGroupHeader
          source={{ name: 'team-pack/netops', repo: 'https://github.com/acme/netops', packMember: 'team-pack' } as never}
          count={2}
          hasSearch={false}
          isActive={false}
          onToggle={() => {}}
        />
      </MemoryRouter>,
    );
    expect(screen.getByRole('button', { name: 'pack: team-pack' })).toBeInTheDocument();
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
