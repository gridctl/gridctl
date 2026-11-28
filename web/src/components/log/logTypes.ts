import type { LogEntry } from '../../lib/api';
import { formatDuration } from '../../lib/duration';

// Log level configuration
export type LogLevel = 'DEBUG' | 'INFO' | 'WARN' | 'ERROR';

export const LOG_LEVELS: LogLevel[] = ['ERROR', 'WARN', 'INFO', 'DEBUG'];

export const LEVEL_STYLES: Record<LogLevel, { text: string; bg: string; border: string; dot: string }> = {
  ERROR: {
    text: 'text-status-error',
    bg: 'bg-status-error/10',
    border: 'border-status-error/30',
    dot: 'bg-status-error',
  },
  WARN: {
    text: 'text-status-pending',
    bg: 'bg-status-pending/10',
    border: 'border-status-pending/30',
    dot: 'bg-status-pending',
  },
  INFO: {
    text: 'text-primary',
    bg: 'bg-primary/10',
    border: 'border-primary/30',
    dot: 'bg-primary',
  },
  DEBUG: {
    text: 'text-text-muted',
    bg: 'bg-surface-highlight',
    border: 'border-border/30',
    dot: 'bg-text-muted',
  },
};

export interface ParsedLog {
  level: LogLevel;
  timestamp: string;
  message: string;
  component?: string;
  traceId?: string;
  attrs?: Record<string, unknown>;
  raw: string;
}

const VALID_LEVELS = new Set<string>(['DEBUG', 'INFO', 'WARN', 'ERROR']);

function toLogLevel(raw: string | undefined): LogLevel {
  const upper = raw?.toUpperCase() ?? '';
  return VALID_LEVELS.has(upper) ? (upper as LogLevel) : 'INFO';
}

// Matches Docker timestamp prefix: 2026-02-03T15:22:01.637603230Z
const DOCKER_TS_RE = /^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d+Z)\s+/;

// Matches slog text format: time=2026-... level=INFO msg="..." [key=value ...]
const SLOG_TEXT_RE = /^time=(\S+)\s+level=(\S+)\s+msg=("(?:[^"\\]|\\.)*"|\S+)(.*)/;

// Parses slog text key=value pairs from the remainder string.
// Handles quoted values: key="value with spaces"
function parseSlogAttrs(remainder: string): Record<string, string> | undefined {
  const attrs: Record<string, string> = {};
  const re = /\s+([a-zA-Z_][\w.]*)=((?:"(?:[^"\\]|\\.)*"|\S+))/g;
  let match;
  let found = false;
  while ((match = re.exec(remainder)) !== null) {
    found = true;
    let val = match[2];
    if (val.startsWith('"') && val.endsWith('"')) {
      val = val.slice(1, -1);
    }
    attrs[match[1]] = val;
  }
  return found ? attrs : undefined;
}

export function parseLogEntry(input: string | LogEntry): ParsedLog {
  if (typeof input === 'object') {
    return {
      level: toLogLevel(input.level),
      timestamp: input.ts || '',
      message: input.msg || '',
      component: input.component,
      traceId: input.trace_id,
      attrs: input.attrs,
      raw: JSON.stringify(input, null, 2),
    };
  }

  // Try JSON first
  try {
    const parsed = JSON.parse(input);
    return {
      level: toLogLevel(parsed.level),
      timestamp: parsed.ts || parsed.time || parsed.timestamp || '',
      message: parsed.msg || parsed.message || '',
      component: parsed.component || parsed.logger,
      traceId: parsed.trace_id || parsed.traceId,
      attrs: parsed,
      raw: input,
    };
  } catch {
    // Not JSON — try structured text formats
  }

  // Strip Docker timestamp prefix if present
  let line = input;
  let dockerTs = '';
  const dockerMatch = DOCKER_TS_RE.exec(line);
  if (dockerMatch) {
    dockerTs = dockerMatch[1];
    line = line.slice(dockerMatch[0].length);
  }

  // Try slog text format: time=... level=... msg="..."
  const slogMatch = SLOG_TEXT_RE.exec(line);
  if (slogMatch) {
    let msg = slogMatch[3];
    if (msg.startsWith('"') && msg.endsWith('"')) {
      msg = msg.slice(1, -1);
    }
    const attrs = parseSlogAttrs(slogMatch[4]);
    return {
      level: toLogLevel(slogMatch[2]),
      timestamp: slogMatch[1] || dockerTs,
      message: msg,
      component: attrs?.component,
      traceId: attrs?.trace_id ?? attrs?.traceId,
      attrs,
      raw: input,
    };
  }

  // Plain text — detect level from keywords
  const level: LogLevel = line.includes('ERROR')
    ? 'ERROR'
    : line.includes('WARN')
      ? 'WARN'
      : line.includes('INFO')
        ? 'INFO'
        : 'DEBUG';

  return {
    level,
    timestamp: dockerTs,
    message: line,
    raw: input,
  };
}

