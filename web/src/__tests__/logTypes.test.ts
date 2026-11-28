import { describe, it, expect } from 'vitest';
import {
  GATEWAY_LOG_SOURCE,
  countNewLogs,
  filterParsedLogs,
  formatGoDuration,
  formatLogCount,
  formatLogCountTitle,
  formatTimestamp,
  formatTimestampTitle,
  hiddenQueryMatch,
  inlineLogFields,
  logsSpanMultipleDays,
  logEntryKeys,
  reuseUnchangedLogs,
  logSourceOf,
  normalizeLogTimeRangeParam,
  normalizeLogWindowParam,
  parseGoDurationMs,
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

  it('renders local midnight with a 00 hour', () => {
    const ts = new Date(2026, 0, 15, 0, 5, 9, 123).toISOString();
    expect(formatTimestamp(ts)).toBe('00:05:09.123');
  });

  it('prefixes MM-DD when showDate is set', () => {
    const ts = new Date(2026, 0, 15, 0, 5, 9, 123).toISOString();
    expect(formatTimestamp(ts, true)).toBe('01-15 00:05:09.123');
  });

  it('keeps invalid and empty handling with showDate', () => {
    expect(formatTimestamp('', true)).toBe('');
    expect(formatTimestamp('garbage', true)).not.toContain('NaN');
  });
});

describe('formatTimestampTitle', () => {
  it('shows local date, time, zone, and the original string', () => {
    const ts = new Date(2026, 10, 22, 0, 5, 9, 123).toISOString();
    const out = formatTimestampTitle(ts);
    expect(out).toMatch(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d{3} \S.* · /);
    expect(out.startsWith('2026-11-22 00:05:09.123 ')).toBe(true);
    expect(out.endsWith(ts)).toBe(true);
  });

  it('returns unparseable input unchanged', () => {
    expect(formatTimestampTitle('not-a-timestamp')).toBe('not-a-timestamp');
    expect(formatTimestampTitle('')).toBe('');
  });
});

describe('logsSpanMultipleDays', () => {
  const at = (d: Date) => entry({ timestamp: d.toISOString() });

  it('is false for empty input', () => {
    expect(logsSpanMultipleDays([])).toBe(false);
  });

  it('is false for entries a minute apart on one day', () => {
    expect(
      logsSpanMultipleDays([at(new Date(2026, 0, 15, 12, 0)), at(new Date(2026, 0, 15, 12, 1))]),
    ).toBe(false);
  });

  it('is true for entries 48 hours apart', () => {
    expect(
      logsSpanMultipleDays([at(new Date(2026, 0, 15, 12, 0)), at(new Date(2026, 0, 17, 12, 0))]),
    ).toBe(true);
  });

  it('ignores unparseable timestamps', () => {
    expect(
      logsSpanMultipleDays([
        at(new Date(2026, 0, 15, 12, 0)),
        entry({ timestamp: 'garbage' }),
        entry({ timestamp: '' }),
      ]),
    ).toBe(false);
    expect(logsSpanMultipleDays([entry({ timestamp: 'garbage' })])).toBe(false);
  });
});

describe('formatLogCount', () => {
  it('reads Showing N of M buffered when nothing is filtered out', () => {
    expect(formatLogCount({ filtered: 500, loaded: 500, bufferTotal: 1000, bufferCapacity: 1000 }))
      .toBe('Showing 500 of 1,000 buffered');
  });

  it('reads N of M match when a filter hides entries', () => {
    expect(formatLogCount({ filtered: 12, loaded: 500, bufferTotal: 1000, bufferCapacity: 1000 }))
      .toBe('12 of 500 match · 1,000 buffered');
  });

  it('falls back to entry counts when the buffer is unknown', () => {
    expect(formatLogCount({ filtered: 1500, loaded: 1500, bufferTotal: 0, bufferCapacity: 0 }))
      .toBe('1,500 entries');
    expect(formatLogCount({ filtered: 12, loaded: 500, bufferTotal: 0, bufferCapacity: 0 }))
      .toBe('12 of 500 entries');
    expect(formatLogCount({ filtered: 1, loaded: 1, bufferTotal: 0, bufferCapacity: 0 }))
      .toBe('1 entry');
  });
});

