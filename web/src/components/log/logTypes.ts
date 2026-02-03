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
    const level: LogLevel = input.includes('ERROR')
      ? 'ERROR'
      : input.includes('WARN')
        ? 'WARN'
        : input.includes('INFO')
          ? 'INFO'
          : 'DEBUG';

    return {
      level,
      timestamp: '',
      message: input,
      raw: input,
    };
  }
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
