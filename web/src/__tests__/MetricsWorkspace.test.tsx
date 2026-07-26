import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import '@testing-library/jest-dom';
import { MetricsWorkspace } from '../components/workspaces/MetricsWorkspace';
import { useStackStore } from '../stores/useStackStore';
import { useUIStore, COMPACT_MODE_DEFAULTS } from '../stores/useUIStore';
import type { CostMetricsResponse, CostUsage, MCPServerStatus, TokenMetricsResponse, TokenUsage } from '../types';

vi.mock('../components/ui/Toast', () => ({ showToast: vi.fn() }));
vi.mock('../hooks/useWindowManager', () => ({
  useWindowManager: () => ({
    openDetachedWindow: vi.fn(),
    closeDetachedWindow: vi.fn(),
    broadcastStateUpdate: vi.fn(),
    broadcastSelectionChange: vi.fn(),
  }),
}));
vi.mock('../lib/api', async (importActual) => {
  const actual = await importActual<typeof import('../lib/api')>();
  return {
    ...actual,
    fetchTokenMetrics: vi.fn(),
    fetchCostMetrics: vi.fn(),
    clearTokenMetrics: vi.fn().mockResolvedValue(undefined),
    fetchToolUsage: vi.fn().mockResolvedValue({
      servers: {
        github: {
          create_issue: { calls: 4, lastCalledAt: '2026-07-01T00:00:00Z', inputTokens: 120, outputTokens: 80, costUsd: 0.02 },
          list_repos: { calls: 1, inputTokens: 30, outputTokens: 10 },
        },
      },
    }),
  };
});

// Imported after the mock factory so the mock-then-import order is visible.
import { fetchTokenMetrics, fetchCostMetrics } from '../lib/api';

function server(name: string): MCPServerStatus {
  return { name, transport: 'stdio', initialized: true, tools: [], healthy: true } as unknown as MCPServerStatus;
}

const tokenUsage: TokenUsage = {
  session: { input_tokens: 100, output_tokens: 40, total_tokens: 140 },
  per_server: {
    github: { input_tokens: 60, output_tokens: 20, total_tokens: 80 },
    atlassian: { input_tokens: 40, output_tokens: 20, total_tokens: 60 },
  },
  per_client: { claude: { input_tokens: 100, output_tokens: 40, total_tokens: 140 } },
  format_savings: { original_tokens: 0, formatted_tokens: 0, saved_tokens: 0, savings_percent: 0 },
};

const costUsage: CostUsage = {
  session: { input_usd: 0.2, output_usd: 0.1, total_usd: 0.3 },
  per_server: { github: { input_usd: 0.15, output_usd: 0.05, total_usd: 0.2 } },
  per_client: { claude: { input_usd: 0.2, output_usd: 0.1, total_usd: 0.3 } },
};

// Series fixtures. The mocked fetchers echo the requested range (the hook
// discards responses whose range does not match the active one), and the
// default window is empty — the common Live case for an idle stack. Tests
// that exercise the window math swap in the seeded buckets.
const emptyTokenSeries: TokenMetricsResponse = {
  range: '30m',
  interval: '1m',
  data_points: [],
  per_server: {},
};
const emptyCostSeries: CostMetricsResponse = {
  range: '30m',
  interval: '1m',
  data_points: [],
  per_server: {},
  per_client: {},
};
const seededTokenSeries: TokenMetricsResponse = {
  range: '30m',
  interval: '1m',
  data_points: [
    { timestamp: '2026-01-01T00:00:00Z', input_tokens: 7, output_tokens: 3, total_tokens: 10 },
    { timestamp: '2026-01-01T00:01:00Z', input_tokens: 5, output_tokens: 5, total_tokens: 10 },
  ],
  per_server: {},
};
const seededCostSeries: CostMetricsResponse = {
  range: '30m',
  interval: '1m',
  data_points: [
    { timestamp: '2026-01-01T00:00:00Z', usd: 0.02 },
    { timestamp: '2026-01-01T00:01:00Z', usd: 0.03 },
  ],
  per_server: {},
  per_client: {},
};

