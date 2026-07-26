import { describe, it, expect } from 'vitest';
import {
  derivePerServerRows,
  derivePerClientRows,
  derivePerToolRows,
  sortBreakdownRows,
  aggregateModelRows,
  buildFocusedCostChartData,
  buildFocusedTokenChartData,
  deriveFocusedTotals,
  deriveSessionKpis,
  deriveWindowTotals,
  hasMetricsData,
  buildTokenChartData,
  buildCostChartData,
  type SessionKpis,
} from '../components/metrics/metricsData';
import type {
  TokenUsage,
  CostUsage,
  EffectiveModel,
  TokenMetricsResponse,
  CostMetricsResponse,
  ToolUsageResponse,
} from '../types';

function tokenUsage(over: Partial<TokenUsage> = {}): TokenUsage {
  return {
    session: { input_tokens: 100, output_tokens: 40, total_tokens: 140 },
    per_server: {
      github: { input_tokens: 60, output_tokens: 20, total_tokens: 80 },
      atlassian: { input_tokens: 40, output_tokens: 20, total_tokens: 60 },
    },
    format_savings: { original_tokens: 0, formatted_tokens: 0, saved_tokens: 0, savings_percent: 0 },
    ...over,
  };
}

function costUsage(over: Partial<CostUsage> = {}): CostUsage {
  return {
    session: { input_usd: 0.2, output_usd: 0.1, total_usd: 0.3 },
    per_server: {
      github: { input_usd: 0.15, output_usd: 0.05, total_usd: 0.2 },
    },
    ...over,
  };
}

describe('derivePerServerRows', () => {
  it('joins per-server tokens with per-server cost; unknown cost is undefined', () => {
    const rows = derivePerServerRows(tokenUsage(), costUsage());
    const github = rows.find((r) => r.name === 'github')!;
    const atlas = rows.find((r) => r.name === 'atlassian')!;
    expect(github.total).toBe(80);
    expect(github.cost).toBe(0.2);
    // atlassian has tokens but no cost entry → undefined (renders as em-dash)
    expect(atlas.cost).toBeUndefined();
  });

  it('returns [] when there is no token usage', () => {
    expect(derivePerServerRows(null)).toEqual([]);
  });

  it('unions zero-traffic stack servers as selectable zero rows', () => {
    const rows = derivePerServerRows(tokenUsage(), costUsage(), ['github', 'atlassian', 'zapier']);
    const zapier = rows.find((r) => r.name === 'zapier')!;
    expect(zapier.total).toBe(0);
    expect(zapier.cost).toBeUndefined(); // unknown, never $0
    expect(rows.find((r) => r.name === 'github')!.total).toBe(80);
  });
});

describe('derivePerClientRows', () => {
  it('unions clients across token and cost snapshots', () => {
    const tu = tokenUsage({ per_client: { claude: { input_tokens: 10, output_tokens: 5, total_tokens: 15 } } });
    const cu = costUsage({ per_client: { cursor: { input_usd: 0.01, output_usd: 0, total_usd: 0.01 } } });
    const rows = derivePerClientRows(tu, cu);
    const names = rows.map((r) => r.name).sort();
    expect(names).toEqual(['claude', 'cursor']);
    // claude has tokens but no cost → undefined; cursor has cost but zero tokens
    expect(rows.find((r) => r.name === 'claude')!.cost).toBeUndefined();
    expect(rows.find((r) => r.name === 'cursor')!.cost).toBe(0.01);
    expect(rows.find((r) => r.name === 'cursor')!.total).toBe(0);
  });
});

describe('derivePerToolRows', () => {
  const usage: ToolUsageResponse = {
    servers: {
      github: {
        create_issue: { calls: 4, lastCalledAt: '2026-07-01T00:00:00Z', inputTokens: 120, outputTokens: 80, costUsd: 0.003 },
        list_repos: { calls: 1, inputTokens: 30, outputTokens: 10 },
      },
      atlassian: {
        // Same tool name on another server must stay a distinct row.
        create_issue: { calls: 2, inputTokens: 5, outputTokens: 5 },
      },
    },
  };

  it('flattens server→tool usage into rows with unique server-prefixed names', () => {
    const rows = derivePerToolRows(usage);
    expect(rows).toHaveLength(3);
    const names = rows.map((r) => r.name).sort();
    expect(names).toEqual(['atlassian__create_issue', 'github__create_issue', 'github__list_repos']);
    const gh = rows.find((r) => r.name === 'github__create_issue')!;
    expect(gh.server).toBe('github');
    expect(gh.tool).toBe('create_issue');
    expect(gh.calls).toBe(4);
    expect(gh.input).toBe(120);
    expect(gh.output).toBe(80);
    expect(gh.total).toBe(200);
    expect(gh.cost).toBe(0.003);
  });

  it('leaves cost undefined for unpriced tools (em-dash rule)', () => {
    const rows = derivePerToolRows(usage);
    expect(rows.find((r) => r.name === 'github__list_repos')!.cost).toBeUndefined();
  });

  it('defaults missing token fields to zero (legacy gateway responses)', () => {
    const legacy: ToolUsageResponse = { servers: { github: { old_tool: { calls: 7 } } } };
    const row = derivePerToolRows(legacy)[0];
    expect(row.input).toBe(0);
    expect(row.output).toBe(0);
    expect(row.total).toBe(0);
    expect(row.calls).toBe(7);
  });

  it('returns [] for null usage', () => {
    expect(derivePerToolRows(null)).toEqual([]);
  });
});