// Canonical source token for gateway-origin entries. Server-origin entries in
// the aggregate /api/logs buffer carry attrs.server; gateway components do
// not. URL state uses this token (`?source=gateway`), never the display string.
export const GATEWAY_LOG_SOURCE = 'gateway';

export function logSourceOf(log: ParsedLog): string {
  const server = log.attrs?.server;
  return typeof server === 'string' && server !== '' ? server : GATEWAY_LOG_SOURCE;
}

// Normalizes the ?source= URL param (or its permanent legacy alias ?agent=)
// to a source token: absent/empty = all sources (null). Legacy handles from
// the pre-workspace detached page carry the display string "Gateway" — fold
// it into the canonical token instead of filtering to nothing.
export function normalizeLogSourceParam(param: string | null): string | null {
  if (param === null || param === '') return null;
  return param === 'Gateway' ? GATEWAY_LOG_SOURCE : param;
}

export interface LogFilter {
  // null = all sources; GATEWAY_LOG_SOURCE = gateway-origin only; else server name.
  source?: string | null;
  levels?: Set<LogLevel>;
  query?: string;
  traceId?: string | null;
  /** Epoch ms cutoff: entries older than this are dropped. Entries with an
   * unparseable timestamp are kept (same policy as the clear watermark). */
  since?: number | null;
}

// Defensive stringification for attr values on the wire: slog delivers
// strings, numbers, bools, and dotted-flattened nested maps.
export function stringifyAttrValue(value: unknown): string {
  if (value == null) return '';
  if (typeof value === 'string') return value;
  if (typeof value === 'number' || typeof value === 'boolean') return String(value);
  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
}

function attrsMatchQuery(attrs: Record<string, unknown> | undefined, query: string): boolean {
  if (!attrs) return false;
  for (const value of Object.values(attrs)) {
    if (stringifyAttrValue(value).toLowerCase().includes(query)) return true;
  }
  return false;
}

// Single shared predicate so the workspace and the detached window filter the
// aggregate stream identically.
export function filterParsedLogs(logs: ParsedLog[], filter: LogFilter): ParsedLog[] {
  const query = filter.query?.toLowerCase() ?? '';
  return logs.filter((log) => {
    if (filter.source != null && logSourceOf(log) !== filter.source) return false;
    if (filter.levels && !filter.levels.has(log.level)) return false;
    if (filter.traceId && log.traceId !== filter.traceId) return false;
    if (filter.since != null) {
      const ts = Date.parse(log.timestamp);
      if (Number.isFinite(ts) && ts < filter.since) return false;
    }
    if (query) {
      return (
        log.message.toLowerCase().includes(query) ||
        log.component?.toLowerCase().includes(query) ||
        logSourceOf(log).toLowerCase().includes(query) ||
        log.traceId?.toLowerCase().includes(query) ||
        attrsMatchQuery(log.attrs, query)
      );
    }
    return true;
  });
}

// Stable per-entry identity for list rendering and expand state. Keys derive
// from entry fields, not array position, so the 2s poll that replaces the
// whole array keeps React reconciliation and the expanded row anchored to the
// same logical entry. Duplicate lines get an occurrence suffix to stay unique.
export function logEntryKeys(logs: ParsedLog[]): string[] {
  const seen = new Map<string, number>();
  return logs.map((log) => {
    const base = `${log.timestamp}|${logSourceOf(log)}|${log.traceId ?? ''}|${log.message}`;
    const n = seen.get(base) ?? 0;
    seen.set(base, n + 1);
    return n === 0 ? base : `${base}|#${n}`;
  });
}

export interface LogTailAnchor {
  key: string;
  /** Parsed timestamp in ms; NaN when the entry's timestamp does not parse. */
  ts: number;
}

export interface NewLogCount {
  count: number;
  /** The anchor left the list, so count is a lower bound. */
  overflow: boolean;
}

