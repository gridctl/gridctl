import { describe, it, expect } from 'vitest';
import {
  derivePerServerRows,
  derivePerClientRows,
  derivePerToolRows,
  sortBreakdownRows,
  buildFocusedTokenChartData,
  deriveFocusedTotals,
  deriveSessionKpis,
  deriveWindowTotals,
  hasMetricsData,
  buildTokenChartData,
  toTokenPoints,
  formatChartTick,
  formatChartBucket,
  formatChartTooltipTitle,
  impactUnitLabel,
  findingToolsHref,
  type SessionKpis,
} from '../components/metrics/metricsData';
import type {
  OptimizeFinding,
  TokenUsage,
  TokenMetricsResponse,
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

describe('derivePerServerRows', () => {
  it('derives per-server token rows', () => {
    const rows = derivePerServerRows(tokenUsage());
    const github = rows.find((r) => r.name === 'github')!;
    const atlas = rows.find((r) => r.name === 'atlassian')!;
    expect(github.total).toBe(80);
    expect(atlas.total).toBe(60);
  });

  it('returns [] when there is no token usage', () => {
    expect(derivePerServerRows(null)).toEqual([]);
  });

  it('unions zero-traffic stack servers as selectable zero rows', () => {
    const rows = derivePerServerRows(tokenUsage(), ['github', 'atlassian', 'zapier']);
    const zapier = rows.find((r) => r.name === 'zapier')!;
    expect(zapier.total).toBe(0);
    expect(rows.find((r) => r.name === 'github')!.total).toBe(80);
  });
});

describe('derivePerClientRows', () => {
  it('derives rows from the per-client token snapshot', () => {
    const tu = tokenUsage({ per_client: { claude: { input_tokens: 10, output_tokens: 5, total_tokens: 15 } } });
    const rows = derivePerClientRows(tu);
    expect(rows.map((r) => r.name)).toEqual(['claude']);
    expect(rows[0].total).toBe(15);
  });

  it('returns [] when no per-client attribution exists', () => {
    expect(derivePerClientRows(tokenUsage())).toEqual([]);
    expect(derivePerClientRows(null)).toEqual([]);
  });
});

describe('derivePerToolRows', () => {
  const usage: ToolUsageResponse = {
    servers: {
      github: {
        create_issue: { calls: 4, lastCalledAt: '2026-07-01T00:00:00Z', inputTokens: 120, outputTokens: 80 },
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
    { name: 'b', input: 0, output: 0, total: 30 },
    { name: 'a', input: 0, output: 0, total: 10 },
    { name: 'c', input: 0, output: 0, total: 20 },
  ];

  it('sorts by total descending', () => {
    expect(sortBreakdownRows(rows, 'total', 'desc').map((r) => r.name)).toEqual(['b', 'c', 'a']);
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
});

describe('deriveFocusedTotals', () => {
  const windowTotals = { input: 60, output: 40, total: 100, isEmpty: false };

  it('sums the entity window and shares against fleet tokens', () => {
    const t = deriveFocusedTotals(
      [{ timestamp: 't', input_tokens: 6, output_tokens: 4, total_tokens: 10 }],
      windowTotals,
    );
    expect(t.tokens).toBe(10);
    expect(t.share).toBeCloseTo(0.1);
  });

  it('omits terms for absent series instead of reporting zeros', () => {
    // A focused client has no token series. It must read as "not
    // measurable", never 0.
    const t = deriveFocusedTotals(undefined, windowTotals);
    expect(t.tokens).toBeUndefined();
    expect(t.share).toBeUndefined();
  });

  it('reports no share when the fleet denominator is zero', () => {
    const t = deriveFocusedTotals(
      [{ timestamp: 't', input_tokens: 1, output_tokens: 1, total_tokens: 2 }],
      { input: 0, output: 0, total: 0, isEmpty: true },
    );
    expect(t.share).toBeUndefined();
  });
});

describe('deriveSessionKpis', () => {
  it('derives session totals and format savings', () => {
    const k = deriveSessionKpis(
      tokenUsage({
        format_savings: { original_tokens: 100, formatted_tokens: 66, saved_tokens: 34, savings_percent: 34 },
      }),
    );
    expect(k.total).toBe(140);
    expect(k.savingsPercent).toBe(34);
    expect(k.savedTokens).toBe(34);
  });

  it('zeroes everything for a null snapshot', () => {
    const k = deriveSessionKpis(null);
    expect(k.total).toBe(0);
    expect(k.savingsPercent).toBe(0);
  });
});

describe('hasMetricsData', () => {
  const emptyKpis: SessionKpis = {
    input: 0, output: 0, total: 0, savingsPercent: 0, savedTokens: 0,
  };

  it('is false with no totals and no series', () => {
    expect(hasMetricsData(emptyKpis, null)).toBe(false);
  });

  it('is true when the session has tokens', () => {
    expect(hasMetricsData({ ...emptyKpis, total: 5 }, null)).toBe(true);
  });

  it('is true when a token series has points', () => {
    const series = { range: '1h', interval: '1m', data_points: [{ timestamp: 't', input_tokens: 1, output_tokens: 1, total_tokens: 2 }], per_server: {} } as TokenMetricsResponse;
    expect(hasMetricsData(emptyKpis, series)).toBe(true);
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

  it('sums token buckets across the window', () => {
    const w = deriveWindowTotals(tokenSeries);
    expect(w.input).toBe(12);
    expect(w.output).toBe(8);
    expect(w.total).toBe(20);
    expect(w.isEmpty).toBe(false);
  });

  it('returns zeros and isEmpty for series with no buckets', () => {
    const emptyTokens: TokenMetricsResponse = { range: '1h', interval: '1m', data_points: [], per_server: {} };
    const w = deriveWindowTotals(emptyTokens);
    expect(w.total).toBe(0);
    expect(w.isEmpty).toBe(true);
  });

  it('treats a loaded series with null points as an empty window, not unknown', () => {
    // The backend marshals an empty downsampled range as null data_points.
    const nullTokens = { range: '24h', interval: '1h', data_points: null, per_server: {} } as unknown as TokenMetricsResponse;
    const w = deriveWindowTotals(nullTokens);
    expect(w.total).toBe(0);
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
    expect(out[0].time).toBe('2026-01-01T00:00:00Z');
    expect(out[0]['Input Tokens']).toBe(3);
    expect(out[0]['Output Tokens']).toBe(2);
  });

  it('toTokenPoints keeps the raw timestamp as time', () => {
    const out = toTokenPoints([
      { timestamp: '2026-11-20T09:00:00Z', input_tokens: 10, output_tokens: 5, total_tokens: 15 },
      { timestamp: '2026-11-21T09:00:00Z', input_tokens: 20, output_tokens: 8, total_tokens: 28 },
    ]);
    expect(out.map((r) => r.time)).toEqual(['2026-11-20T09:00:00Z', '2026-11-21T09:00:00Z']);
  });

  it('buildFocusedTokenChartData keeps the raw timestamp as time', () => {
    const fleet: TokenMetricsResponse = {
      range: '7d',
      interval: '1h',
      data_points: [
        { timestamp: '2026-11-20T09:00:00Z', input_tokens: 10, output_tokens: 5, total_tokens: 15 },
        { timestamp: '2026-11-21T09:00:00Z', input_tokens: 20, output_tokens: 8, total_tokens: 28 },
      ],
      per_server: {},
    };
    const out = buildFocusedTokenChartData(fleet, [
      { timestamp: '2026-11-21T09:00:00Z', input_tokens: 7, output_tokens: 3, total_tokens: 10 },
    ]);
    expect(out.map((r) => r.time)).toEqual(['2026-11-20T09:00:00Z', '2026-11-21T09:00:00Z']);
    expect(out[0]['Input Tokens']).toBe(0);
    expect(out[1]['Input Tokens']).toBe(7);
  });

  it('returns [] for null input', () => {
    expect(buildTokenChartData(null)).toEqual([]);
  });
});

describe('chart timestamp formatters', () => {
  it('formatChartTick produces 72 unique month-bearing labels across three days on 7d', () => {
    const start = Date.parse('2026-11-20T00:00:00Z');
    const labels = Array.from({ length: 72 }, (_, i) =>
      formatChartTick(new Date(start + i * 3_600_000).toISOString(), '7d', 'en-US', 'UTC'),
    );
    expect(new Set(labels).size).toBe(72);
    for (const label of labels) {
      expect(label).toContain('Nov');
    }
  });

  it('formatChartTick produces 25 unique weekday-bearing labels across 24h', () => {
    const start = Date.parse('2026-11-20T09:00:00Z');
    const labels = Array.from({ length: 25 }, (_, i) =>
      formatChartTick(new Date(start + i * 3_600_000).toISOString(), '24h', 'en-US', 'UTC'),
    );
    expect(new Set(labels).size).toBe(25);
    expect(labels[0]).toMatch(/^Fri\b/);
    expect(labels[24]).toMatch(/^Sat\b/);
  });

  it('formatChartTick keeps hours and minutes only for 30m, 1h, and 6h', () => {
    for (const range of ['30m', '1h', '6h']) {
      const label = formatChartTick('2026-11-20T09:05:00Z', range, 'en-US', 'UTC');
      expect(label).toMatch(/09:05\s*AM/);
      expect(label).not.toContain('Fri');
      expect(label).not.toContain('Nov');
    }
  });

  it('formatChartTick falls back to month and day for unknown or missing ranges', () => {
    for (const range of [undefined, '30d']) {
      const label = formatChartTick('2026-11-20T09:00:00Z', range, 'en-US', 'UTC');
      expect(label).toContain('Nov');
      expect(label).toContain('20');
      expect(label).toMatch(/09:00\s*AM/);
    }
  });

  it('formatChartTooltipTitle includes full date, start and end time, and interval suffix', () => {
    const hourly = formatChartTooltipTitle('2026-11-20T09:00:00Z', '1h', 'en-US', 'UTC');
    expect(hourly).toContain('Fri');
    expect(hourly).toContain('Nov');
    expect(hourly).toContain('20');
    expect(hourly).toContain('2026');
    expect(hourly).toContain('09:00');
    expect(hourly).toContain('10:00');
    expect(hourly).toContain('1h interval');

    const minute = formatChartTooltipTitle('2026-11-20T09:00:00Z', '1m', 'en-US', 'UTC');
    expect(minute).toContain('09:00');
    expect(minute).toContain('09:01');
    expect(minute).toContain('1m interval');

    const unknown = formatChartTooltipTitle('2026-11-20T09:00:00Z', '5m', 'en-US', 'UTC');
    expect(unknown).toContain('2026');
    expect(unknown).toContain('09:00');
    expect(unknown).not.toContain('–');
    expect(unknown).not.toContain('interval');
  });

  it('formatChartBucket includes full date and start-end time without interval suffix', () => {
    const hourly = formatChartBucket('2026-11-20T09:00:00Z', '1h', 'en-US', 'UTC');
    expect(hourly).toContain('Fri');
    expect(hourly).toContain('Nov');
    expect(hourly).toContain('20');
    expect(hourly).toContain('2026');
    expect(hourly).toContain('09:00');
    expect(hourly).toContain('10:00');
    expect(hourly).toContain('–');
    expect(hourly).not.toContain('interval');

    const minute = formatChartBucket('2026-11-20T09:00:00Z', '1m', 'en-US', 'UTC');
    expect(minute).toContain('09:00');
    expect(minute).toContain('09:01');
    expect(minute).not.toContain('interval');

    const unknown = formatChartBucket('2026-11-20T09:00:00Z', undefined, 'en-US', 'UTC');
    expect(unknown).toContain('2026');
    expect(unknown).toContain('09:00');
    expect(unknown).not.toContain('–');
  });

  it('returns the input unchanged for unparseable timestamps', () => {
    for (const bad of ['not-a-timestamp', '']) {
      expect(formatChartTick(bad, '7d', 'en-US', 'UTC')).toBe(bad);
      expect(formatChartBucket(bad, '1h', 'en-US', 'UTC')).toBe(bad);
      expect(formatChartTooltipTitle(bad, '1h', 'en-US', 'UTC')).toBe(bad);
    }
  });
});

function makeFinding(over: Partial<OptimizeFinding> = {}): OptimizeFinding {
  return {
    id: 'f1',
    heuristic: 'unused_server',
    severity: 'warn',
    title: 'Unused server: zapier',
    summary: 'No calls observed.',
    remediation: '',
    detected_at: '2026-11-20T00:00:00Z',
    ...over,
  };
}

describe('impactUnitLabel', () => {
  it('returns heuristic-specific labels for known heuristics and a fallback for unknown ones', () => {
    expect(impactUnitLabel('unused_server')).toBe(
      "Estimated schema tokens sent per week for this server's tools. Not recorded usage.",
    );
    expect(impactUnitLabel('unused_tool')).toBe(
      'Estimated schema tokens sent per week for this tool. Not recorded usage.',
    );
    expect(impactUnitLabel('schema_overhead')).toBe(
      "Estimated schema tokens sent per week for this server's tool list. Not recorded usage.",
    );
    expect(impactUnitLabel('format_savings_shortfall')).toBe(
      'Projected output tokens saved per week, from savings measured on other servers.',
    );
    expect(impactUnitLabel('unknown_future_heuristic')).toBe('Estimated tokens per week.');
  });
});

describe('findingToolsHref', () => {
  it('builds Tools audit URLs for schema heuristics with server and optional tool', () => {
    expect(findingToolsHref(makeFinding({ heuristic: 'unused_server', server: 'zapier' }))).toBe(
      '/tools?server=zapier&audit=1',
    );
    expect(findingToolsHref(makeFinding({ heuristic: 'schema_overhead', server: 'github' }))).toBe(
      '/tools?server=github&audit=1',
    );
    expect(
      findingToolsHref(makeFinding({ heuristic: 'unused_tool', server: 'github', tool: 'list_repos' })),
    ).toBe('/tools?server=github&audit=1&tool=list_repos');
  });

  it('returns null without a server or for non-schema heuristics', () => {
    expect(findingToolsHref(makeFinding({ heuristic: 'unused_server', server: undefined }))).toBeNull();
    expect(
      findingToolsHref(makeFinding({ heuristic: 'unused_tool', server: undefined, tool: 'list_repos' })),
    ).toBeNull();
    expect(
      findingToolsHref(makeFinding({ heuristic: 'format_savings_shortfall', server: 'github' })),
    ).toBeNull();
    expect(findingToolsHref(makeFinding({ heuristic: 'unlisted_heuristic', server: 'github' }))).toBeNull();
  });
});