function mockSeries(tokens: TokenMetricsResponse, cost: CostMetricsResponse) {
  vi.mocked(fetchTokenMetrics).mockImplementation((range = '1h') =>
    Promise.resolve({ ...tokens, range }),
  );
  vi.mocked(fetchCostMetrics).mockImplementation((range = '1h') =>
    Promise.resolve({ ...cost, range }),
  );
}

function seed(over: Partial<ReturnType<typeof useStackStore.getState>> = {}) {
  useStackStore.setState({
    isLoading: false,
    mcpServers: [server('github'), server('atlassian')],
    tokenUsage,
    costUsage,
    costAttribution: true,
    clientModels: {},
    effectiveClientModels: {},
    effectiveServerModels: {},
    defaultModel: '',
    ...over,
  });
}

beforeEach(() => {
  vi.clearAllMocks();
  useUIStore.setState({ compactMode: { ...COMPACT_MODE_DEFAULTS } });
  seed();
  mockSeries(emptyTokenSeries, emptyCostSeries);
});

function renderAt(path = '/metrics') {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <MetricsWorkspace />
    </MemoryRouter>,
  );
}

describe('MetricsWorkspace', () => {
  it('renders the scope navigator, the window label, and the session line', async () => {
    renderAt();
    // Anchor names so "Models" doesn't also match the "Edit pricing models" control.
    expect(screen.getByRole('button', { name: /^overview/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^clients/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^servers/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^tools/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^models/i })).toBeInTheDocument();
    // KPI cards are window-scoped (labeled by the active range); the session
    // snapshot renders once, on its own explicitly labeled line.
    expect(await screen.findByText('Total Tokens')).toBeInTheDocument();
    expect(screen.getByText('Last 30m')).toBeInTheDocument();
    expect(screen.getByText(/Session total: 140 tokens/)).toBeInTheDocument();
  });

  it('binds the KPI cards to the ranged series, not the session snapshot', async () => {
    mockSeries(seededTokenSeries, seededCostSeries);
    renderAt();
    // Window sums: 12 in / 8 out / 20 total / $0.050 — not the 140-token
    // session snapshot, which stays on the session line.
    expect(await screen.findByText('20')).toBeInTheDocument();
    expect(screen.getByText('12')).toBeInTheDocument();
    expect(screen.getByText('8')).toBeInTheDocument();
    expect(screen.getByText('$0.050')).toBeInTheDocument();
    expect(screen.getByText(/Session total: 140 tokens/)).toBeInTheDocument();
  });

  it('notes an idle window instead of presenting lifetime numbers unlabeled', async () => {
    renderAt();
    expect(await screen.findByText(/No activity in this window/)).toBeInTheDocument();
  });

  it('switches the window when a range is selected and mirrors it to ?range=', async () => {
    renderAt();
    fireEvent.click(screen.getByRole('button', { name: '24h' }));
    // The control reads back from the URL param, so a pressed state proves
    // the round-trip; the fetch carries the new range to the API.
    expect(await screen.findByText('Last 24h')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '24h' })).toHaveAttribute('aria-pressed', 'true');
    expect(vi.mocked(fetchTokenMetrics)).toHaveBeenLastCalledWith('24h');
  });

  it('restores the range from a ?range= deep link', () => {
    renderAt('/metrics?range=24h');
    expect(screen.getByRole('button', { name: '24h' })).toHaveAttribute('aria-pressed', 'true');
    expect(vi.mocked(fetchTokenMetrics)).toHaveBeenCalledWith('24h');
  });

  it('names the full blast radius in the clear confirm', async () => {
    renderAt();
    fireEvent.click(screen.getByTitle('Clear Metrics'));
    expect(await screen.findByText('Clear all metrics?')).toBeInTheDocument();
    expect(screen.getByText(/token, cost, tool usage, and model history/)).toBeInTheDocument();
  });

  it('defaults to the overview scope with the model-mix panel', async () => {
    renderAt();
    expect(await screen.findByText('Cost by Model')).toBeInTheDocument();
  });

  it('switches to the servers breakdown and selects a row into the inspector', async () => {
    renderAt();
    fireEvent.click(screen.getByRole('button', { name: /^servers/i }));

    // The breakdown table now lists each server, labeled as session totals.
    expect(await screen.findByText('Per-Server · session totals')).toBeInTheDocument();
    expect(screen.getByText('github')).toBeInTheDocument();
    expect(screen.getByText('atlassian')).toBeInTheDocument();

    // Selecting a row opens the inspector (its "Pricing model" section is
    // unique to a selected entity).
    fireEvent.click(screen.getByText('github'));
    expect(await screen.findByText('Pricing model')).toBeInTheDocument();
  });

  it('explains an empty per-entity series instead of hiding the sparklines', async () => {
    // Also pins the ?scope=&selected= deep link the Traces pivot relies on.
    renderAt('/metrics?scope=servers&selected=github');
    expect(await screen.findByText('Pricing model')).toBeInTheDocument();
    expect(await screen.findByText(/No samples for github in this window/)).toBeInTheDocument();
  });

  it('hides the inspector setup hint when cost is already priced', () => {
    renderAt();
    expect(screen.queryByText(/Set a pricing model in the pricing manager/)).not.toBeInTheDocument();
  });

  it('shows the pricing-manager setup hint when nothing is priced', async () => {
    seed({ costAttribution: false, costUsage: null });
    renderAt();
    // Once the data view lands, the hint renders exactly twice: under the
    // Cost KPI card and in the inspector overview legend.
    expect(await screen.findByText(/Session total: 140 tokens/)).toBeInTheDocument();
    expect(screen.getAllByText(/Set a pricing model in the pricing manager/)).toHaveLength(2);
  });

  it('shows the model-mix panel under the models scope', async () => {
    renderAt('/metrics?scope=models');
    expect(await screen.findByText('Cost by Model')).toBeInTheDocument();
  });

  it('shows the per-tool breakdown with server-qualified names under the tools scope', async () => {
    renderAt('/metrics?scope=tools');
    expect(await screen.findByText('Per-Tool · session totals')).toBeInTheDocument();
    // Rows render server › tool so name collisions across servers stay distinct.
    expect(await screen.findByText('create_issue')).toBeInTheDocument();
    expect(screen.getByText('list_repos')).toBeInTheDocument();
    // Priced tool shows a cost; unpriced tool shows the em dash, never $0.
    // (Scoped to the table: the window Cost KPI legitimately reads $0.00 for
    // a priced session with an idle window.)
    const table = screen.getByRole('table');
    expect(within(table).getByText('$0.020')).toBeInTheDocument();
    expect(within(table).getByText('—')).toBeInTheDocument();
    expect(within(table).queryByText('$0.00')).not.toBeInTheDocument();
  });

  it('selects a tool row into the inspector without a pricing-model editor', async () => {
    renderAt('/metrics?scope=tools');
    fireEvent.click(await screen.findByText('create_issue'));
    // The inspector shows the tool's KPI grid (Calls is tools-only)…
    expect(await screen.findByText('Calls')).toBeInTheDocument();
    // …but no pricing editor: a tool's cost inherits client/server attribution.
    expect(screen.queryByText('Pricing model')).not.toBeInTheDocument();
  });

  it('shows the onboarding empty state when there is no traffic', async () => {
    seed({ tokenUsage: null, costUsage: null, costAttribution: false });
    renderAt();
    // The first-load skeleton clears once the (empty) series resolves.
    expect(await screen.findByText('Your metrics home')).toBeInTheDocument();
  });
});