// Entries newer than the anchor (the newest line seen while following). The
// poll returns a sliding window, so the count is identity-based: by the
// anchor's position when it is still present, else by timestamp.
export function countNewLogs(
  keys: string[],
  logs: ParsedLog[],
  anchor: LogTailAnchor | null,
): NewLogCount {
  if (!anchor || logs.length === 0) return { count: 0, overflow: false };
  const i = keys.lastIndexOf(anchor.key);
  if (i >= 0) return { count: logs.length - 1 - i, overflow: false };
  if (!Number.isFinite(anchor.ts)) return { count: 0, overflow: false };
  let count = 0;
  for (const log of logs) {
    const ts = Date.parse(log.timestamp);
    if (Number.isFinite(ts) && ts > anchor.ts) count++;
  }
  return { count, overflow: count > 0 };
}

// Keep the previous ParsedLog when a poll repeats the same logEntryKeys
// identity and the same raw line. Memoized rows then see a stable reference.
// A changed raw under the same key (level, attrs) must not be reused.
export function reuseUnchangedLogs(previous: ParsedLog[], next: ParsedLog[]): ParsedLog[] {
  if (previous.length === 0 || next.length === 0) return next;
  const previousByKey = new Map<string, ParsedLog>();
  const previousKeys = logEntryKeys(previous);
  for (let i = 0; i < previous.length; i++) {
    previousByKey.set(previousKeys[i], previous[i]);
  }
  const nextKeys = logEntryKeys(next);
  return next.map((log, i) => {
    const prior = previousByKey.get(nextKeys[i]);
    if (prior && prior.raw === log.raw) return prior;
    return log;
  });
}

// hourCycle 'h23' pins midnight to 00. hour12: false resolved to h24 (24:05)
// on older engines that followed the pre-2023 ECMA-402 rule.
const LOG_TIME_FORMAT = new Intl.DateTimeFormat('en-US', {
  hourCycle: 'h23',
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
});

const pad2 = (n: number) => String(n).padStart(2, '0');

function formatLocalTime(date: Date): string {
  return LOG_TIME_FORMAT.format(date) + '.' + String(date.getMilliseconds()).padStart(3, '0');
}

/** HH:MM:SS.mmm in local time, prefixed with MM-DD when showDate is set. */
export function formatTimestamp(ts: string, showDate = false): string {
  if (!ts) return '';
  const date = new Date(ts);
  // An unparseable timestamp yields an invalid Date, not an exception;
  // unguarded formatting would render "Invalid Date.NaN".
  if (!Number.isFinite(date.getTime())) {
    return ts.slice(11, 23) || '\u2014';
  }
  const time = formatLocalTime(date);
  return showDate ? `${pad2(date.getMonth() + 1)}-${pad2(date.getDate())} ${time}` : time;
}

const LOG_ZONE_FORMAT = new Intl.DateTimeFormat('en-US', { timeZoneName: 'short' });

function shortTimeZoneName(date: Date): string {
  const part = LOG_ZONE_FORMAT.formatToParts(date)
    .find((p) => p.type === 'timeZoneName');
  return part?.value ?? '';
}

/** Tooltip text: full local date, time, and zone, then the original string. */
export function formatTimestampTitle(ts: string): string {
  const date = new Date(ts);
  if (!ts || !Number.isFinite(date.getTime())) return ts;
  const day = `${date.getFullYear()}-${pad2(date.getMonth() + 1)}-${pad2(date.getDate())}`;
  const zone = shortTimeZoneName(date);
  return `${day} ${formatLocalTime(date)}${zone ? ` ${zone}` : ''} \u00b7 ${ts}`;
}

/** True when the parseable timestamps fall on more than one local calendar day. */
export function logsSpanMultipleDays(logs: ParsedLog[]): boolean {
  let min = Infinity;
  let max = -Infinity;
  for (const log of logs) {
    const t = Date.parse(log.timestamp);
    if (!Number.isFinite(t)) continue;
    if (t < min) min = t;
    if (t > max) max = t;
  }
  if (min === Infinity) return false;
  return new Date(min).toDateString() !== new Date(max).toDateString();
}

const formatCount = (n: number) => n.toLocaleString('en-US');

/** Visible count-line text for the filter bar. */
export function formatLogCount({
  filtered,
  loaded,
  bufferTotal,
  bufferCapacity,
}: {
  filtered: number;
  loaded: number;
  bufferTotal: number;
  bufferCapacity: number;
}): string {
  if (bufferCapacity > 0) {
    if (filtered === loaded) {
      return `Showing ${formatCount(loaded)} of ${formatCount(bufferTotal)} buffered`;
    }
    return `${formatCount(filtered)} of ${formatCount(loaded)} match \u00b7 ${formatCount(bufferTotal)} buffered`;
  }
  const noun = loaded === 1 ? 'entry' : 'entries';
  if (filtered === loaded) return `${formatCount(loaded)} ${noun}`;
  return `${formatCount(filtered)} of ${formatCount(loaded)} ${noun}`;
}

