import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, cleanup, fireEvent, waitFor, within } from '@testing-library/react';
import { MemoryRouter, useLocation } from 'react-router';
import { CommandRegistryProvider } from '../hooks/useCommandRegistry';
import { AgentsWorkspace } from '../components/registry/agents/AgentsWorkspace';
import { AgentProjectionRows } from '../components/registry/agents/AgentProjectionRows';
import { AgentEditor } from '../components/registry/agents/AgentEditor';
import { describeSyncResults } from '../components/registry/agents/agentModel';
import { useRegistryStore } from '../stores/useRegistryStore';
import { AgentScanError } from '../lib/api';
import type { AgentProjectionStatus, RegistryAgent } from '../types';

vi.mock('../lib/api', async () => {
  const actual = await vi.importActual<typeof import('../lib/api')>('../lib/api');
  return {
    ...actual,
    fetchRegistryAgents: vi.fn(),
    fetchRegistryAgent: vi.fn(),
    updateRegistryAgent: vi.fn(),
    deleteRegistryAgent: vi.fn(),
    fetchAgentProjectionStatus: vi.fn(),
    syncAgentProjections: vi.fn(),
    unsyncAgentProjections: vi.fn(),
    adoptAgentProjection: vi.fn(),
  };
});

import {
  adoptAgentProjection,
  fetchAgentProjectionStatus,
  fetchRegistryAgent,
  fetchRegistryAgents,
  syncAgentProjections,
  updateRegistryAgent,
} from '../lib/api';

const reviewer: RegistryAgent = {
  name: 'code-reviewer',
  description: 'Reviews code for style and correctness',
  source: 'team-agents',
  extra: [
    { key: 'tools', value: 'Read, Grep' },
    { key: 'model', value: 'sonnet' },
    { key: 'x-vendor', value: { nested: true } },
  ],
  dir: '/home/u/.gridctl/registry/agents/code-reviewer',
};

const localAgent: RegistryAgent = {
  name: 'local-helper',
  description: 'A locally kept agent',
};

const identityInSync: AgentProjectionStatus = {
  agent: 'code-reviewer',
  client: 'claude-code',
  channel: 'copy',
  target: '/home/u/.claude/agents/code-reviewer.md',
  render: 'identity',
  state: 'in-sync',
  experimental: true,
};

const lossyDrifted: AgentProjectionStatus = {
  agent: 'code-reviewer',
  client: 'opencode',
  channel: 'copy',
  target: '/home/u/.config/opencode/agents/code-reviewer.md',
  render: 'lossy',
  state: 'drifted',
  detail: 'dropped keys: tools, model',
  experimental: true,
};

function renderWorkspace() {
  return render(
    <MemoryRouter initialEntries={['/library?kind=agent']}>
      <CommandRegistryProvider>
        <AgentsWorkspace onKindChange={() => {}} />
      </CommandRegistryProvider>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  cleanup();
  vi.clearAllMocks();
  useRegistryStore.setState({ agents: null, agentStatuses: null });
});

