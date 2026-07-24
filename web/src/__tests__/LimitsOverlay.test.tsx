import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, cleanup, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { LimitsBadge } from '../components/metrics/LimitsBadge';
import { BudgetBar, LimitsPanel } from '../components/metrics/LimitsShared';
import {
  budgetForRow,
  deriveLimitsSummary,
  limitStateFillClass,
} from '../components/metrics/limitsData';
import { fetchLimits, type LimitEntry, type LimitsReport } from '../lib/api';

vi.mock('../lib/api', async () => {
  const actual = await vi.importActual<typeof import('../lib/api')>('../lib/api');
  return { ...actual, fetchLimits: vi.fn() };
});

const mockFetchLimits = vi.mocked(fetchLimits);

function budgetEntry(overrides: Partial<LimitEntry> & { spent?: number; max?: number; percent?: number } = {}): LimitEntry {
  const { spent = 1.25, max = 5, percent = 25, ...rest } = overrides;
  return {
    kind: 'budget',
    scope: 'client',
    key: 'claude-code',
    state: 'ok',
    budget: {
      max_usd: max,
      spent_usd: spent,
      percent,
      period: 'daily',
      window_start: '2026-07-20T00:00:00-04:00',
      window_end: '2026-07-21T00:00:00-04:00',
    },
    ...rest,
  };
}

const rateEntry: LimitEntry = {
  kind: 'rate',
  scope: 'server',
  key: 'github',
  state: 'ok',
  rate: { calls_per_minute: 30, burst: 10 },
};

function report(entries: LimitEntry[], configured = true): LimitsReport {
  return { configured, entries };
}

beforeEach(() => {
  cleanup();
  mockFetchLimits.mockReset();
});

describe('LimitsBadge', () => {
  it('renders nothing when limits are unconfigured', async () => {
    mockFetchLimits.mockResolvedValue(report([], false));
    const { container } = render(<LimitsBadge />, { wrapper: MemoryRouter });
    await waitFor(() => expect(mockFetchLimits).toHaveBeenCalled());
    expect(container).toBeEmptyDOMElement();
  });

  it('renders nothing while every limit is ok', async () => {
    mockFetchLimits.mockResolvedValue(report([budgetEntry(), rateEntry]));
    const { container } = render(<LimitsBadge />, { wrapper: MemoryRouter });
    await waitFor(() => expect(mockFetchLimits).toHaveBeenCalled());
    expect(container).toBeEmptyDOMElement();
  });

  it('shows an amber near-cap chip at warn', async () => {
    mockFetchLimits.mockResolvedValue(report([budgetEntry({ state: 'warn', percent: 85, spent: 4.25 })]));
    render(<LimitsBadge />, { wrapper: MemoryRouter });
    const chip = await screen.findByRole('button', { name: /1 limit near cap/i });
    expect(chip.className).toContain('text-status-pending');
  });

  it('shows a red exceeded chip that wins over warn', async () => {
    mockFetchLimits.mockResolvedValue(
      report([
        budgetEntry({ state: 'warn', key: 'cursor' }),
        budgetEntry({ state: 'exceeded', percent: 104, spent: 5.2 }),
      ]),
    );
    render(<LimitsBadge />, { wrapper: MemoryRouter });
    const chip = await screen.findByRole('button', { name: /1 budget exceeded/i });
    expect(chip.className).toContain('text-status-error');
  });
});

describe('BudgetBar', () => {
  it('renders a real $0.00 for zero spend, never the em-dash', () => {
    render(<BudgetBar entry={budgetEntry({ spent: 0, percent: 0 })} />);
    expect(screen.getByText(/\$0\.00\/\$5\.00/)).toBeInTheDocument();
    expect(screen.queryByText('—')).not.toBeInTheDocument();
  });

  it('colors by state', () => {
    expect(limitStateFillClass('ok')).toBe('bg-primary/70');
    expect(limitStateFillClass('warn')).toBe('bg-status-pending');
    expect(limitStateFillClass('exceeded')).toBe('bg-status-error');
  });

  it('clamps the fill width at 100% when over cap', () => {
    const { container } = render(
      <BudgetBar entry={budgetEntry({ state: 'exceeded', percent: 140, spent: 7 })} />,
    );
    const fill = container.querySelector('.bg-status-error') as HTMLElement;
    expect(fill.style.width).toBe('100%');
  });
});

describe('LimitsPanel', () => {
  it('renders nothing when unconfigured', () => {
    const { container } = render(<LimitsPanel summary={deriveLimitsSummary(report([], false))} />);
    expect(container).toBeEmptyDOMElement();
  });

  it('lists budget and rate entries with elevated states first', () => {
    const summary = deriveLimitsSummary(
      report([budgetEntry(), rateEntry, budgetEntry({ state: 'exceeded', scope: 'tool', key: 'github__search_code' })]),
    );
    render(<LimitsPanel summary={summary} />);
    expect(screen.getByText('Limits')).toBeInTheDocument();
    expect(screen.getByText(/30 calls\/min/)).toBeInTheDocument();
    expect(screen.getByText('1 exceeded')).toBeInTheDocument();
    const items = screen.getAllByRole('listitem');
    expect(items[0].textContent).toContain('github__search_code');
  });
});

describe('limitsData helpers', () => {
  it('matches client rows through key normalization', () => {
    const entries = [budgetEntry({ key: 'Claude Code' })];
    expect(budgetForRow(entries, 'client', 'claude-code')).toBe(entries[0]);
    expect(budgetForRow(entries, 'client', 'cursor')).toBeUndefined();
  });

  it('matches server and tool scopes verbatim only', () => {
    const entries = [
      budgetEntry({ scope: 'server', key: 'github' }),
      budgetEntry({ scope: 'tool', key: 'github__search_code' }),
    ];
    expect(budgetForRow(entries, 'server', 'github')).toBe(entries[0]);
    expect(budgetForRow(entries, 'tool', 'github__search_code')).toBe(entries[1]);
    // A server budget never decorates the tool table and vice versa.
    expect(budgetForRow(entries, 'tool', 'github')).toBeUndefined();
    expect(budgetForRow(entries, 'server', 'github__search_code')).toBeUndefined();
  });

  it('derives worst state and counts', () => {
    const summary = deriveLimitsSummary(
      report([budgetEntry({ state: 'warn' }), budgetEntry({ state: 'exceeded', key: 'cursor' }), rateEntry]),
    );
    expect(summary.worst).toBe('exceeded');
    expect(summary.exceededCount).toBe(1);
    expect(summary.warnCount).toBe(1);
  });
});
