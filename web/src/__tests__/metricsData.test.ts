import { describe, it, expect } from 'vitest';
import {
  derivePerServerRows,
  derivePerClientRows,
  derivePerToolRows,
  sortBreakdownRows,
  aggregateModelMix,
  deriveSessionKpis,
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

describe('aggregateModelMix', () => {
  it('sums cost per model across servers, descending, with recomputed shares', () => {
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
    const mix = aggregateModelMix(servers, {});
    expect(mix.map((m) => m.model)).toEqual(['gpt-4o', 'gpt-4o-mini']);
    expect(mix[0].cost_usd).toBeCloseTo(0.8);
    expect(mix[0].share).toBeCloseTo(0.8); // 0.8 / 1.0 total
    expect(mix.reduce((s, m) => s + m.share, 0)).toBeCloseTo(1);
  });

  it('falls back to client breakdowns when no server priced', () => {
    const clients: Record<string, EffectiveModel> = {
      claude: { provenance: 'declared', model: 'claude-opus', models: [{ model: 'claude-opus', cost_usd: 0.5, share: 1 }] },
    };
    const mix = aggregateModelMix({}, clients);
    expect(mix).toHaveLength(1);
    expect(mix[0].model).toBe('claude-opus');
  });

  it('returns [] when nothing is priced', () => {
    expect(aggregateModelMix({}, {})).toEqual([]);
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
