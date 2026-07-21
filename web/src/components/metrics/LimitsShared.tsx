import { Gauge, ShieldAlert } from 'lucide-react';
import { cn } from '../../lib/cn';
import { formatUSD } from '../../lib/format';
import type { LimitEntry } from '../../lib/api';
import { limitStateFillClass, limitStateTextClass, type LimitsSummary } from './limitsData';
import { PanelHeader } from './metricsShared';

// Presentational pieces for the limits overlay, shared by the Metrics
// workspace and the detached window (same parity contract as metricsShared).

// BudgetBar is the compact consumption-vs-cap bar rendered under a breakdown
// row's name (and inside the Limits panel). Spend is always a real number
// here — a zero-spend budget shows $0.00, never the em-dash, because the
// dash means "unknown" and a fresh window is a known zero.
export function BudgetBar({ entry, className }: { entry: LimitEntry; className?: string }) {
  const b = entry.budget;
  if (!b) return null;
  const pct = Math.min(100, Math.max(0, b.percent));
  const label = `${formatUSD(b.spent_usd)} of ${formatUSD(b.max_usd)} ${b.period} budget (${Math.round(b.percent)}%)${
    entry.state === 'exceeded' ? ', exceeded' : entry.state === 'warn' ? ', near cap' : ''
  }`;
  return (
    <div className={cn('flex items-center gap-1.5 min-w-0', className)} role="img" aria-label={label} title={label}>
      <span className="relative h-1 w-16 flex-shrink-0 rounded-full bg-surface-highlight/60 overflow-hidden">
        <span
          className={cn('absolute inset-y-0 left-0 rounded-full', limitStateFillClass(entry.state))}
          style={{ width: `${pct}%` }}
        />
      </span>
      <span className={cn('text-[9px] tabular-nums whitespace-nowrap', limitStateTextClass(entry.state))}>
        {formatUSD(b.spent_usd)}/{formatUSD(b.max_usd)}
      </span>
    </div>
  );
}

// One row of the Limits panel: budget entries get the full bar, rate entries
// a compact calls/min annotation.
function LimitsPanelRow({ entry }: { entry: LimitEntry }) {
  return (
    <li className="flex items-center gap-2 px-3 py-1.5">
      <span
        className={cn(
          'w-14 flex-shrink-0 text-[9px] uppercase tracking-wider',
          entry.kind === 'budget' ? 'text-text-muted' : 'text-text-muted/70',
        )}
      >
        {entry.kind === 'budget' ? entry.budget?.period ?? 'budget' : 'rate'}
      </span>
      <span className="w-40 flex-shrink-0 truncate font-mono text-[10px] text-text-secondary" title={entry.key}>
        <span className="text-text-muted/60">{entry.scope}:</span> {entry.key}
      </span>
      {entry.kind === 'budget' ? (
        <BudgetBar entry={entry} className="flex-1" />
      ) : (
        <span className="flex-1 text-[10px] tabular-nums text-text-muted">
          {entry.rate?.calls_per_minute} calls/min
          <span className="text-text-muted/60"> · burst {entry.rate?.burst}</span>
        </span>
      )}
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

// LimitsPanel lists every configured limit, elevated states first. It is the
// guaranteed-visibility surface: an entry whose scope key has no matching
// breakdown row (a tool budget before the tool's first call) still shows
// here. Renders nothing when no limits: block is configured, so stacks
// without limits are visually unchanged.
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
      label="Limits"
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
      <ul className="py-1 divide-y divide-border/15" aria-label="Configured limits">
        {sorted.map((e) => (
          <LimitsPanelRow key={`${e.kind}:${e.scope}:${e.key}`} entry={e} />
        ))}
      </ul>
    </PanelHeader>
  );
}
