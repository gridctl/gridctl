import { describe, it, expect } from 'vitest';
import {
  GATEWAY_LOG_SOURCE,
  filterParsedLogs,
  formatTimestamp,
  logEntryKeys,
  logSourceOf,
  normalizeLogTimeRangeParam,
  normalizeLogWindowParam,
  parseLogEntry,
  serializeLogsJSONL,
  serializeLogsText,
  type ParsedLog,
} from '../components/log/logTypes';

function entry(over: Partial<ParsedLog>): ParsedLog {
  return {
    level: 'INFO',
    timestamp: '2026-07-23T10:00:00Z',
    message: 'hello',
    raw: 'hello',
    ...over,
  };
}

describe('logSourceOf', () => {
  it('reads the server attribute when present', () => {
    expect(logSourceOf(entry({ attrs: { server: 'github' } }))).toBe('github');
  });

  it('falls back to the gateway source without a server attribute', () => {
    expect(logSourceOf(entry({}))).toBe(GATEWAY_LOG_SOURCE);
    expect(logSourceOf(entry({ attrs: { other: 'x' } }))).toBe(GATEWAY_LOG_SOURCE);
    expect(logSourceOf(entry({ attrs: { server: '' } }))).toBe(GATEWAY_LOG_SOURCE);
  });

  it('classifies parsed structured entries by their server attr', () => {
    const parsed = parseLogEntry({
      level: 'ERROR',
      ts: '2026-07-23T10:00:00Z',
      msg: 'boom',
      attrs: { server: 'zapier' },
    });
    expect(logSourceOf(parsed)).toBe('zapier');
  });
});

describe('filterParsedLogs', () => {
  const logs: ParsedLog[] = [
    entry({ message: 'gateway up', component: 'gateway' }),
    entry({ level: 'ERROR', message: 'call failed', attrs: { server: 'github' }, traceId: 'abc123' }),
    entry({ level: 'DEBUG', message: 'poll tick', attrs: { server: 'zapier' } }),
  ];

  it('passes everything through with no filter', () => {
    expect(filterParsedLogs(logs, {})).toHaveLength(3);
  });

  it('filters by source, treating gateway as entries without a server attr', () => {
    expect(filterParsedLogs(logs, { source: 'github' }).map((l) => l.message)).toEqual(['call failed']);
    expect(filterParsedLogs(logs, { source: GATEWAY_LOG_SOURCE }).map((l) => l.message)).toEqual(['gateway up']);
    expect(filterParsedLogs(logs, { source: null })).toHaveLength(3);
  });

  it('filters by level set', () => {
    expect(filterParsedLogs(logs, { levels: new Set(['ERROR']) }).map((l) => l.message)).toEqual([
      'call failed',
    ]);
  });

  it('filters by trace id', () => {
    expect(filterParsedLogs(logs, { traceId: 'abc123' }).map((l) => l.message)).toEqual(['call failed']);
    expect(filterParsedLogs(logs, { traceId: 'missing' })).toHaveLength(0);
  });

  it('matches search queries against message, component, source, and trace id', () => {
    expect(filterParsedLogs(logs, { query: 'failed' })).toHaveLength(1);
    expect(filterParsedLogs(logs, { query: 'zapier' }).map((l) => l.message)).toEqual(['poll tick']);
    expect(filterParsedLogs(logs, { query: 'abc123' })).toHaveLength(1);
    expect(filterParsedLogs(logs, { query: 'nope' })).toHaveLength(0);
  });

  it('composes filters', () => {
    expect(
      filterParsedLogs(logs, { source: 'zapier', levels: new Set(['DEBUG']), query: 'poll' }),
    ).toHaveLength(1);
    expect(
      filterParsedLogs(logs, { source: 'zapier', levels: new Set(['ERROR']) }),
    ).toHaveLength(0);
  });

  it('matches search queries against stringified attr values', () => {
    const attred = [
      entry({ message: 'tool done', attrs: { server: 'github', tool: 'create_issue', replica_id: 3, is_error: false } }),
      entry({ message: 'other line' }),
    ];
    expect(filterParsedLogs(attred, { query: 'create_issue' }).map((l) => l.message)).toEqual(['tool done']);
    // Non-string values stringify defensively.
    expect(filterParsedLogs(attred, { query: '3' }).map((l) => l.message)).toEqual(['tool done']);
    expect(filterParsedLogs(attred, { query: 'false' }).map((l) => l.message)).toEqual(['tool done']);
    // Nested (dotted-flattened miss) values do not crash the predicate.
    const nested = entry({ message: 'nested', attrs: { group: { inner: 'deep-value' } } });
    expect(filterParsedLogs([nested], { query: 'deep-value' })).toHaveLength(1);
  });

  it('drops entries older than the since cutoff but keeps unparseable timestamps', () => {
    const old = entry({ message: 'old', timestamp: '2026-07-23T09:00:00Z' });
    const fresh = entry({ message: 'fresh', timestamp: '2026-07-23T10:00:00Z' });
    const weird = entry({ message: 'weird', timestamp: 'not-a-timestamp' });
    const cutoff = Date.parse('2026-07-23T09:30:00Z');
    expect(filterParsedLogs([old, fresh, weird], { since: cutoff }).map((l) => l.message)).toEqual([
      'fresh',
      'weird',
    ]);
  });
});

