export type RunsTimeRange = 'all' | '15m' | '1h' | '24h' | '7d' | 'custom';

export interface RunsFilters {
  server: string;
  tool: string;
  disposition: string;
  requested: string;
  client: string;
  access: string;
  attempt: string;
  parent: string;
  root: string;
  previous: string;
  trace: string;
  range: RunsTimeRange;
  since: string;
  until: string;
}

/** Resolved fetchRuns parameters for a fresh query, without a cursor. */
export interface RunsQuery {
  server?: string;
  tool?: string;
  disposition?: string;
  requested?: string;
  client?: string;
  access?: string;
  attempt?: string;
  parent?: string;
  root?: string;
  previous?: string;
  trace?: string;
  since?: string;
  until?: string;
}

export const emptyFilters = (): RunsFilters => ({
  server: '',
  tool: '',
  disposition: '',
  requested: '',
  client: '',
  access: '',
  attempt: '',
  parent: '',
  root: '',
  previous: '',
  trace: '',
  range: 'all',
  since: '',
  until: '',
});

export const RUN_TIME_RANGES: ReadonlyArray<{ value: RunsTimeRange; label: string; ms: number | null }> = [
  { value: 'all', label: 'Any time', ms: null },
  { value: '15m', label: 'Last 15 minutes', ms: 15 * 60 * 1000 },
  { value: '1h', label: 'Last hour', ms: 60 * 60 * 1000 },
  { value: '24h', label: 'Last 24 hours', ms: 24 * 60 * 60 * 1000 },
  { value: '7d', label: 'Last 7 days', ms: 7 * 24 * 60 * 60 * 1000 },
  { value: 'custom', label: 'Custom range', ms: null },
];

export const RUN_DISPOSITIONS = [
  'completed',
  'denied',
  'tool_error',
  'routing_failed',
  'transport_error',
  'cancelled',
  'timeout',
  'input_required',
  'retry_rejected',
] as const;

export type RunTextFilterKey = 'tool' | 'client' | 'requested' | 'access' | 'attempt' | 'parent' | 'root' | 'previous' | 'trace';

/** Filters kept behind More filters, in display order. */
export const RUN_HIDDEN_FILTERS: ReadonlyArray<{ key: RunTextFilterKey; label: string; placeholder: string }> = [
  { key: 'requested', label: 'Requested name', placeholder: 'exact name' },
  { key: 'access', label: 'Access label', placeholder: 'exact name' },
  { key: 'attempt', label: 'Attempt ID', placeholder: 'exact ID' },
  { key: 'parent', label: 'Parent ID', placeholder: 'exact ID' },
  { key: 'root', label: 'Root ID', placeholder: 'exact ID' },
  { key: 'previous', label: 'Previous ID', placeholder: 'exact ID' },
  { key: 'trace', label: 'Trace ID', placeholder: 'exact ID' },
];

// Filter keys mirrored in the URL. `range` and `run` complete the Runs set.
export const RUN_FILTER_URL_KEYS = [
  'server',
  'tool',
  'disposition',
  'client',
  'requested',
  'access',
  'attempt',
  'parent',
  'root',
  'previous',
  'trace',
  'range',
  'since',
  'until',
] as const;

export const RUN_URL_KEYS = [...RUN_FILTER_URL_KEYS, 'run'] as const;

const STRING_KEYS = [
  'server',
  'tool',
  'client',
  'requested',
  'access',
  'attempt',
  'parent',
  'root',
  'previous',
  'trace',
] as const;

function isTimeRange(value: string): value is RunsTimeRange {
  return RUN_TIME_RANGES.some((r) => r.value === value);
}

function isDisposition(value: string): boolean {
  return (RUN_DISPOSITIONS as readonly string[]).includes(value);
}

const RFC3339 = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d+)?(?:Z|[+-](\d{2}):(\d{2}))$/;

// Date accepts values the server's time.RFC3339 parser rejects (lowercase t
// and z, hour 24, February 31), so each field is range-checked first.
function validInstant(value: string): boolean {
  const m = RFC3339.exec(value);
  if (!m) return false;
  const [y, mo, d, h, mi, s] = m.slice(1, 7).map(Number);
  if (h > 23 || mi > 59 || s > 59) return false;
  if (m[7] != null && (Number(m[7]) > 23 || Number(m[8]) > 59)) return false;
  const day = new Date(0);
  day.setUTCFullYear(y, mo - 1, d);
  if (day.getUTCFullYear() !== y || day.getUTCMonth() !== mo - 1 || day.getUTCDate() !== d) return false;
  return !Number.isNaN(new Date(value).getTime());
}