/** Tooltip sentence for the count line naming the window size and buffer limit. */
export function formatLogCountTitle({
  windowSize,
  bufferTotal,
  bufferCapacity,
}: {
  windowSize?: number;
  bufferTotal: number;
  bufferCapacity: number;
}): string {
  const parts: string[] = [];
  if (windowSize) {
    parts.push(
      `Loaded the most recent ${formatCount(windowSize)} entries; change the window under View options.`,
    );
  }
  if (bufferCapacity > 0) {
    parts.push(
      `The server keeps the newest ${formatCount(bufferCapacity)} lines and holds ${formatCount(bufferTotal)} now.`,
    );
  }
  return parts.join(' ');
}

export interface HiddenQueryMatch {
  label: 'trace ID' | 'attributes';
  title: string;
}

// Names the first place the filter matched that the collapsed row does not
// render, so a kept row always shows why. Normalization matches
// filterParsedLogs: lowercased, not trimmed.
export function hiddenQueryMatch(
  log: ParsedLog,
  query: string,
  visibleTexts: readonly (string | undefined)[],
): HiddenQueryMatch | null {
  const q = query.toLowerCase();
  if (!q) return null;
  if (visibleTexts.some((t) => t?.toLowerCase().includes(q))) return null;
  if (log.traceId?.toLowerCase().includes(q)) {
    return { label: 'trace ID', title: `trace_id=${log.traceId}` };
  }
  for (const [key, value] of Object.entries(log.attrs ?? {})) {
    const text = stringifyAttrValue(value);
    if (text.toLowerCase().includes(q)) return { label: 'attributes', title: `${key}=${text}` };
  }
  return null;
}

// Selectable poll window sizes. 1000 equals the server ring capacity, so the
// UI cannot offer a larger window than the buffer can hold. The default is
// omitted from URLs and prefs comparisons everywhere it appears.
export const LOG_WINDOW_SIZES = [200, 500, 1000] as const;
export const DEFAULT_LOG_WINDOW = 500;

export function normalizeLogWindowParam(param: string | null, fallback: number): number {
  const n = param ? Number(param) : NaN;
  return (LOG_WINDOW_SIZES as readonly number[]).includes(n) ? n : fallback;
}

/** Client-side time window over the buffered timestamps. */
export type LogTimeRange = '1m' | '5m' | '15m' | 'all';

export const LOG_TIME_RANGES: { value: LogTimeRange; label: string }[] = [
  { value: '1m', label: '1m' },
  { value: '5m', label: '5m' },
  { value: '15m', label: '15m' },
  { value: 'all', label: 'Window' },
];

export const LOG_TIME_RANGE_MS: Record<LogTimeRange, number | null> = {
  '1m': 60 * 1000,
  '5m': 5 * 60 * 1000,
  '15m': 15 * 60 * 1000,
  all: null,
};

export function normalizeLogTimeRangeParam(param: string | null): LogTimeRange {
  return param === '1m' || param === '5m' || param === '15m' ? param : 'all';
}

// MCP-first promotion for the expand panel: these attr keys render as named
// fields at the top; anything else collapses under "Other attributes". Keys
// are slog-flat names as emitted by the gateway, not OTel dotted keys.
export const PROMOTED_LOG_FIELDS: { label: string; key: string }[] = [
  { label: 'Tool', key: 'tool' },
  { label: 'Server', key: 'server' },
  { label: 'Client', key: 'client' },
  { label: 'Replica', key: 'replica_id' },
  { label: 'Duration', key: 'duration' },
  { label: 'Error', key: 'error' },
  { label: 'Is error', key: 'is_error' },
];

export const PROMOTED_LOG_KEYS = new Set(PROMOTED_LOG_FIELDS.map((f) => f.key));

const GO_DURATION_UNIT_MS: Record<string, number> = {
  h: 3_600_000,
  m: 60_000,
  s: 1000,
  ms: 1,
  'µs': 0.001,
  'μs': 0.001,
  us: 0.001,
  ns: 0.000001,
};

const GO_DURATION_PART_RE = /(\d+(?:\.\d*)?|\.\d+)(ns|us|µs|μs|ms|s|m|h)/y;