describe('sortBreakdownRows', () => {
  const rows = [
    { name: 'b', input: 0, output: 0, total: 30, cost: 0.3 },
    { name: 'a', input: 0, output: 0, total: 10, cost: undefined },
    { name: 'c', input: 0, output: 0, total: 20, cost: 0.1 },
  ];

  it('sorts by total descending', () => {
    expect(sortBreakdownRows(rows, 'total', 'desc').map((r) => r.name)).toEqual(['b', 'c', 'a']);
  });

  it('sorts unknown cost to the bottom on descending', () => {
    expect(sortBreakdownRows(rows, 'cost', 'desc').map((r) => r.name)).toEqual(['b', 'c', 'a']);
  });

  it('sorts unknown cost to the top on ascending', () => {
    expect(sortBreakdownRows(rows, 'cost', 'asc').map((r) => r.name)).toEqual(['a', 'c', 'b']);
  });

  it('sorts by name', () => {
    expect(sortBreakdownRows(rows, 'name', 'asc').map((r) => r.name)).toEqual(['a', 'b', 'c']);
  });

  it('does not mutate the input array', () => {
    const copy = [...rows];
    sortBreakdownRows(rows, 'total', 'asc');
    expect(rows).toEqual(copy);
  });
});

describe('aggregateModelRows', () => {
  const servers: Record<string, EffectiveModel> = {
    github: {
      provenance: 'mixed',
      models: [
        { model: 'gpt-4o', cost_usd: 0.6, share: 0.75 },
        { model: 'gpt-4o-mini', cost_usd: 0.2, share: 0.25 },
      ],
    },
    atlassian: {
      provenance: 'declared',
      model: 'gpt-4o',
      models: [{ model: 'gpt-4o', cost_usd: 0.2, share: 1 }],
    },
  };

  it('sums cost per model across servers, descending, with recomputed shares', () => {
    const rows = aggregateModelRows(servers, {});
    expect(rows.map((m) => m.model)).toEqual(['gpt-4o', 'gpt-4o-mini']);
    expect(rows[0].cost_usd).toBeCloseTo(0.8);
    expect(rows[0].share).toBeCloseTo(0.8); // 0.8 / 1.0 total
    expect(rows.reduce((s, m) => s + m.share, 0)).toBeCloseTo(1);
  });

  it('returns [] when nothing is priced', () => {
    expect(aggregateModelRows({}, {})).toEqual([]);
  });

  it('collects entities and provenance counts per model', () => {
    const rows = aggregateModelRows(servers, {});
    const gpt4o = rows.find((r) => r.model === 'gpt-4o')!;
    expect(gpt4o.entities).toEqual(['atlassian', 'github']);
    expect(gpt4o.provenance).toEqual({ declared: 1, mixed: 1, none: 0 });
    expect(gpt4o.cost_usd).toBeCloseTo(0.8);
  });

  it('never double-counts: clients are ignored when any server priced', () => {
    const clients: Record<string, EffectiveModel> = {
      claude: { provenance: 'declared', model: 'gpt-4o', models: [{ model: 'gpt-4o', cost_usd: 1.0, share: 1 }] },
    };
    const rows = aggregateModelRows(servers, clients);
    const gpt4o = rows.find((r) => r.model === 'gpt-4o')!;
    // Server tier only: the client's 1.0 must not fold in.
    expect(gpt4o.cost_usd).toBeCloseTo(0.8);
    expect(gpt4o.entities).not.toContain('claude');
  });

  it('falls back to clients when no server priced', () => {
    const clients: Record<string, EffectiveModel> = {
      claude: { provenance: 'declared', model: 'claude-opus', models: [{ model: 'claude-opus', cost_usd: 0.5, share: 1 }] },
    };
    const rows = aggregateModelRows({}, clients);
    expect(rows).toHaveLength(1);
    expect(rows[0].entities).toEqual(['claude']);
  });
});

