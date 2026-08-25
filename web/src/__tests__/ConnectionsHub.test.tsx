import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, cleanup, fireEvent, waitFor, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import ConnectionsWorkspace from '../components/workspaces/ConnectionsWorkspace';
import {
  clientHealth,
  sortClients,
  unjoinedAgentSlugs,
} from '../components/connections/connectionsModel';
import { useStackStore } from '../stores/useStackStore';
import { useContextStore } from '../stores/useContextStore';
import { useRegistryStore } from '../stores/useRegistryStore';
import type { ClientStatus, WiringRow } from '../types';
import type { ContextDoc } from '../lib/api';

vi.mock('../components/ui/Toast', async () => {
  const actual = await vi.importActual<typeof import('../components/ui/Toast')>('../components/ui/Toast');
  return { ...actual, showToast: vi.fn() };
});

vi.mock('../lib/api', async () => {
  const actual = await vi.importActual<typeof import('../lib/api')>('../lib/api');
  return {
    ...actual,
    fetchClients: vi.fn(),
    fetchSessions: vi.fn(),
    fetchWiringStatus: vi.fn(),
    adoptWiringEntry: vi.fn(),
    fetchAgentProjectionStatus: vi.fn(),
    fetchModelsStatus: vi.fn(),
    fetchModelsValidation: vi.fn(),
    fetchGlobalContext: vi.fn(),
    linkClient: vi.fn(),
    unlinkClient: vi.fn(),
    previewClientLink: vi.fn(),
    syncGlobalContext: vi.fn(),
    unsyncGlobalContext: vi.fn(),
  };
});

import { showToast } from '../components/ui/Toast';
import {
  adoptWiringEntry,
  fetchClients,
  linkClient,
  fetchAgentProjectionStatus,
  fetchGlobalContext,
  fetchModelsStatus,
  fetchSessions,
  fetchWiringStatus,
} from '../lib/api';

function client(overrides: Partial<ClientStatus> & { slug: string; name: string }): ClientStatus {
  return {
    detected: true,
    linked: false,
    transport: 'http',
    ...overrides,
  } as ClientStatus;
}

const claude = client({ slug: 'claude', name: 'Claude Desktop', linked: true, configPath: '/home/u/.claude.json' });
const cursor = client({ slug: 'cursor', name: 'Cursor' });
const ghost = client({ slug: 'zed', name: 'Zed', detected: false });

const foreignRow: WiringRow = {
  client: 'claude',
  name: 'gridctl',
  channel: 'config-entry',
  target: '/home/u/.claude.json',
  state: 'foreign',
  detail: 'entry was not recorded by gridctl',
  remediation: 'adopt to take ownership',
};

const emptyContextDoc: ContextDoc = {
  canonical: { path: '/home/u/.gridctl/context/AGENTS.md', exists: false, content: '' },
  needs_sync: false,
  clients: [],
};

function renderHub(initialEntry = '/connections') {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <ConnectionsWorkspace />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  cleanup();
  vi.clearAllMocks();
  useStackStore.setState({ clients: [claude, cursor, ghost], sessionEntries: null });
  useContextStore.setState({ doc: null, loading: false, error: null });
  useRegistryStore.setState({ agentStatuses: null });
  vi.mocked(fetchClients).mockResolvedValue([claude, cursor, ghost]);
  vi.mocked(fetchWiringStatus).mockResolvedValue([foreignRow]);
  vi.mocked(fetchAgentProjectionStatus).mockResolvedValue([]);
  vi.mocked(fetchGlobalContext).mockResolvedValue(emptyContextDoc);
  vi.mocked(fetchSessions).mockResolvedValue({ count: 0, sessions: [], entries: [] });
  vi.mocked(fetchModelsStatus).mockResolvedValue({
    policy_path: '/home/u/.gridctl/models/policy.yaml',
    policy_exists: false,
    needs_attention: false,
    targets: [],
  });
});

