import { describe, it, expect } from 'vitest';
import {
  avgTokensPerCall,
  breakdownRowsToCsv,
  derivePerServerRows,
  derivePerToolRows,
  sortBreakdownRows,
  windowColumnCaption,
  windowColumnLabel,
  type BreakdownRow,
} from '../components/metrics/metricsData';
import { metricsExportFilename } from '../lib/download';
import type { TokenMetricsResponse, TokenUsage, ToolUsageResponse } from '../types';

const tokenUsage: TokenUsage = {
  session: { input_tokens: 100, output_tokens: 40, total_tokens: 140 },
  per_server: {
    github: { input_tokens: 60, output_tokens: 20, total_tokens: 80 },
    atlassian: { input_tokens: 40, output_tokens: 20, total_tokens: 60 },
  },
  format_savings: { original_tokens: 0, formatted_tokens: 0, saved_tokens: 0, savings_percent: 0 },
};

const toolUsage: ToolUsageResponse = {
  servers: {
    github: {
      create_issue: { calls: 4, lastCalledAt: '2026-07-01T00:00:00Z', inputTokens: 120, outputTokens: 80 },
      list_repos: { calls: 1, lastCalledAt: '2026-07-02T00:00:00Z', inputTokens: 30, outputTokens: 10 },
    },
  },
};

function series(perServer: Record<string, unknown>): TokenMetricsResponse {
  return { range: '1h', interval: '1m', data_points: [], per_server: perServer } as unknown as TokenMetricsResponse;
}

const row = (over: Partial<BreakdownRow>): BreakdownRow => ({ name: 'x', input: 0, output: 0, total: 0, ...over });

describe('avgTokensPerCall', () => {
  it('rounds total over calls and is undefined without calls', () => {
    expect(avgTokensPerCall(row({ total: 200, calls: 4 }))).toBe(50);
    expect(avgTokensPerCall(row({ total: 10, calls: 3 }))).toBe(3);
    expect(avgTokensPerCall(row({ total: 10, calls: 0 }))).toBeUndefined();
    expect(avgTokensPerCall(row({ total: 10 }))).toBeUndefined();
  });

  it('prefers a precomputed average', () => {
    expect(avgTokensPerCall(row({ total: 999, calls: 1, avg: 40 }))).toBe(40);
  });
});

describe('derivePerToolRows lastUsed', () => {
  it('carries a parseable timestamp and drops an absent or invalid one', () => {
    const rows = derivePerToolRows({
      servers: {
        s: {
          a: { calls: 1, lastCalledAt: '2026-07-01T00:00:00Z' },
          b: { calls: 1 },
          c: { calls: 1, lastCalledAt: 'not a time' },
        },
      },
    });
    expect(rows.find((r) => r.tool === 'a')!.lastUsed).toBe('2026-07-01T00:00:00Z');
    expect(rows.find((r) => r.tool === 'b')!.lastUsed).toBeUndefined();
    expect(rows.find((r) => r.tool === 'c')!.lastUsed).toBeUndefined();
  });
});

describe('derivePerServerRows with tool usage and series', () => {
  it('keeps the array signature working', () => {
    const rows = derivePerServerRows(tokenUsage, ['github', 'zapier']);
    expect(rows.map((r) => r.name).sort()).toEqual(['atlassian', 'github', 'zapier']);
    expect(rows.every((r) => r.calls === undefined && r.windowTotal === undefined)).toBe(true);
  });

  it('aggregates calls, newest last used, and the tool-usage average', () => {
    const rows = derivePerServerRows(tokenUsage, { serverNames: ['zapier'], toolUsage });
    const github = rows.find((r) => r.name === 'github')!;
    expect(github.calls).toBe(5);
    expect(github.lastUsed).toBe('2026-07-02T00:00:00Z');
    // (200 + 40) / 5 from tool usage, not status total 80 / 5.
    expect(avgTokensPerCall(github)).toBe(48);
    const zapier = rows.find((r) => r.name === 'zapier')!;
    expect(zapier.calls).toBe(0);
    expect(zapier.lastUsed).toBeUndefined();
    expect(avgTokensPerCall(zapier)).toBeUndefined();
    expect(zapier.total).toBe(0);
  });

  it('leaves calls undefined when tool usage is not loaded', () => {
    const rows = derivePerServerRows(tokenUsage, { toolUsage: null });
    expect(rows.find((r) => r.name === 'github')!.calls).toBeUndefined();
    expect(rows.find((r) => r.name === 'github')!.lastUsed).toBeUndefined();
  });

  it('sums the ranged series, zero when loaded but absent, undefined when unloaded', () => {
    const data = series({
      github: [
        { timestamp: 't1', input_tokens: 5, output_tokens: 1, total_tokens: 6 },
        { timestamp: 't2', input_tokens: 3, output_tokens: 2, total_tokens: 5 },
      ],
      atlassian: null,
    });
    const rows = derivePerServerRows(tokenUsage, { serverNames: ['zapier'], metricsData: data });
    expect(rows.find((r) => r.name === 'github')!.windowTotal).toBe(11);
    expect(rows.find((r) => r.name === 'atlassian')!.windowTotal).toBe(0);
    expect(rows.find((r) => r.name === 'zapier')!.windowTotal).toBe(0);
    const unloaded = derivePerServerRows(tokenUsage, { metricsData: null });
    expect(unloaded.every((r) => r.windowTotal === undefined)).toBe(true);
  });
});