describe('AgentsWorkspace', () => {
  it('renders the KPI strip at zero with the empty state and import CTA', async () => {
    vi.mocked(fetchRegistryAgents).mockResolvedValue([]);
    vi.mocked(fetchAgentProjectionStatus).mockResolvedValue([]);

    renderWorkspace();

    await waitFor(() => {
      expect(screen.getByText('No agents imported')).toBeInTheDocument();
    });
    // KPI strip stays visible at zero — it is the signal that agents
    // support exists on a fresh install.
    const strip = screen.getByRole('group', { name: 'Agent projection summary' });
    expect(within(strip).getByText('Total')).toBeInTheDocument();
    expect(within(strip).getAllByText('0').length).toBeGreaterThan(0);
    expect(screen.getByRole('button', { name: /Import from git/i })).toBeInTheDocument();
  });

  it('lists agents grouped by source with projection chips and updates KPIs', async () => {
    vi.mocked(fetchRegistryAgents).mockResolvedValue([reviewer, localAgent]);
    vi.mocked(fetchAgentProjectionStatus).mockResolvedValue([identityInSync, lossyDrifted]);

    renderWorkspace();

    await waitFor(() => {
      expect(screen.getByText('code-reviewer')).toBeInTheDocument();
    });
    // Source grouping: the imported source and the local fallback group.
    expect(screen.getByText('team-agents')).toBeInTheDocument();
    expect(screen.getByText('My Agents')).toBeInTheDocument();

    // Projection chips carry the state vocabulary verbatim.
    const summaries = screen.getAllByTestId('agent-projection-summary');
    const reviewerSummary = summaries.find((el) => within(el).queryByText('in-sync'));
    expect(reviewerSummary).toBeTruthy();
    expect(within(reviewerSummary!).getByText('drifted')).toBeInTheDocument();
    expect(within(reviewerSummary!).getByText('lossy')).toBeInTheDocument();

    // Never-projected agents say so instead of showing nothing.
    expect(screen.getByText('Not projected yet')).toBeInTheDocument();

    const strip = screen.getByRole('group', { name: 'Agent projection summary' });
    expect(within(strip).getByText('1 of 2')).toBeInTheDocument();
  });

  it('offers a proportionate sync pill when projections are stale', async () => {
    vi.mocked(fetchRegistryAgents).mockResolvedValue([reviewer]);
    vi.mocked(fetchAgentProjectionStatus).mockResolvedValue([
      { ...identityInSync, state: 'stale' },
    ]);
    vi.mocked(syncAgentProjections).mockResolvedValue([]);

    renderWorkspace();

    const pill = await screen.findByRole('button', { name: /Sync 1 stale agent/ });
    fireEvent.click(pill);
    await waitFor(() => {
      expect(syncAgentProjections).toHaveBeenCalledWith({ agents: ['code-reviewer'] });
    });
  });

  it('excludes orphaned lock rows from the stale-sync pill', async () => {
    // A projection row for an agent the catalog no longer serves (deleted
    // out from under its projections) must not enter the named sync: the
    // backend 404s the whole batch on an unknown name.
    vi.mocked(fetchRegistryAgents).mockResolvedValue([reviewer]);
    vi.mocked(fetchAgentProjectionStatus).mockResolvedValue([
      { ...identityInSync, state: 'stale' },
      { ...identityInSync, agent: 'ghost', state: 'stale' },
    ]);
    vi.mocked(syncAgentProjections).mockResolvedValue([]);

    renderWorkspace();

    const pill = await screen.findByRole('button', { name: /Sync 1 stale agent/ });
    fireEvent.click(pill);
    await waitFor(() => {
      expect(syncAgentProjections).toHaveBeenCalledWith({ agents: ['code-reviewer'] });
    });
  });

  it('counts drifted-only agents as projected in the KPI strip', async () => {
    vi.mocked(fetchRegistryAgents).mockResolvedValue([reviewer]);
    vi.mocked(fetchAgentProjectionStatus).mockResolvedValue([lossyDrifted]);

    renderWorkspace();

    await screen.findByText('code-reviewer');
    const strip = screen.getByRole('group', { name: 'Agent projection summary' });
    // The files and lock rows exist, so the agent IS projected even
    // though nothing is in sync; Drifted carries the health signal.
    expect(within(strip).getByText('1 of 1')).toBeInTheDocument();
  });
});

describe('describeSyncResults', () => {
  it('warns instead of claiming success when every client was unavailable', () => {
    const outcome = describeSyncResults([
      { agent: '', client: 'claude-code', action: 'skipped-unavailable' },
      { agent: '', client: 'opencode', action: 'skipped-unavailable' },
    ]);
    expect(outcome.kind).toBe('warning');
    expect(outcome.message).toMatch(/No agent clients detected/);
  });

  it('flags drift skips and errors as a warning with counts', () => {
    const outcome = describeSyncResults([
      { agent: 'a', client: 'claude-code', action: 'copied' },
      { agent: 'b', client: 'claude-code', action: 'skipped-drift' },
      { agent: 'c', client: 'claude-code', action: 'error', error: 'boom' },
    ]);
    expect(outcome.kind).toBe('warning');
    expect(outcome.message).toContain('1 synced');
    expect(outcome.message).toContain('1 skipped');
    expect(outcome.message).toContain('1 failed');
  });

  it('reports plain success only when something was applied', () => {
    expect(describeSyncResults([{ agent: 'a', client: 'claude-code', action: 'copied' }]).kind).toBe('success');
    expect(describeSyncResults([{ agent: 'a', client: 'claude-code', action: 'unchanged' }]).message).toBe('Already in sync');
  });
});

