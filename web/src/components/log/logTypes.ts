import type { LogEntry } from '../../lib/api';

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

export function formatTimestamp(ts: string): string {
  if (!ts) return '';
  const date = new Date(ts);
  // An unparseable timestamp yields an invalid Date, not an exception —
  // unguarded formatting would render "Invalid Date.NaN".
  if (!Number.isFinite(date.getTime())) {
    return ts.slice(11, 23) || '\u2014';
  }
  return date.toLocaleTimeString('en-US', {
    hour12: false,
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  }) + '.' + String(date.getMilliseconds()).padStart(3, '0');
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
