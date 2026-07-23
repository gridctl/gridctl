import { describe, it, expect } from 'vitest';
import {
  GATEWAY_LOG_SOURCE,
  filterParsedLogs,
  logSourceOf,
  parseLogEntry,
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
});
