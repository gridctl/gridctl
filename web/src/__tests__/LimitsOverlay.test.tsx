import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, cleanup, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { LimitsBadge } from '../components/metrics/LimitsBadge';
import { LimitsPanel } from '../components/metrics/LimitsShared';
import {
  deriveLimitsSummary,
  limitStateFillClass,
  rateForRow,
} from '../components/metrics/limitsData';
import { fetchLimits, type LimitEntry, type LimitsReport } from '../lib/api';

vi.mock('../lib/api', async () => {
  const actual = await vi.importActual<typeof import('../lib/api')>('../lib/api');
  return { ...actual, fetchLimits: vi.fn() };
});

const mockFetchLimits = vi.mocked(fetchLimits);

function rateEntry(overrides: Partial<LimitEntry> = {}): LimitEntry {
  return {
    kind: 'rate',
    scope: 'server',
    key: 'github',
    state: 'ok',
    rate: { calls_per_minute: 30, burst: 10 },
    ...overrides,
  };
}

// A budget entry as an older backend still reports it. The UI must filter it
// out everywhere rather than misrender it as a rate.
const legacyBudgetEntry = {
  kind: 'budget',
  scope: 'client',
  key: 'claude-code',
  state: 'exceeded',
} as LimitEntry;

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
    mockFetchLimits.mockResolvedValue(report([rateEntry()]));
    const { container } = render(<LimitsBadge />, { wrapper: MemoryRouter });
    await waitFor(() => expect(mockFetchLimits).toHaveBeenCalled());
    expect(container).toBeEmptyDOMElement();
  });

  it('shows an amber near-cap chip at warn', async () => {
    mockFetchLimits.mockResolvedValue(report([rateEntry({ state: 'warn' })]));
    render(<LimitsBadge />, { wrapper: MemoryRouter });
    const chip = await screen.findByRole('button', { name: /1 limit near cap/i });
    expect(chip.className).toContain('text-status-pending');
  });

  it('shows a red exceeded chip that wins over warn', async () => {
    mockFetchLimits.mockResolvedValue(
      report([rateEntry({ state: 'warn', key: 'atlassian' }), rateEntry({ state: 'exceeded' })]),
    );
    render(<LimitsBadge />, { wrapper: MemoryRouter });
    const chip = await screen.findByRole('button', { name: /1 rate limit exceeded/i });
    expect(chip.className).toContain('text-status-error');
  });

  it('ignores legacy budget entries entirely', async () => {
    mockFetchLimits.mockResolvedValue(report([legacyBudgetEntry]));
    const { container } = render(<LimitsBadge />, { wrapper: MemoryRouter });
    await waitFor(() => expect(mockFetchLimits).toHaveBeenCalled());
    expect(container).toBeEmptyDOMElement();
  });
});

describe('state color helpers', () => {
  it('colors by state', () => {
    expect(limitStateFillClass('ok')).toBe('bg-primary/70');
    expect(limitStateFillClass('warn')).toBe('bg-status-pending');
    expect(limitStateFillClass('exceeded')).toBe('bg-status-error');
  });
});

describe('LimitsPanel', () => {
  it('renders nothing when unconfigured', () => {
    const { container } = render(<LimitsPanel summary={deriveLimitsSummary(report([], false))} />);
    expect(container).toBeEmptyDOMElement();
  });

  it('lists rate entries with elevated states first', () => {
    const summary = deriveLimitsSummary(
      report([rateEntry(), rateEntry({ state: 'exceeded', scope: 'tool', key: 'github__search_code' })]),
    );
    render(<LimitsPanel summary={summary} />);
    expect(screen.getByText('Rate Limits')).toBeInTheDocument();
    expect(screen.getAllByText(/30 calls\/min/)).toHaveLength(2);
    expect(screen.getByText('1 exceeded')).toBeInTheDocument();
    const items = screen.getAllByRole('listitem');
    expect(items[0].textContent).toContain('github__search_code');
  });

  it('renders nothing when only legacy budget entries are configured', () => {
    const summary = deriveLimitsSummary(report([legacyBudgetEntry]));
    const { container } = render(<LimitsPanel summary={summary} />);
    expect(container).toBeEmptyDOMElement();
  });
});

describe('limitsData helpers', () => {
  it('matches client rows through key normalization', () => {
    const entries = [rateEntry({ scope: 'client', key: 'Claude Code' })];
    expect(rateForRow(entries, 'client', 'claude-code')).toBe(entries[0]);
    expect(rateForRow(entries, 'client', 'cursor')).toBeUndefined();
  });

  it('matches server and tool scopes verbatim only', () => {
    const entries = [
      rateEntry({ scope: 'server', key: 'github' }),
      rateEntry({ scope: 'tool', key: 'github__search_code' }),
    ];
    expect(rateForRow(entries, 'server', 'github')).toBe(entries[0]);
    expect(rateForRow(entries, 'tool', 'github__search_code')).toBe(entries[1]);
    // A server limit never decorates the tool table and vice versa.
    expect(rateForRow(entries, 'tool', 'github')).toBeUndefined();
    expect(rateForRow(entries, 'server', 'github__search_code')).toBeUndefined();
  });

  it('derives worst state and counts', () => {
    const summary = deriveLimitsSummary(
      report([rateEntry({ state: 'warn' }), rateEntry({ state: 'exceeded', key: 'atlassian' }), rateEntry({ key: 'zapier' })]),
    );
    expect(summary.worst).toBe('exceeded');
    expect(summary.exceededCount).toBe(1);
    expect(summary.warnCount).toBe(1);
  });
});
