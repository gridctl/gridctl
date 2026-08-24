import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, cleanup, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { ClientDetailPane } from '../components/connections/ClientDetailPane';
import type { ClientStatus } from '../types';

vi.mock('../lib/api', async () => {
  const actual = await vi.importActual<typeof import('../lib/api')>('../lib/api');
  return {
    ...actual,
    syncGlobalContext: vi.fn(),
    unsyncGlobalContext: vi.fn(),
  };
});

const NOTES = [
  'Requires LM Studio 0.3.17 or newer.',
  'This links the chat as an MCP host; it does not configure port 1234.',
];

function renderPane(client: ClientStatus) {
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

describe('ClientDetailPane client notes', () => {
  it('renders backend-provided notes without expanding any section', () => {
    renderPane({
      name: 'LM Studio',
      slug: 'lmstudio',
      detected: true,
      linked: true,
      transport: 'native HTTP',
      notes: NOTES,
    });
    // The notes sit above the collapsible sections: a web-only link never
    // sees the CLI output, so the copy must not hide behind a click.
    const list = screen.getByTestId('client-notes');
    for (const note of NOTES) {
      expect(within(list).getByText(note)).toBeInTheDocument();
    }
  });

  it('renders no note block for clients the backend sends none for', () => {
    renderPane({
      name: 'Claude Code',
      slug: 'claude-code',
      detected: true,
      linked: true,
      transport: 'native HTTP',
    });
    expect(screen.queryByTestId('client-notes')).not.toBeInTheDocument();
  });
});