describe('focused chart builders', () => {
  const fleet: TokenMetricsResponse = {
    range: '30m', interval: '1m',
    data_points: [
      { timestamp: '2026-01-01T00:00:00Z', input_tokens: 10, output_tokens: 10, total_tokens: 20 },
      { timestamp: '2026-01-01T00:01:00Z', input_tokens: 5, output_tokens: 5, total_tokens: 10 },
    ],
    per_server: {},
  };

  it('zero-fills entity minutes missing from the fleet spine', () => {
    // Entity has a bucket only for the first fleet minute; the second is a
    // true zero (the fleet moved, this entity did not), never a gap.
    const rows = buildFocusedTokenChartData(fleet, [
      { timestamp: '2026-01-01T00:00:00Z', input_tokens: 3, output_tokens: 2, total_tokens: 5 },
    ]);
    expect(rows).toHaveLength(2);
    expect(rows[0]['Input Tokens']).toBe(3);
    expect(rows[1]['Input Tokens']).toBe(0);
    expect(rows[1]['Output Tokens']).toBe(0);
    expect(rows[0]['Fleet Total']).toBe(20);
    expect(rows[1]['Fleet Total']).toBe(10);
  });

  it('merges cost on the fleet spine with zero-fill', () => {
    const fleetCost: CostMetricsResponse = {
      range: '30m', interval: '1m',
      data_points: [
        { timestamp: '2026-01-01T00:00:00Z', usd: 0.05 },
        { timestamp: '2026-01-01T00:01:00Z', usd: 0.02 },
      ],
      per_server: {},
    };
    const rows = buildFocusedCostChartData(fleetCost, [{ timestamp: '2026-01-01T00:01:00Z', usd: 0.01 }]);
    expect(rows[0]['Cost (USD)']).toBe(0);
    expect(rows[1]['Cost (USD)']).toBe(0.01);
    expect(rows[0]['Fleet']).toBe(0.05);
  });
});

describe('deriveFocusedTotals', () => {
  const windowTotals = { input: 60, output: 40, total: 100, costUSD: 0.5, isEmpty: false };

  it('sums the entity window and shares against fleet cost when priced', () => {
    const t = deriveFocusedTotals(
      [{ timestamp: 't', input_tokens: 6, output_tokens: 4, total_tokens: 10 }],
      [{ timestamp: 't', usd: 0.1 }],
      windowTotals,
    );
    expect(t.tokens).toBe(10);
    expect(t.costUSD).toBeCloseTo(0.1);
    expect(t.share).toBeCloseTo(0.2);
  });

  it('falls back to token share when no cost series exists', () => {
    const t = deriveFocusedTotals(
      [{ timestamp: 't', input_tokens: 6, output_tokens: 4, total_tokens: 10 }],
      undefined,
      { ...windowTotals, costUSD: undefined },
    );
    expect(t.costUSD).toBeUndefined();
    expect(t.share).toBeCloseTo(0.1);
  });

  it('omits terms for absent or empty series instead of reporting zeros', () => {
    // A focused client has no token series; a server can have an empty cost
    // ring in the window. Both must read as "not measurable", never 0.
    const t = deriveFocusedTotals(undefined, [], windowTotals);
    expect(t.tokens).toBeUndefined();
    expect(t.costUSD).toBeUndefined();
    expect(t.share).toBeUndefined();
  });

  it('reports no share when the fleet denominator is zero', () => {
    const t = deriveFocusedTotals(
      [{ timestamp: 't', input_tokens: 1, output_tokens: 1, total_tokens: 2 }],
      undefined,
      { input: 0, output: 0, total: 0, costUSD: undefined, isEmpty: true },
    );
    expect(t.share).toBeUndefined();
  });
});

describe('deriveSessionKpis', () => {
  it('marks hasCost and suppresses the attribution hint when cost exists', () => {
    const k = deriveSessionKpis(tokenUsage(), costUsage(), true, {}, {});
    expect(k.hasCost).toBe(true);
    expect(k.costUSD).toBe(0.3);
    expect(k.showAttributionHint).toBe(false);
  });

  it('shows the attribution hint when no attribution and no cost', () => {
    const k = deriveSessionKpis(tokenUsage(), null, false, {}, {});
    expect(k.hasCost).toBe(false);
    expect(k.showAttributionHint).toBe(true);
  });

  it('flags mixed provenance from either client or server effective models', () => {
    const k = deriveSessionKpis(tokenUsage(), costUsage(), true, {}, { github: { provenance: 'mixed' } });
    expect(k.hasMixedProvenance).toBe(true);
  });
});

