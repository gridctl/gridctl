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
// not. URL state uses this token (`?agent=gateway`), never the display string.
export const GATEWAY_LOG_SOURCE = 'gateway';

export function logSourceOf(log: ParsedLog): string {
  const server = log.attrs?.server;
  return typeof server === 'string' && server !== '' ? server : GATEWAY_LOG_SOURCE;
}

// Normalizes the ?agent= URL param to a source token: absent/empty = all
// sources (null). Legacy handles from the pre-workspace detached page carry
// the display string "Gateway" — fold it into the canonical token instead of
// filtering to nothing.
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
}

// Single shared predicate so the workspace and the detached window filter the
// aggregate stream identically.
export function filterParsedLogs(logs: ParsedLog[], filter: LogFilter): ParsedLog[] {
  const query = filter.query?.toLowerCase() ?? '';
  return logs.filter((log) => {
    if (filter.source != null && logSourceOf(log) !== filter.source) return false;
    if (filter.levels && !filter.levels.has(log.level)) return false;
    if (filter.traceId && log.traceId !== filter.traceId) return false;
    if (query) {
      return (
        log.message.toLowerCase().includes(query) ||
        log.component?.toLowerCase().includes(query) ||
        logSourceOf(log).toLowerCase().includes(query) ||
        log.traceId?.toLowerCase().includes(query)
      );
    }
    return true;
  });
}

export function formatTimestamp(ts: string): string {
  if (!ts) return '';
  try {
    const date = new Date(ts);
    return date.toLocaleTimeString('en-US', {
      hour12: false,
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    }) + '.' + String(date.getMilliseconds()).padStart(3, '0');
  } catch {
    return ts.slice(11, 23);
  }
}