// Parses a Go time.Duration string ("1.234567ms", "850µs", "1m2.5s") into
// milliseconds. Returns null when the string is not a Go duration.
export function parseGoDurationMs(value: string): number | null {
  let rest = value.trim();
  let sign = 1;
  if (rest.startsWith('-') || rest.startsWith('+')) {
    if (rest[0] === '-') sign = -1;
    rest = rest.slice(1);
  }
  if (rest === '0') return 0;
  if (rest === '') return null;
  GO_DURATION_PART_RE.lastIndex = 0;
  let total = 0;
  while (GO_DURATION_PART_RE.lastIndex < rest.length) {
    const match = GO_DURATION_PART_RE.exec(rest);
    if (!match) return null;
    total += Number(match[1]) * GO_DURATION_UNIT_MS[match[2]];
  }
  return Number.isFinite(total) ? sign * total : null;
}

// Go duration strings render compactly; anything else, including a numeric
// value, is shown as it arrived. Negative and sub-microsecond values also
// stay raw, since the compact form would misstate them.
export function formatGoDuration(value: unknown): string {
  const raw = stringifyAttrValue(value);
  if (typeof value !== 'string') return raw;
  const ms = parseGoDurationMs(value);
  if (ms === null || ms < 0 || (ms !== 0 && ms < 0.0005)) return raw;
  return formatDuration(ms);
}

export interface InlineLogField {
  key: string;
  label: string | null;
  value: string;
  tone: 'default' | 'ok' | 'error';
  title: string;
}

interface InlineFieldSpec {
  key: string;
  label: string | null;
  format?: (value: unknown) => { value: string; tone?: InlineLogField['tone'] } | null;
  tone?: InlineLogField['tone'];
}

function formatOutcome(value: unknown): { value: string; tone: InlineLogField['tone'] } | null {
  if (value === false || value === 'false') return { value: 'ok', tone: 'ok' };
  if (value === true || value === 'true') return { value: 'error', tone: 'error' };
  return null;
}

// Attribute keys shown after the message in a collapsed row, in priority
// order. `server` and `replica_id` are absent on purpose: the source column
// already shows the server, and replica ids are noise at row level. `client`
// sits after the outcome and error so those never lose the cap to it.
const INLINE_LOG_FIELDS: InlineFieldSpec[] = [
  { key: 'tool', label: 'tool' },
  { key: 'name', label: 'name' },
  { key: 'duration', label: null, format: (v) => ({ value: formatGoDuration(v) }) },
  { key: 'is_error', label: null, format: formatOutcome },
  { key: 'error', label: 'error', tone: 'error' },
  { key: 'client', label: 'client' },
  { key: 'reason', label: 'reason' },
  { key: 'status', label: 'status' },
  { key: 'method', label: 'method' },
  { key: 'path', label: 'path' },
  { key: 'output', label: null },
];

export const MAX_INLINE_LOG_FIELDS = 4;

export function inlineLogFields(log: ParsedLog): InlineLogField[] {
  const attrs = log.attrs;
  if (!attrs) return [];
  const fields: InlineLogField[] = [];
  for (const spec of INLINE_LOG_FIELDS) {
    if (fields.length >= MAX_INLINE_LOG_FIELDS) break;
    const raw = attrs[spec.key];
    if (raw == null || raw === '') continue;
    const rawText = stringifyAttrValue(raw);
    const formatted = spec.format ? spec.format(raw) : { value: rawText };
    if (!formatted) continue;
    const value = formatted.value.replace(/\s*[\r\n]+\s*/g, ' ').trim();
    if (value === '') continue;
    fields.push({
      key: spec.key,
      label: spec.label,
      value,
      tone: formatted.tone ?? spec.tone ?? 'default',
      title: `${spec.key}=${rawText}`,
    });
  }
  return fields;
}

// Export serialization. JSONL rebuilds the wire-shaped entry object per line —
// `raw` is pretty-printed multi-line JSON and would not be valid JSONL.
export function serializeLogsJSONL(logs: ParsedLog[]): string {
  return logs
    .map((log) =>
      JSON.stringify({
        level: log.level,
        ts: log.timestamp,
        msg: log.message,
        ...(log.component ? { component: log.component } : {}),
        ...(log.traceId ? { trace_id: log.traceId } : {}),
        ...(log.attrs && Object.keys(log.attrs).length > 0 ? { attrs: log.attrs } : {}),
      }),
    )
    .join('\n');
}

export function serializeLogsText(logs: ParsedLog[]): string {
  return logs
    .map((log) => {
      const parts = [log.timestamp, log.level, logSourceOf(log)];
      if (log.component) parts.push(log.component);
      parts.push(log.message);
      return parts.join('  ');
    })
    .join('\n');
}