describe('sortBreakdownRows extended columns', () => {
  const rows = [
    row({ name: 'b', calls: 1, lastUsed: '2026-07-01T00:00:00Z', windowTotal: 5 }),
    row({ name: 'a' }),
    row({ name: 'c', calls: 4, lastUsed: '2026-07-03T00:00:00Z', windowTotal: 5 }),
    row({ name: 'd', calls: 1 }),
  ];

  it('sorts calls with ties broken by name', () => {
    expect(sortBreakdownRows(rows, 'calls', 'desc').map((r) => r.name)).toEqual(['c', 'b', 'd', 'a']);
    expect(sortBreakdownRows(rows, 'calls', 'asc').map((r) => r.name)).toEqual(['b', 'd', 'c', 'a']);
  });

  it('sorts lastUsed by time with undefined last in both directions', () => {
    expect(sortBreakdownRows(rows, 'lastUsed', 'desc').map((r) => r.name)).toEqual(['c', 'b', 'a', 'd']);
    expect(sortBreakdownRows(rows, 'lastUsed', 'asc').map((r) => r.name)).toEqual(['b', 'c', 'a', 'd']);
  });

  it('sorts window and avg with undefined last', () => {
    expect(sortBreakdownRows(rows, 'window', 'asc').map((r) => r.name)).toEqual(['b', 'c', 'a', 'd']);
    expect(sortBreakdownRows(rows, 'avg', 'desc').map((r) => r.name)).toEqual(['b', 'c', 'd', 'a']);
  });
});

describe('window column strings', () => {
  it('builds the caption from the header string', () => {
    expect(windowColumnLabel('Last 1h')).toBe('Last 1h tokens');
    expect(windowColumnCaption('Last 1h')).toBe(
      'The Last 1h tokens column follows the range control. Every other number is a session total.',
    );
  });
});

describe('breakdownRowsToCsv', () => {
  it('writes the tools header, raw values, empty fields, and CRLF', () => {
    const csv = breakdownRowsToCsv(
      [
        row({ name: 'g__c', server: 'github', tool: 'create_issue', calls: 4, input: 1200, output: 800, total: 2000, lastUsed: '2026-07-01T00:00:00Z' }),
        row({ name: 'g__l', server: 'github', tool: 'list_repos', calls: 0 }),
      ],
      'tools',
    );
    expect(csv).toBe(
      'server,tool,calls,input_tokens,output_tokens,total_tokens,avg_tokens_per_call,last_used\r\n' +
        'github,create_issue,4,1200,800,2000,500,2026-07-01T00:00:00Z\r\n' +
        'github,list_repos,0,0,0,0,,\r\n',
    );
  });

  it('writes the servers header with the API window', () => {
    const csv = breakdownRowsToCsv([row({ name: 'github', calls: 5, avg: 48, windowTotal: 11, total: 80 })], 'servers', {
      window: '1h',
    });
    expect(csv.split('\r\n')[0]).toBe(
      'server,calls,window,window_tokens,input_tokens,output_tokens,total_tokens,avg_tokens_per_call,last_used',
    );
    expect(csv.split('\r\n')[1]).toBe('github,5,1h,11,0,0,80,48,');
  });

  it('writes the clients header', () => {
    expect(breakdownRowsToCsv([row({ name: 'cli', total: 3 })], 'clients')).toBe(
      'client,input_tokens,output_tokens,total_tokens\r\ncli,0,0,3\r\n',
    );
  });

  it('quotes per RFC 4180 and guards formula-leading text', () => {
    const lines = breakdownRowsToCsv(
      [
        row({ name: 'a,b' }),
        row({ name: 'say "hi"' }),
        row({ name: '=SUM(A1)' }),
        row({ name: '+1' }),
        row({ name: '-2' }),
        row({ name: '@x' }),
        row({ name: '=a,b' }),
      ],
      'clients',
    ).split('\r\n');
    expect(lines[1]).toBe('"a,b",0,0,0');
    expect(lines[2]).toBe('"say ""hi""",0,0,0');
    expect(lines[3]).toBe("'=SUM(A1),0,0,0");
    expect(lines[4]).toBe("'+1,0,0,0");
    expect(lines[5]).toBe("'-2,0,0,0");
    expect(lines[6]).toBe("'@x,0,0,0");
    expect(lines[7]).toBe(`"'=a,b",0,0,0`);
  });
});

describe('metricsExportFilename', () => {
  it('names the kind and a timestamp', () => {
    expect(metricsExportFilename('tools')).toMatch(/^gridctl-metrics-tools-\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}\.csv$/);
  });
});