describe('clientHealth', () => {
  it('joins wiring, context, and agent drift per slug', () => {
    const health = clientHealth(
      'claude',
      [foreignRow],
      [
        {
          slug: 'claude',
          name: 'Claude Desktop',
          supported: true,
          available: true,
          state: 'drifted',
        },
      ],
      [
        {
          agent: 'reviewer',
          client: 'claude',
          channel: 'copy',
          target: '/x',
          render: 'identity',
          state: 'stale',
        },
      ],
    );
    expect(health.attention).toBe(true);
    expect(health.reasons).toEqual([
      'wiring foreign',
      'context drifted',
      '1 agent projection stale',
    ]);
  });

  it('treats missing wiring as an opportunity, not attention', () => {
    const health = clientHealth(
      'cursor',
      [{ ...foreignRow, client: 'cursor', state: 'missing' }],
      null,
      null,
    );
    expect(health.attention).toBe(false);
  });

  it('joins OpenCode model-routing drift, and only drift', () => {
    const drifted = clientHealth('opencode', null, null, null, [
      { target: 'opencode', client: 'opencode', state: 'drifted', path: '/home/u/.config/opencode/opencode.json' },
    ]);
    expect(drifted.attention).toBe(true);
    expect(drifted.reasons).toEqual(['model routing drifted']);

    // Restart-pending is an annotation, never attention: the fragment
    // row is in-sync and its latch must not light the rail.
    const pending = clientHealth('opencode', null, null, null, [
      { target: 'opencode', client: 'opencode', state: 'in-sync' },
      { target: 'litellm-fragment', client: 'litellm', state: 'in-sync', restart_pending: true },
    ]);
    expect(pending.attention).toBe(false);
  });

  it('never joins LiteLLM model-routing targets to any rail slug', () => {
    // client "litellm" is not a provisioner slug; drift on the fragment
    // or include line lives in the Model routing dialog alone.
    const health = clientHealth('claude', null, null, null, [
      { target: 'litellm-fragment', client: 'litellm', state: 'drifted' },
      { target: 'litellm-include', client: 'litellm', state: 'target-missing' },
    ]);
    expect(health.attention).toBe(false);
  });

  it('surfaces agent slugs that join no client instead of dropping them', () => {
    const slugs = unjoinedAgentSlugs(
      [
        { agent: 'a', client: 'copilot', channel: 'copy', target: '/x', render: 'lossy', state: 'in-sync' },
        { agent: 'a', client: 'claude', channel: 'copy', target: '/y', render: 'identity', state: 'in-sync' },
      ],
      new Set(['claude']),
    );
    // copilot is the documented non-provisioner agent target; it must
    // surface here rather than vanish in the join.
    expect(slugs).toEqual(['copilot']);
  });
});

describe('sortClients', () => {
  it('orders attention, connected, detected, rest', () => {
    const order = sortClients([ghost, cursor, claude], (slug) =>
      slug === 'cursor' ? { attention: true, reasons: ['wiring drifted'] } : { attention: false, reasons: [] },
    ).map((c) => c.slug);
    expect(order).toEqual(['cursor', 'claude', 'zed']);
  });
});

