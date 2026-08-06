import { Gauge, ShieldAlert } from 'lucide-react';
import { cn } from '../../lib/cn';
import type { LimitEntry } from '../../lib/api';
import { limitStateTextClass, type LimitsSummary } from './limitsData';
import { PanelHeader } from './metricsShared';

// Presentational pieces for the rate-limit overlay, shared by the Metrics
// workspace and the detached window (same parity contract as metricsShared).

// One row of the Limits panel: a rate entry with its calls/min annotation.
function LimitsPanelRow({ entry }: { entry: LimitEntry }) {
  return (
    <li className="flex items-center gap-2 px-3 py-1.5">
      <span className="w-14 flex-shrink-0 text-[9px] uppercase tracking-wider text-text-muted/70">rate</span>
      <span className="w-40 flex-shrink-0 truncate font-mono text-[10px] text-text-secondary" title={entry.key}>
        <span className="text-text-muted/60">{entry.scope}:</span> {entry.key}
      </span>
      <span className="flex-1 text-[10px] tabular-nums text-text-muted">
        {entry.rate?.calls_per_minute} calls/min
        <span className="text-text-muted/60"> · burst {entry.rate?.burst}</span>
      </span>
      <span
        className={cn(
          'w-16 flex-shrink-0 text-right text-[9px] font-medium uppercase tracking-wider',
          entry.state === 'ok' ? 'text-status-running' : limitStateTextClass(entry.state),
        )}
      >
        {entry.state}
      </span>
    </li>
  );
}

// LimitsPanel lists every configured rate limit, elevated states first. It is
// the guaranteed-visibility surface: an entry whose scope key has no matching
// breakdown row (a tool limit before the tool's first call) still shows here.
// Renders nothing when no limits: block is configured, so stacks without
// limits are visually unchanged.
export function LimitsPanel({ summary }: { summary: LimitsSummary }) {
  if (!summary.configured || summary.entries.length === 0) return null;
  const order = { exceeded: 0, warn: 1, ok: 2 } as const;
  const sorted = [...summary.entries].sort(
    (a, b) => order[a.state] - order[b.state] || a.key.localeCompare(b.key),
  );
  const elevated = summary.exceededCount + summary.warnCount;
  return (
    <PanelHeader
      icon={summary.worst === 'ok' ? Gauge : ShieldAlert}
      label="Rate Limits"
      right={
        elevated > 0 ? (
          <span className={cn('text-[10px] font-medium', limitStateTextClass(summary.worst))}>
            {summary.exceededCount > 0
              ? `${summary.exceededCount} exceeded`
              : `${summary.warnCount} near cap`}
          </span>
        ) : undefined
      }
    >
      <ul className="py-1 divide-y divide-border/15" aria-label="Configured rate limits">
        {sorted.map((e) => (
          <LimitsPanelRow key={`${e.kind}:${e.scope}:${e.key}`} entry={e} />
        ))}
      </ul>
    </PanelHeader>
  );
}