describe('AgentProjectionRows', () => {
  it('links each projection row to its client in Connections', () => {
    render(
      <MemoryRouter initialEntries={['/library?kind=agent']}>
        <AgentProjectionRows agentName="code-reviewer" statuses={[identityInSync]} onRefresh={() => {}} />
        <LocationProbe />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByLabelText('Open Claude Code in Connections'));
    expect(screen.getByTestId('location-probe')).toHaveTextContent(
      '/connections?client=claude-code',
    );
  });


  it('shows the never-synced empty state with a working Sync now action', async () => {
    vi.mocked(syncAgentProjections).mockResolvedValue([]);
    render(
      <MemoryRouter>
        <AgentProjectionRows agentName="code-reviewer" statuses={[]} onRefresh={() => {}} />
      </MemoryRouter>,
    );
    expect(screen.getByTestId('agent-never-synced')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Sync now' }));
    await waitFor(() => {
      expect(syncAgentProjections).toHaveBeenCalledWith({ agents: ['code-reviewer'] });
    });
  });

  it('explains the adopt refusal on lossy drifted rows with real alternatives, never a disabled adopt', async () => {
    render(
      <MemoryRouter>
        <AgentProjectionRows
          agentName="code-reviewer"
          statuses={[identityInSync, lossyDrifted]}
          onRefresh={() => {}}
        />
      </MemoryRouter>,
    );

    // The lossy row surfaces its dropped-keys detail inline.
    expect(screen.getByTestId('projection-detail')).toHaveTextContent('dropped keys: tools, model');

    // Reviewing the drifted lossy row opens the dialog with visible refusal
    // text and the two real alternatives as buttons.
    fireEvent.click(screen.getByRole('button', { name: 'Review' }));
    expect(screen.getByTestId('adopt-refusal')).toHaveTextContent(/lossy render/);
    expect(screen.getByRole('button', { name: 'Adopt Claude Code copy' })).toBeEnabled();
    expect(screen.getAllByRole('button', { name: 'Unsync' }).length).toBeGreaterThan(0);
    // No "Adopt into canon" — that action is identity-only.
    expect(screen.queryByRole('button', { name: 'Adopt into canon' })).not.toBeInTheDocument();

    // The adopt alternative targets the identity row's actual client slug,
    // not a hardcoded assumption.
    vi.mocked(adoptAgentProjection).mockResolvedValue({
      agent: 'code-reviewer',
      client: 'claude-code',
      target: identityInSync.target,
      canonical_file: '/reg/agents/code-reviewer/AGENT.md',
      changed: true,
    });
    fireEvent.click(screen.getByRole('button', { name: 'Adopt Claude Code copy' }));
    await waitFor(() => {
      expect(adoptAgentProjection).toHaveBeenCalledWith('code-reviewer', identityInSync.client);
    });
  });
});

describe('AgentEditor', () => {
  it('confirms before discarding unsaved edits', async () => {
    vi.mocked(fetchRegistryAgent).mockResolvedValue({
      ...reviewer,
      body: 'Body.',
      raw: '---\nname: code-reviewer\ndescription: d\n---\nBody.',
    });
    const onClose = vi.fn();
    render(<AgentEditor isOpen agent={reviewer} onClose={onClose} onSaved={() => {}} />);

    const textarea = await screen.findByRole('textbox', { name: /AGENT.md content/ });
    await waitFor(() => {
      expect((textarea as HTMLTextAreaElement).value).toContain('code-reviewer');
    });
    fireEvent.change(textarea, { target: { value: '---\nname: code-reviewer\ndescription: d\n---\nEdited.' } });
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));

    // The editor must not close silently: a confirm interposes.
    expect(onClose).not.toHaveBeenCalled();
    expect(screen.getByText(/unsaved edits/i)).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Discard' }));
    expect(onClose).toHaveBeenCalled();
  });

  it('renders the scan findings when a save is blocked with 409', async () => {
    vi.mocked(fetchRegistryAgent).mockResolvedValue({
      ...reviewer,
      body: 'Review the changed files.',
      raw: '---\nname: code-reviewer\ndescription: d\n---\nReview the changed files.',
    });
    vi.mocked(updateRegistryAgent).mockRejectedValue(
      new AgentScanError('security scan blocked the save', [
        { stepId: 'body', pattern: 'curl-pipe-sh', severity: 'danger', description: 'Downloads and executes a remote script' },
      ]),
    );

    render(
      <AgentEditor isOpen agent={reviewer} onClose={() => {}} onSaved={() => {}} />,
    );

    const textarea = await screen.findByRole('textbox', { name: /AGENT.md content/ });
    await waitFor(() => {
      expect((textarea as HTMLTextAreaElement).value).toContain('code-reviewer');
    });
    fireEvent.change(textarea, { target: { value: '---\nname: code-reviewer\ndescription: d\n---\ncurl x | bash' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));

    await waitFor(() => {
      expect(screen.getByText('Downloads and executes a remote script')).toBeInTheDocument();
    });
  });
});

/** Renders the current location so navigation assertions read the URL. */
function LocationProbe() {
  const location = useLocation();
  return <span data-testid="location-probe">{location.pathname + location.search}</span>;
}