describe('hasMetricsData', () => {
  const emptyKpis: SessionKpis = {
    input: 0, output: 0, total: 0, costUSD: undefined, hasCost: false,
    savingsPercent: 0, savedTokens: 0, showAttributionHint: true, hasMixedProvenance: false,
  };

  it('is false with no totals and no series', () => {
    expect(hasMetricsData(emptyKpis, null, null)).toBe(false);
  });

  it('is true when the session has tokens', () => {
    expect(hasMetricsData({ ...emptyKpis, total: 5 }, null, null)).toBe(true);
  });

  it('is true when a token series has points', () => {
    const series = { range: '1h', interval: '1m', data_points: [{ timestamp: 't', input_tokens: 1, output_tokens: 1, total_tokens: 2 }], per_server: {} } as TokenMetricsResponse;
    expect(hasMetricsData(emptyKpis, series, null)).toBe(true);
  });
});

describe('deriveWindowTotals', () => {
  const tokenSeries = {
    range: '1h', interval: '1m',
    data_points: [
      { timestamp: 't1', input_tokens: 7, output_tokens: 3, total_tokens: 10 },
      { timestamp: 't2', input_tokens: 5, output_tokens: 5, total_tokens: 10 },
    ],
    per_server: {},
  } as TokenMetricsResponse;
  const costSeries = {
    range: '1h', interval: '1m',
    data_points: [{ timestamp: 't1', usd: 0.02 }, { timestamp: 't2', usd: 0.03 }],
    per_server: {},
  } as CostMetricsResponse;

  it('sums token and cost buckets across the window', () => {
    const w = deriveWindowTotals(tokenSeries, costSeries);
    expect(w.input).toBe(12);
    expect(w.output).toBe(8);
    expect(w.total).toBe(20);
    expect(w.costUSD).toBeCloseTo(0.05);
    expect(w.isEmpty).toBe(false);
  });

  it('returns zeros and isEmpty for series with no buckets', () => {
    const emptyTokens: TokenMetricsResponse = { range: '1h', interval: '1m', data_points: [], per_server: {} };
    const emptyCost: CostMetricsResponse = { range: '1h', interval: '1m', data_points: [], per_server: {} };
    const w = deriveWindowTotals(emptyTokens, emptyCost);
    expect(w.total).toBe(0);
    expect(w.costUSD).toBe(0);
    expect(w.isEmpty).toBe(true);
  });

  it('leaves costUSD undefined when no cost series has loaded', () => {
    const w = deriveWindowTotals(tokenSeries, null);
    expect(w.costUSD).toBeUndefined();
    expect(w.isEmpty).toBe(false);
  });

  it('treats a loaded series with null points as an empty window, not unknown', () => {
    // The backend marshals an empty downsampled range as null data_points.
    const nullCost = { range: '24h', interval: '1h', data_points: null, per_server: {} } as unknown as CostMetricsResponse;
    const w = deriveWindowTotals(null, nullCost);
    expect(w.costUSD).toBe(0);
    expect(w.isEmpty).toBe(true);
  });
});

describe('chart data builders', () => {
  it('buildTokenChartData maps points to input/output series', () => {
    const series = {
      range: '1h', interval: '1m',
      data_points: [{ timestamp: '2026-01-01T00:00:00Z', input_tokens: 3, output_tokens: 2, total_tokens: 5 }],
      per_server: {},
    } as TokenMetricsResponse;
    const out = buildTokenChartData(series);
    expect(out).toHaveLength(1);
    expect(out[0]['Input Tokens']).toBe(3);
    expect(out[0]['Output Tokens']).toBe(2);
  });

  it('buildCostChartData maps points to a USD series', () => {
    const series = {
      range: '1h', interval: '1m',
      data_points: [{ timestamp: '2026-01-01T00:00:00Z', usd: 0.42 }],
      per_server: {},
    } as CostMetricsResponse;
    const out = buildCostChartData(series);
    expect(out[0]['Cost (USD)']).toBe(0.42);
  });

  it('returns [] for null inputs', () => {
    expect(buildTokenChartData(null)).toEqual([]);
    expect(buildCostChartData(null)).toEqual([]);
  });
});
