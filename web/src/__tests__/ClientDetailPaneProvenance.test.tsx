import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, cleanup, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { ClientDetailPane } from '../components/connections/ClientDetailPane';
import type { ContextClientStatus } from '../lib/api';
import type { ClientStatus } from '../types';

vi.mock('../lib/api', async () => {
  const actual = await vi.importActual<typeof import('../lib/api')>('../lib/api');
  return {
    ...actual,
    syncGlobalContext: vi.fn(),
    unsyncGlobalContext: vi.fn(),
  };
});

const client: ClientStatus = {
  name: 'Claude Code',
  slug: 'claude-code',
  detected: true,
  linked: true,
  transport: 'native HTTP',
};

const contextClient: ContextClientStatus = {
  slug: 'claude-code',
  name: 'Claude Code',
  supported: true,
  available: true,
  mode: 'multi-file',
  target_path: '/home/u/.claude/rules',
  state: 'drifted',
  fragments: [
    { name: 'team-style', state: 'drifted', pack: 'team-pack' },
    { name: 'personal', state: 'stale' },
  ],
};

function renderPane() {
  return render(
    <MemoryRouter>
      <ClientDetailPane
        client={client}
        health={{ attention: true, reasons: ['context drifted'] }}
        wiringRows={[]}
        contextClient={contextClient}
        agentRows={[]}
        sessions={[]}
        onRefresh={() => {}}
        modelsTargets={null}
        onReviewContext={() => {}}
        onOpenModelRouting={() => {}}
      />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe('ClientDetailPane context provenance', () => {
  it('lists non-synced fragments with pack chips when the wire carries a tag', () => {
    renderPane();
    const list = screen.getByLabelText('Claude Code out-of-sync context fragments');
    // Both fragment rows render; only the pack-applied one gets a chip.
    expect(within(list).getByText('team-style')).toBeInTheDocument();
    expect(within(list).getByText('personal')).toBeInTheDocument();
    expect(within(list).getByText('pack: team-pack')).toBeInTheDocument();
    expect(within(list).getAllByText(/^pack:/)).toHaveLength(1);
  });

  it('renders no fragment list when the wire omits fragments (all in sync)', () => {
    render(
      <MemoryRouter>
        <ClientDetailPane
          client={client}
          health={{ attention: false, reasons: [] }}
          wiringRows={[]}
          contextClient={{ ...contextClient, state: 'in-sync', fragments: undefined }}
          agentRows={[]}
          sessions={[]}
          onRefresh={() => {}}
          modelsTargets={null}
          onReviewContext={() => {}}
          onOpenModelRouting={() => {}}
        />
      </MemoryRouter>,
    );
    expect(
      screen.queryByLabelText('Claude Code out-of-sync context fragments'),
    ).not.toBeInTheDocument();
  });
});