/** Converts a datetime-local value (local time) to RFC3339 in UTC; '' when empty or invalid. */
export function localInputToRFC3339(value: string): string {
  if (!value) return '';
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return '';
  return d.toISOString();
}

/** Converts an RFC3339 instant to a datetime-local value (YYYY-MM-DDTHH:mm, local time); '' when invalid. */
export function rfc3339ToLocalInput(value: string): string {
  if (!validInstant(value)) return '';
  const d = new Date(value);
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export function sameRunsFilters(a: RunsFilters, b: RunsFilters): boolean {
  return (Object.keys(a) as (keyof RunsFilters)[]).every((k) => a[k] === b[k]);
}

/** fetchRuns parameters for a fresh query. A relative preset resolves against `now`. */
export function resolveRunsQuery(filters: RunsFilters, now: number): RunsQuery {
  const q: RunsQuery = {};
  for (const key of STRING_KEYS) {
    if (filters[key]) q[key] = filters[key];
  }
  if (filters.disposition) q.disposition = filters.disposition;
  if (filters.range === 'custom') {
    if (filters.since) q.since = filters.since;
    if (filters.until) q.until = filters.until;
  } else {
    const ms = RUN_TIME_RANGES.find((r) => r.value === filters.range)?.ms;
    if (ms != null) q.since = new Date(now - ms).toISOString();
  }
  return q;
}

/** Reads the Runs keys. `filters` is null when the URL carries no filter key. */
export function parseRunsSearch(params: URLSearchParams): { filters: RunsFilters | null; run: string | null } {
  const runRaw = params.get('run');
  const run = runRaw ? runRaw : null;
  if (!RUN_FILTER_URL_KEYS.some((k) => params.has(k))) return { filters: null, run };
  const filters = emptyFilters();
  for (const key of STRING_KEYS) {
    const v = params.get(key)?.trim();
    if (v) filters[key] = v;
  }
  const disposition = params.get('disposition') ?? '';
  if (isDisposition(disposition)) filters.disposition = disposition;
  const since = params.get('since') ?? '';
  const until = params.get('until') ?? '';
  const range = params.get('range');
  if (range != null && isTimeRange(range)) filters.range = range;
  else if (range == null && (params.has('since') || params.has('until'))) filters.range = 'custom';
  if (filters.range === 'custom') {
    if (validInstant(since)) filters.since = since;
    if (validInstant(until)) filters.until = until;
  }
  return { filters, run };
}

/** Rewrites the Runs keys on a copy of `prev`, keeping every other key. */
export function writeRunsSearch(prev: URLSearchParams, filters: RunsFilters, selectedId: string | null): URLSearchParams {
  const next = new URLSearchParams(prev);
  for (const key of RUN_URL_KEYS) next.delete(key);
  if (filters.server) next.set('server', filters.server);
  if (filters.tool) next.set('tool', filters.tool);
  if (filters.disposition) next.set('disposition', filters.disposition);
  if (filters.client) next.set('client', filters.client);
  for (const { key } of RUN_HIDDEN_FILTERS) {
    if (filters[key]) next.set(key, filters[key]);
  }
  if (filters.range !== 'all') next.set('range', filters.range);
  if (filters.range === 'custom') {
    if (filters.since) next.set('since', filters.since);
    if (filters.until) next.set('until', filters.until);
  }
  if (selectedId) next.set('run', selectedId);
  return next;
}

// Custom bounds kept while another preset is selected are inert, so they do
// not count as active.
export function hasActiveRunFilters(filters: RunsFilters): boolean {
  if (filters.range !== 'all') return true;
  return (Object.keys(filters) as (keyof RunsFilters)[]).some(
    (k) => k !== 'range' && k !== 'since' && k !== 'until' && filters[k] !== '',
  );
}

export function activeHiddenFilters(filters: RunsFilters): Array<{ key: RunTextFilterKey; label: string; value: string }> {
  return RUN_HIDDEN_FILTERS.filter(({ key }) => filters[key] !== '').map(({ key, label }) => ({
    key,
    label,
    value: filters[key],
  }));
}