describe('export serialization', () => {
  const logs = [
    entry({
      message: 'tool call finished',
      timestamp: '2026-07-23T10:00:01Z',
      traceId: 'abc123',
      attrs: { server: 'github', tool: 'create_issue' },
      raw: JSON.stringify({ msg: 'tool call finished' }, null, 2),
    }),
    entry({ message: 'plain line', timestamp: '2026-07-23T10:00:02Z' }),
  ];

  it('emits one parseable JSON object per JSONL line', () => {
    const lines = serializeLogsJSONL(logs).split('\n');
    expect(lines).toHaveLength(2);
    for (const line of lines) {
      expect(() => JSON.parse(line)).not.toThrow();
    }
    const first = JSON.parse(lines[0]);
    expect(first.msg).toBe('tool call finished');
    expect(first.trace_id).toBe('abc123');
    expect(first.attrs.tool).toBe('create_issue');
    const second = JSON.parse(lines[1]);
    expect(second.trace_id).toBeUndefined();
    expect(second.attrs).toBeUndefined();
  });

  it('emits readable single-line text entries', () => {
    const lines = serializeLogsText(logs).split('\n');
    expect(lines).toHaveLength(2);
    expect(lines[0]).toContain('tool call finished');
    expect(lines[0]).toContain('github');
    expect(lines[1]).toContain('plain line');
  });
});

describe('URL param normalizers', () => {
  it('accepts only known window sizes', () => {
    expect(normalizeLogWindowParam('200', 500)).toBe(200);
    expect(normalizeLogWindowParam('1000', 500)).toBe(1000);
    expect(normalizeLogWindowParam('999', 500)).toBe(500);
    expect(normalizeLogWindowParam(null, 500)).toBe(500);
  });

  it('accepts only known time ranges', () => {
    expect(normalizeLogTimeRangeParam('5m')).toBe('5m');
    expect(normalizeLogTimeRangeParam('2h')).toBe('all');
    expect(normalizeLogTimeRangeParam(null)).toBe('all');
  });
});

describe('parseLogEntry slog text format', () => {
  it('carries trace_id from slog text attrs onto traceId', () => {
    const parsed = parseLogEntry(
      'time=2026-07-24T10:00:00Z level=INFO msg="tool done" trace_id=abc123 server=github',
    );
    expect(parsed.traceId).toBe('abc123');
    // Trace filtering and the pivot read log.traceId, so the line must
    // participate in correlation like object/JSON entries do.
    expect(filterParsedLogs([parsed], { traceId: 'abc123' })).toHaveLength(1);
  });
});

describe('formatTimestamp', () => {
  it('formats valid ISO timestamps as HH:MM:SS.mmm', () => {
    expect(formatTimestamp('2026-07-24T10:00:00.123Z')).toMatch(/^\d{2}:\d{2}:\d{2}\.\d{3}$/);
  });

  it('never renders Invalid Date or NaN for unparseable input', () => {
    for (const bad of ['not-a-timestamp', 'garbage', '2026-99-99Tzz']) {
      const out = formatTimestamp(bad);
      expect(out).not.toContain('Invalid');
      expect(out).not.toContain('NaN');
    }
    expect(formatTimestamp('')).toBe('');
  });
});

describe('logEntryKeys', () => {
  it('derives stable keys from entry identity, not array position', () => {
    const a = entry({ message: 'first' });
    const b = entry({ message: 'second' });
    const before = logEntryKeys([a, b]);
    // A poll replaces the array and prepends a new entry; existing entries
    // must keep their keys.
    const after = logEntryKeys([entry({ message: 'newest' }), a, b]);
    expect(after[1]).toBe(before[0]);
    expect(after[2]).toBe(before[1]);
  });

  it('disambiguates duplicate lines', () => {
    const dup = entry({ message: 'same line' });
    const keys = logEntryKeys([dup, dup, dup]);
    expect(new Set(keys).size).toBe(3);
  });
});