describe('formatLogCountTitle', () => {
  it('names the window size and buffer capacity', () => {
    const out = formatLogCountTitle({ windowSize: 500, bufferTotal: 900, bufferCapacity: 1000 });
    expect(out).toContain('most recent 500 entries');
    expect(out).toContain('newest 1,000 lines');
    expect(out).toContain('holds 900');
  });

  it('is empty when neither is known', () => {
    expect(formatLogCountTitle({ bufferTotal: 0, bufferCapacity: 0 })).toBe('');
  });
});

describe('hiddenQueryMatch', () => {
  const log = entry({
    message: 'tool call ok',
    component: 'gateway',
    traceId: 'abc123def',
    attrs: { server: 'github', client: 'claude-code' },
  });

  it('is null for an empty query', () => {
    expect(hiddenQueryMatch(log, '', [log.message])).toBeNull();
  });

  it('is null when the message contains the query', () => {
    expect(hiddenQueryMatch(log, 'TOOL', [log.message, log.component])).toBeNull();
  });

  it('names the attribute key for an attribute-only match', () => {
    expect(hiddenQueryMatch(log, 'claude', [log.message, log.component])).toEqual({
      label: 'attributes',
      title: 'client=claude-code',
    });
  });

  it('reports a trace-only match', () => {
    const out = hiddenQueryMatch(log, 'abc123', [log.message, log.component]);
    expect(out?.label).toBe('trace ID');
    expect(out?.title).toContain('abc123def');
  });

  it('is null when the caller passes a visible text containing the query', () => {
    expect(hiddenQueryMatch(log, 'claude', [log.message, 'claude-code'])).toBeNull();
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

describe('reuseUnchangedLogs', () => {
  it('returns the previous object when identity and raw are unchanged', () => {
    const prior = entry({ message: 'steady', raw: 'steady-raw' });
    const parsed = entry({ message: 'steady', raw: 'steady-raw', component: 'other' });
    const [reused] = reuseUnchangedLogs([prior], [parsed]);
    expect(reused).toBe(prior);
  });

  it('returns the new object when raw changes under the same identity', () => {
    const prior = entry({ message: 'steady', raw: 'old', level: 'INFO' });
    const parsed = entry({ message: 'steady', raw: 'new', level: 'ERROR' });
    const [reused] = reuseUnchangedLogs([prior], [parsed]);
    expect(reused).toBe(parsed);
    expect(reused).not.toBe(prior);
  });

  it('reuses by duplicate ordinal, not by the first matching line', () => {
    const first = entry({ message: 'dup', raw: 'raw-a', timestamp: '2026-07-24T10:00:00Z' });
    const second = entry({ message: 'dup', raw: 'raw-b', timestamp: '2026-07-24T10:00:00Z' });
    const nextFirst = entry({ message: 'dup', raw: 'raw-a', timestamp: '2026-07-24T10:00:00Z' });
    const nextSecond = entry({ message: 'dup', raw: 'raw-b-changed', timestamp: '2026-07-24T10:00:00Z' });
    const reused = reuseUnchangedLogs([first, second], [nextFirst, nextSecond]);
    expect(reused[0]).toBe(first);
    expect(reused[1]).toBe(nextSecond);
    expect(reused[1]).not.toBe(second);
  });

  it('does not reuse an entry whose identity moved to a different ordinal', () => {
    const kept = entry({ message: 'dup', raw: 'raw-a' });
    const extra = entry({ message: 'dup', raw: 'raw-b' });
    const only = entry({ message: 'dup', raw: 'raw-b' });
    const [reused] = reuseUnchangedLogs([kept, extra], [only]);
    // Ordinal 0 previously held raw-a, so raw-b at ordinal 0 is a new entry.
    expect(reused).toBe(only);
    expect(reused).not.toBe(extra);
  });
});

describe('parseGoDurationMs', () => {
  it('parses Go duration strings into milliseconds', () => {
    expect(parseGoDurationMs('1.234567ms')).toBeCloseTo(1.234567);
    expect(parseGoDurationMs('850µs')).toBeCloseTo(0.85);
    expect(parseGoDurationMs('850us')).toBeCloseTo(0.85);
    expect(parseGoDurationMs('2.5s')).toBe(2500);
    expect(parseGoDurationMs('1m2.5s')).toBe(62500);
    expect(parseGoDurationMs('1h0m0s')).toBe(3_600_000);
    expect(parseGoDurationMs('42ns')).toBeCloseTo(0.000042);
    expect(parseGoDurationMs('0s')).toBe(0);
    expect(parseGoDurationMs('0')).toBe(0);
  });

  it('returns null for values that are not Go durations', () => {
    expect(parseGoDurationMs('soon')).toBeNull();
    expect(parseGoDurationMs('')).toBeNull();
    expect(parseGoDurationMs('12')).toBeNull();
    expect(parseGoDurationMs('1.5 s')).toBeNull();
    expect(parseGoDurationMs('3d')).toBeNull();
  });
});

describe('formatGoDuration', () => {
  it('formats parsed durations compactly', () => {
    expect(formatGoDuration('1.234567ms')).toBe('1.2ms');
    expect(formatGoDuration('850µs')).toBe('850µs');
    expect(formatGoDuration('2.5s')).toBe('2.50s');
    expect(formatGoDuration('1m2.5s')).toBe('62.50s');
  });

  it('shows unparseable and numeric values as they arrived', () => {
    expect(formatGoDuration('about a second')).toBe('about a second');
    expect(formatGoDuration(1500)).toBe('1500');
  });

  it('shows negative durations as they arrived', () => {
    expect(formatGoDuration('-1.5s')).toBe('-1.5s');
  });

  it('shows sub-microsecond durations as they arrived', () => {
    expect(formatGoDuration('42ns')).toBe('42ns');
    expect(formatGoDuration('0s')).toBe('0µs');
  });
});

describe('inlineLogFields', () => {
  const summary = (log: ParsedLog) =>
    inlineLogFields(log).map((f) => [f.key, f.label, f.value, f.tone]);

  it('yields tool, duration, ok, and client for a finished call', () => {
    const log = entry({
      message: 'tool call finished',
      attrs: {
        server: 'github',
        replica_id: 0,
        client: 'claude-code',
        tool: 'create_issue',
        duration: '1.234567ms',
        is_error: false,
      },
    });
    expect(summary(log)).toEqual([
      ['tool', 'tool', 'create_issue', 'default'],
      ['duration', null, '1.2ms', 'default'],
      ['is_error', null, 'ok', 'ok'],
      ['client', 'client', 'claude-code', 'default'],
    ]);
  });

  it('yields tool, duration, error, and client for a failed call', () => {
    const log = entry({
      message: 'tool call failed',
      attrs: {
        server: 'github',
        replica_id: 1,
        client: 'claude-code',
        tool: 'search_code',
        duration: '30s',
        error: 'context deadline exceeded',
      },
    });
    expect(summary(log)).toEqual([
      ['tool', 'tool', 'search_code', 'default'],
      ['duration', null, '30.00s', 'default'],
      ['error', 'error', 'context deadline exceeded', 'error'],
      ['client', 'client', 'claude-code', 'default'],
    ]);
  });

  it('follows key order and caps at four, keeping the outcome over client', () => {
    const log = entry({
      attrs: {
        client: 'claude-code',
        is_error: true,
        duration: '2.5s',
        name: 'n1',
        tool: 't1',
        status: 'done',
      },
    });
    expect(inlineLogFields(log).map((f) => f.key)).toEqual(['tool', 'name', 'duration', 'is_error']);
  });

  it('accepts boolean and string is_error and ignores other values', () => {
    const tone = (v: unknown) => inlineLogFields(entry({ attrs: { is_error: v } }));
    expect(tone(false)).toMatchObject([{ value: 'ok', tone: 'ok' }]);
    expect(tone('false')).toMatchObject([{ value: 'ok', tone: 'ok' }]);
    expect(tone(true)).toMatchObject([{ value: 'error', tone: 'error' }]);
    expect(tone('true')).toMatchObject([{ value: 'error', tone: 'error' }]);
    expect(tone('maybe')).toEqual([]);
    expect(tone(0)).toEqual([]);
  });

  it('skips empty values, server, replica_id, and unlisted keys', () => {
    const log = entry({
      attrs: { server: 'github', replica_id: 2, tool: '', name: null, client: undefined, other: 'x' },
    });
    expect(inlineLogFields(log)).toEqual([]);
    expect(inlineLogFields(entry({}))).toEqual([]);
  });

  it('collapses newlines in the value and keeps the raw value in the title', () => {
    const [field] = inlineLogFields(
      entry({ message: 'server stderr', attrs: { server: 'py', output: 'Traceback:\n  line 1' } }),
    );
    expect(field).toEqual({
      key: 'output',
      label: null,
      value: 'Traceback: line 1',
      tone: 'default',
      title: 'output=Traceback:\n  line 1',
    });
  });

  it('shows an unparseable duration as raw text', () => {
    expect(inlineLogFields(entry({ attrs: { duration: 'n/a' } }))).toMatchObject([{ value: 'n/a' }]);
  });
});

describe('countNewLogs', () => {
  const a = entry({ message: 'a', timestamp: '2026-07-23T10:00:01Z' });
  const b = entry({ message: 'b', timestamp: '2026-07-23T10:00:02Z' });
  const c = entry({ message: 'c', timestamp: '2026-07-23T10:00:03Z' });
  const d = entry({ message: 'd', timestamp: '2026-07-23T10:00:04Z' });
  const anchorOf = (log: ParsedLog) => ({ key: logEntryKeys([log])[0], ts: Date.parse(log.timestamp) });

  it('counts entries after the anchor when it is still present', () => {
    const logs = [a, b, c, d];
    expect(countNewLogs(logEntryKeys(logs), logs, anchorOf(b))).toEqual({ count: 2, overflow: false });
    expect(countNewLogs(logEntryKeys(logs), logs, anchorOf(d))).toEqual({ count: 0, overflow: false });
  });

  it('falls back to newer timestamps and marks overflow when the anchor left the window', () => {
    const logs = [c, d];
    expect(countNewLogs(logEntryKeys(logs), logs, anchorOf(a))).toEqual({ count: 2, overflow: true });
  });

  it('reports no overflow when no entry is newer than a missing anchor', () => {
    const logs = [a];
    expect(countNewLogs(logEntryKeys(logs), logs, anchorOf(b))).toEqual({ count: 0, overflow: false });
  });

  it('returns zero for a missing anchor whose timestamp does not parse', () => {
    const logs = [c, d];
    expect(countNewLogs(logEntryKeys(logs), logs, { key: 'gone', ts: Number.NaN })).toEqual({
      count: 0,
      overflow: false,
    });
  });

  it('returns zero without an anchor', () => {
    const logs = [a, b];
    expect(countNewLogs(logEntryKeys(logs), logs, null)).toEqual({ count: 0, overflow: false });
  });

  it('returns zero for empty input', () => {
    expect(countNewLogs([], [], anchorOf(a))).toEqual({ count: 0, overflow: false });
  });
});
