import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, cleanup, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { ClientDetailPane } from '../components/connections/ClientDetailPane';
import type { ClientStatus, ModelsTargetStatus } from '../types';

vi.mock('../lib/api', async () => {
  const actual = await vi.importActual<typeof import('../lib/api')>('../lib/api');
  return {
    ...actual,
    syncGlobalContext: vi.fn(),
    unsyncGlobalContext: vi.fn(),
  };
});

const opencode: ClientStatus = {
  name: 'OpenCode',
  slug: 'opencode',
  detected: true,
  linked: true,
  transport: 'native HTTP',
} as ClientStatus;

const claude: ClientStatus = {
  name: 'Claude Desktop',
  slug: 'claude',
  detected: true,
  linked: true,
  transport: 'stdio',
} as ClientStatus;

function renderPane(
  client: ClientStatus,
  modelsTargets: ModelsTargetStatus[] | null,
  opts: { modelsFailed?: boolean; onOpen?: () => void } = {},
) {
  return render(
    <MemoryRouter>
      <ClientDetailPane
        client={client}
        health={{ attention: false, reasons: [] }}
        wiringRows={[]}
        contextClient={null}
        agentRows={[]}
        sessions={[]}
        onRefresh={() => {}}
        modelsTargets={modelsTargets}
        modelsFailed={opts.modelsFailed ?? false}
        onReviewContext={() => {}}
        onOpenModelRouting={opts.onOpen ?? (() => {})}
      />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe('ClientDetailPane Model routing section', () => {
  it('always renders, with an explanation for non-OpenCode clients', () => {
    renderPane(claude, []);
    fireEvent.click(screen.getByRole('button', { name: /Model routing/ }));
    expect(screen.getByTestId('not-models-target')).toBeInTheDocument();
  });

  it('shows the OpenCode row with a deep link, never row-level actions', () => {
    const onOpen = vi.fn();
    renderPane(
      opencode,
      [
        { target: 'opencode', client: 'opencode', state: 'drifted', path: '/home/u/.config/opencode/opencode.json' },
        { target: 'litellm-fragment', client: 'litellm', state: 'in-sync', restart_pending: true },
      ],
      { onOpen },
    );
    // Drifted state auto-expands the section: the state shows in both
    // the collapsed summary and the expanded row's pill.
    expect(screen.getAllByText('drifted')).toHaveLength(2);
    // No Sync, Adopt, or Mark restarted in the pane: the verbs are
    // whole-policy and live in the dialog.
    expect(screen.queryByRole('button', { name: 'Sync' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Mark restarted' })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Open' }));
    expect(onOpen).toHaveBeenCalledTimes(1);
  });

  it('distinguishes a failed status fetch from one still loading', () => {
    renderPane(opencode, null, { modelsFailed: true });
    fireEvent.click(screen.getByRole('button', { name: /Model routing/ }));
    expect(screen.getByText(/unavailable: the status fetch failed/)).toBeInTheDocument();

    cleanup();
    renderPane(opencode, null);
    fireEvent.click(screen.getByRole('button', { name: /Model routing/ }));
    expect(screen.getByText(/has not loaded/)).toBeInTheDocument();
  });

  it('says when the policy declares no OpenCode client', () => {
    renderPane(opencode, [{ target: 'litellm-fragment', client: 'litellm', state: 'never-synced' }]);
    fireEvent.click(screen.getByRole('button', { name: /Model routing/ }));
    expect(screen.getByText(/declares no OpenCode client/)).toBeInTheDocument();
  });
});
