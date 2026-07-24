import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import '@testing-library/jest-dom';
import { MetricsWorkspace } from '../components/workspaces/MetricsWorkspace';
import { useStackStore } from '../stores/useStackStore';
import { useUIStore, COMPACT_MODE_DEFAULTS } from '../stores/useUIStore';
import type { CostUsage, MCPServerStatus, TokenUsage } from '../types';

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
    fetchTokenMetrics: vi.fn().mockResolvedValue({ range: '30m', interval: '1m', data_points: [], per_server: {} }),
    fetchCostMetrics: vi.fn().mockResolvedValue({ range: '30m', interval: '1m', data_points: [], per_server: {}, per_client: {} }),
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
  useUIStore.setState({ compactMode: { ...COMPACT_MODE_DEFAULTS } });
  seed();
});

function renderAt(path = '/metrics') {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <MetricsWorkspace />
    </MemoryRouter>,
  );
}

describe('MetricsWorkspace', () => {
  it('renders the scope navigator and the session KPI row', () => {
    renderAt();
    // Anchor names so "Models" doesn't also match the "Edit pricing models" control.
    expect(screen.getByRole('button', { name: /^overview/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^clients/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^servers/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^tools/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^models/i })).toBeInTheDocument();
    // KPI row carries the session total.
    expect(screen.getByText('Total Tokens')).toBeInTheDocument();
    expect(screen.getByText('140')).toBeInTheDocument();
  });

  it('defaults to the overview scope with the model-mix panel', () => {
    renderAt();
    expect(screen.getByText('Cost by Model')).toBeInTheDocument();
  });

  it('switches to the servers breakdown and selects a row into the inspector', async () => {
    renderAt();
    fireEvent.click(screen.getByRole('button', { name: /^servers/i }));

    // The breakdown table now lists each server.
    expect(await screen.findByText('github')).toBeInTheDocument();
    expect(screen.getByText('atlassian')).toBeInTheDocument();

    // Selecting a row opens the inspector (its "Pricing model" section is
    // unique to a selected entity).
    fireEvent.click(screen.getByText('github'));
    expect(await screen.findByText('Pricing model')).toBeInTheDocument();
  });

  it('shows the model-mix panel under the models scope', () => {
    renderAt('/metrics?scope=models');
    expect(screen.getByText('Cost by Model')).toBeInTheDocument();
  });

  it('shows the per-tool breakdown with server-qualified names under the tools scope', async () => {
    renderAt('/metrics?scope=tools');
    expect(await screen.findByText('Per-Tool')).toBeInTheDocument();
    // Rows render server › tool so name collisions across servers stay distinct.
    expect(await screen.findByText('create_issue')).toBeInTheDocument();
    expect(screen.getByText('list_repos')).toBeInTheDocument();
    // Priced tool shows a cost; unpriced tool shows the em dash, never $0.
    expect(screen.getByText('$0.020')).toBeInTheDocument();
    expect(screen.getByText('—')).toBeInTheDocument();
    expect(screen.queryByText('$0.00')).not.toBeInTheDocument();
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