describe('ConnectionsWorkspace hub', () => {
  it('header Model routing action opens the dialog', async () => {
    renderHub();
    fireEvent.click(await screen.findByRole('button', { name: 'Model routing' }));
    expect(await screen.findByRole('heading', { name: 'Model routing' })).toBeInTheDocument();
    expect(fetchModelsStatus).toHaveBeenCalled();
    // The beforeEach doc has no policy: the dialog opens on the init
    // empty state rather than an error.
    expect(await screen.findByText('No model routing policy yet')).toBeInTheDocument();
  });

  it('renders the rail and a detail pane with the two axes separately labeled', async () => {
    renderHub();
    // Default selection prefers the attention client (claude has a
    // foreign wiring row).
    await waitFor(() => {
      expect(screen.getByText('Ownership')).toBeInTheDocument();
    });
    expect(screen.getByText('Activity')).toBeInTheDocument();
    // The wiring section auto-expands on attention and speaks the state
    // vocabulary verbatim.
    expect(await screen.findByText('foreign')).toBeInTheDocument();
    expect(screen.getByText('entry was not recorded by gridctl')).toBeInTheDocument();
  });

  it('adopts a foreign wiring entry through the confirm dialog', async () => {
    vi.mocked(adoptWiringEntry).mockResolvedValue({
      client: 'claude',
      name: 'gridctl',
      action: 'adopted',
    });
    renderHub();
    const adopt = await screen.findByRole('button', { name: 'Adopt' });
    fireEvent.click(adopt);
    const dialog = await screen.findByRole('alertdialog');
    expect(within(dialog).getByText(/Record ownership/)).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole('button', { name: 'Adopt' }));
    await waitFor(() => {
      expect(adoptWiringEntry).toHaveBeenCalledWith('claude', 'gridctl');
    });
  });

  it('shows the explicit non-target copy for clients outside the agent projection table', async () => {
    renderHub('/connections?client=claude');
    // The section is collapsed when quiet; expand it. claude (the
    // desktop slug) is not an agentsync target; the section must say so
    // instead of rendering blank space.
    fireEvent.click(await screen.findByRole('button', { name: /^Agents/ }));
    expect(await screen.findByTestId('not-agent-target')).toHaveTextContent(
      /Not an agent projection target/,
    );
  });

  it('never renders "0 sessions": the sessionless copy explains the absence', async () => {
    renderHub('/connections?client=claude');
    fireEvent.click(await screen.findByRole('button', { name: /^Sessions/ }));
    expect(await screen.findByTestId('sessionless-copy')).toHaveTextContent(/sessionless by design/);
  });

  it('buckets unattributable sessions honestly instead of guessing', async () => {
    vi.mocked(fetchSessions).mockResolvedValue({
      count: 2,
      sessions: ['s-1', 's-2'],
      entries: [
        { id: 's-1', generation: 'handshake', protocolVersion: '2025-11-25', accessId: 'claude' },
        { id: 's-2', generation: 'handshake', clientName: 'mystery-client' },
      ],
    });
    renderHub('/connections?client=claude');
    expect(
      await screen.findByText('1 session not matched to a linked client'),
    ).toBeInTheDocument();
    // The attributed one lands on claude's detail pane.
    expect(await screen.findByText(/1 active session/)).toBeInTheDocument();
  });

  it('spotlights the first detected-unlinked client from the wizard route', async () => {
    renderHub('/connections?spotlight=unlinked');
    // cursor is detected and unlinked; the detail pane should land on it.
    await waitFor(() => {
      const pane = screen.getAllByText('Cursor');
      expect(pane.length).toBeGreaterThan(0);
    });
    await waitFor(() => {
      expect(screen.getByRole('heading', { name: 'Cursor' })).toBeInTheDocument();
    });
  });

  it('does not claim ownership health while wiring status is still loading', async () => {
    // A never-resolving wiring fetch: the Ownership axis must say
    // loading, not "in sync".
    vi.mocked(fetchWiringStatus).mockReturnValue(new Promise(() => {}));
    renderHub('/connections?client=claude');
    await screen.findByText('Ownership');
    const strip = screen.getByText('Ownership').parentElement!;
    expect(within(strip).getByText('loading')).toBeInTheDocument();
    expect(screen.queryByText(/is in sync/)).not.toBeInTheDocument();
  });

  it('offers Overwrite (not plain Re-link) on drifted and foreign wiring rows', async () => {
    renderHub('/connections?client=claude');
    // The foreign row gets Adopt + Overwrite; a plain Re-link would
    // systematically 409 without force.
    expect(await screen.findByRole('button', { name: 'Adopt' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Overwrite' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Re-link' })).not.toBeInTheDocument();
    // The engine's remediation renders inline rather than surfacing only
    // on failure.
    expect(screen.getByText('adopt to take ownership')).toBeInTheDocument();
  });

  it('force-links through the Overwrite confirm dialog', async () => {
    vi.mocked(linkClient).mockResolvedValue({
      client: 'claude',
      serverName: 'gridctl',
      linked: true,
      declared: true,
    });
    renderHub('/connections?client=claude');
    fireEvent.click(await screen.findByRole('button', { name: 'Overwrite' }));
    const dialog = await screen.findByRole('alertdialog');
    fireEvent.click(within(dialog).getByRole('button', { name: 'Overwrite' }));
    await waitFor(() => {
      expect(linkClient).toHaveBeenCalledWith('claude', { force: true });
    });
  });

  it('reports a failed sessions fetch as unavailable, never permanent loading', async () => {
    vi.mocked(fetchSessions).mockRejectedValue(new Error('boom'));
    renderHub('/connections?client=claude');
    fireEvent.click(await screen.findByRole('button', { name: /^Sessions/ }));
    expect(await screen.findByText(/Session state is unavailable/)).toBeInTheDocument();
  });

  it('toasts and clears an unknown ?client deep link once loaded', async () => {
    renderHub('/connections?client=nope');
    await waitFor(() => {
      expect(showToast).toHaveBeenCalledWith('error', 'Client "nope" not found');
    });
    // Selection falls back to the default (the attention client).
    expect(await screen.findByRole('heading', { name: 'Claude Desktop' })).toBeInTheDocument();
  });
});
