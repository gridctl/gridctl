import { describe, it, expect } from 'vitest';
import {
  RUN_TIME_RANGES,
  RUN_URL_KEYS,
  activeHiddenFilters,
  emptyFilters,
  hasActiveRunFilters,
  localInputToRFC3339,
  parseRunsSearch,
  resolveRunsQuery,
  rfc3339ToLocalInput,
  writeRunsSearch,
} from '../lib/runsFilters';

const NOW = Date.parse('2026-11-27T12:00:00Z');

describe('runsFilters', () => {
  it('lists the time ranges in order', () => {
    expect(RUN_TIME_RANGES.map((r) => [r.value, r.label])).toEqual([
      ['all', 'Any time'],
      ['15m', 'Last 15 minutes'],
      ['1h', 'Last hour'],
      ['24h', 'Last 24 hours'],
      ['7d', 'Last 7 days'],
      ['custom', 'Custom range'],
    ]);
  });

  it('resolves presets against now and omits empty values', () => {
    expect(resolveRunsQuery(emptyFilters(), NOW)).toEqual({});
    expect(resolveRunsQuery({ ...emptyFilters(), range: '1h', tool: 'x' }, NOW)).toEqual({
      tool: 'x',
      since: '2026-11-27T11:00:00.000Z',
    });
    expect(resolveRunsQuery({ ...emptyFilters(), range: '7d' }, NOW).since).toBe('2026-11-20T12:00:00.000Z');
    expect(resolveRunsQuery({ ...emptyFilters(), range: '15m' }, NOW).until).toBeUndefined();
  });

  it('sends custom bounds only under Custom range', () => {
    const f = { ...emptyFilters(), since: '2026-01-01T00:00:00Z', until: '2026-01-02T00:00:00Z' };
    expect(resolveRunsQuery(f, NOW)).toEqual({});
    expect(resolveRunsQuery({ ...f, range: 'custom' }, NOW)).toEqual({
      since: '2026-01-01T00:00:00Z',
      until: '2026-01-02T00:00:00Z',
    });
    expect(resolveRunsQuery({ ...f, range: '24h' }, NOW)).toEqual({ since: '2026-11-26T12:00:00.000Z' });
  });

  it('round-trips datetime-local values through RFC3339', () => {
    for (const local of ['2026-03-08T02:30', '2026-11-27T09:05', '2025-12-31T23:59']) {
      const rfc = localInputToRFC3339(local);
      expect(rfc).toMatch(/Z$/);
      expect(new Date(rfc).getTime()).toBe(new Date(local).getTime());
      // DST gaps shift a nonexistent local time; compare instants there.
      expect(new Date(localInputToRFC3339(rfc3339ToLocalInput(rfc))).getTime()).toBe(new Date(rfc).getTime());
    }
    expect(rfc3339ToLocalInput(localInputToRFC3339('2026-11-27T09:05'))).toBe('2026-11-27T09:05');
    expect(localInputToRFC3339('')).toBe('');
    expect(rfc3339ToLocalInput('garbage')).toBe('');
    expect(rfc3339ToLocalInput('')).toBe('');
  });

  it('returns null filters when the URL has no filter key', () => {
    expect(parseRunsSearch(new URLSearchParams('view=runs'))).toEqual({ filters: null, run: null });
    expect(parseRunsSearch(new URLSearchParams('view=runs&run=a1'))).toEqual({ filters: null, run: 'a1' });
  });

  it('parses filter keys over defaults', () => {
    const { filters, run } = parseRunsSearch(
      new URLSearchParams('view=runs&server=github&disposition=denied&range=24h&trace=abc&run=a1'),
    );
    expect(run).toBe('a1');
    expect(filters).toEqual({ ...emptyFilters(), server: 'github', disposition: 'denied', range: '24h', trace: 'abc' });
  });

  it('drops invalid range, disposition, since, and until', () => {
    const { filters } = parseRunsSearch(
      new URLSearchParams('range=nope&disposition=nope&since=garbage&until=2026-13-45'),
    );
    expect(filters).toEqual(emptyFilters());
    expect(parseRunsSearch(new URLSearchParams('range=nope')).filters).toEqual(emptyFilters());
    expect(parseRunsSearch(new URLSearchParams('since=garbage')).filters).toEqual({ ...emptyFilters(), range: 'custom' });
    expect(resolveRunsQuery(parseRunsSearch(new URLSearchParams('range=nope&disposition=nope&since=garbage')).filters!, NOW)).toEqual({});
    // Date parses these; the server's RFC3339 parser rejects them.
    for (const bad of ['2026-01-01t00:00:00z', '2026-01-01T24:00:00Z', '2026-02-31T00:00:00Z', '2026-01-01T00:00:60Z']) {
      expect(parseRunsSearch(new URLSearchParams({ range: 'custom', since: bad, until: bad })).filters).toEqual({
        ...emptyFilters(),
        range: 'custom',
      });
    }
    expect(parseRunsSearch(new URLSearchParams('since=2024-02-29T23:59:59.5%2B05:30')).filters!.since).toBe(
      '2024-02-29T23:59:59.5+05:30',
    );
  });

  it('treats since without range as a custom range', () => {
    expect(parseRunsSearch(new URLSearchParams('since=2026-01-01T00:00:00Z')).filters).toEqual({
      ...emptyFilters(),
      range: 'custom',
      since: '2026-01-01T00:00:00Z',
    });
    // since under a relative preset is ignored.
    expect(parseRunsSearch(new URLSearchParams('range=1h&since=2026-01-01T00:00:00Z')).filters?.since).toBe('');
  });

  it('writes only non-default keys and keeps unrelated keys', () => {
    const prev = new URLSearchParams('view=runs&other=1&tool=stale&run=old');
    expect(writeRunsSearch(prev, emptyFilters(), null).toString()).toBe('view=runs&other=1');
    const out = writeRunsSearch(
      prev,
      { ...emptyFilters(), server: 'github', range: 'custom', since: '2026-01-01T00:00:00Z', attempt: 'x' },
      'a1',
    );
    expect(Object.fromEntries(out)).toEqual({
      view: 'runs',
      other: '1',
      server: 'github',
      attempt: 'x',
      range: 'custom',
      since: '2026-01-01T00:00:00Z',
      run: 'a1',
    });
    expect(writeRunsSearch(prev, { ...emptyFilters(), range: '1h', since: '2026-01-01T00:00:00Z' }, null).get('since')).toBeNull();
  });

  it('round-trips filters through the URL', () => {
    const f = { ...emptyFilters(), tool: 't', client: 'c', requested: 'r', access: 'a', parent: 'p', root: 'o', previous: 'v', range: '7d' as const };
    expect(parseRunsSearch(writeRunsSearch(new URLSearchParams(), f, null)).filters).toEqual(f);
    expect(RUN_URL_KEYS).toContain('run');
  });

  it('reports active filters and hidden chips', () => {
    expect(hasActiveRunFilters(emptyFilters())).toBe(false);
    expect(hasActiveRunFilters({ ...emptyFilters(), range: '1h' })).toBe(true);
    expect(hasActiveRunFilters({ ...emptyFilters(), since: '2026-01-01T00:00:00Z' })).toBe(false);
    expect(hasActiveRunFilters({ ...emptyFilters(), tool: 'x' })).toBe(true);
    expect(activeHiddenFilters({ ...emptyFilters(), tool: 'x', trace: 'abc', attempt: 'a' })).toEqual([
      { key: 'attempt', label: 'Attempt ID', value: 'a' },
      { key: 'trace', label: 'Trace ID', value: 'abc' },
    ]);
  });
});
