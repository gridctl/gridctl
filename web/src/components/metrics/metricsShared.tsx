import { type ComponentType, type ReactNode } from 'react';
import { ArrowDown, ArrowUp, ArrowUpDown, DollarSign } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { cn } from '../../lib/cn';
import { formatCompactNumber, formatUSD } from '../../lib/format';
import { AreaChart } from '../chart/AreaChart';
import { ATTRIBUTION_HINT, MIXED_PROVENANCE_NOTE } from '../pricing/constants';
import { sharePct } from '../pricing/effectiveModel';
import type { ModelShare, TokenMetricsResponse, CostMetricsResponse } from '../../types';
import type {
  BreakdownRow,
  BreakdownSortColumn,
  SessionKpis,
  SortDirection,
} from './metricsData';

// Presentational atoms shared by every metrics surface (bottom glance tab,
// Metrics workspace, detached window). Pure data helpers and types live in
// metricsData.ts — this file is components only so Fast Refresh stays happy.

// ---------------------------------------------------------------------------
// KPI cards
// ---------------------------------------------------------------------------

export function KPICard({ label, value, colorClass }: { label: string; value: number; colorClass: string }) {
  return (
    <div className="rounded-lg bg-surface-elevated/60 border border-border/30 p-3">
      <span className="text-[10px] text-text-muted uppercase tracking-wider block mb-1">{label}</span>
      <span className={cn('text-lg font-bold tabular-nums', colorClass)}>{formatCompactNumber(value)}</span>
    </div>
  );
}

// CostKPICard — session USD spend. Renders an em-dash when nothing has been
// priced yet (never a fabricated number). Cost is conveyed by the "$" icon and
// the "Cost" label, not color alone. The honesty subline points at the config
// requirement (showHint) or the mixed-provenance caveat.
export function CostKPICard({
  usd,
  hasCost,
  showHint,
  showMixedNote,
}: {
  usd: number | undefined;
  hasCost: boolean;
  showHint?: boolean;
  showMixedNote?: boolean;
}) {
  return (
    <div className="rounded-lg bg-surface-elevated/60 border border-border/30 p-3">
      <span className="text-[10px] text-text-muted uppercase tracking-wider flex items-center gap-1 mb-1">
        <DollarSign size={10} className="text-text-muted/70" />
        Cost <span className="text-text-muted/50 normal-case tracking-normal">· est.</span>
      </span>
      <span className={cn('text-lg font-bold tabular-nums', hasCost ? 'text-emerald-400' : 'text-text-muted')}>
        {hasCost ? formatUSD(usd ?? 0) : '—'}
      </span>
      {showHint && (
        <span className="block mt-1 text-[9px] leading-snug text-text-muted/60">{ATTRIBUTION_HINT}</span>
      )}
      {!showHint && showMixedNote && (
        <span className="block mt-1 text-[9px] leading-snug text-text-muted/60">{MIXED_PROVENANCE_NOTE}</span>
      )}
    </div>
  );
}

export function FormatSavingsCard({ savingsPercent, savedTokens }: { savingsPercent: number; savedTokens: number }) {
  return (
    <div className="rounded-lg bg-surface-elevated/60 border border-border/30 p-3">
      <span className="text-[10px] text-text-muted uppercase tracking-wider block mb-1">Format Savings</span>
      <div className="flex items-baseline gap-2">
        <span className="text-lg font-bold text-status-running tabular-nums">{Math.round(savingsPercent)}%</span>
        <span className="text-[10px] text-text-muted">{formatCompactNumber(savedTokens)} saved</span>
      </div>
      <div className="mt-2 h-1.5 rounded-full bg-surface-highlight overflow-hidden flex">
        <div className="h-full bg-primary rounded-full" style={{ width: `${100 - savingsPercent}%` }} />
        <div className="h-full bg-primary/20" style={{ width: `${savingsPercent}%` }} />
      </div>
    </div>
  );
}

// The full session KPI grid, identical across surfaces.
export function MetricsKpiRow({ kpis }: { kpis: SessionKpis }) {
  return (
    <div className={cn('grid gap-3', kpis.savingsPercent > 0 ? 'grid-cols-5' : 'grid-cols-4')}>
      <KPICard label="Input Tokens" value={kpis.input} colorClass="text-secondary" />
      <KPICard label="Output Tokens" value={kpis.output} colorClass="text-primary" />
      <KPICard label="Total Tokens" value={kpis.total} colorClass="text-text-primary" />
      <CostKPICard
        usd={kpis.costUSD}
        hasCost={kpis.hasCost}
        showHint={kpis.showAttributionHint}
        showMixedNote={kpis.hasMixedProvenance}
      />
      {kpis.savingsPercent > 0 && (
        <FormatSavingsCard savingsPercent={kpis.savingsPercent} savedTokens={kpis.savedTokens} />
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Charts (with screen-reader text alternatives)
// ---------------------------------------------------------------------------

type TokenPoint = { time: string; 'Input Tokens': number; 'Output Tokens': number };
type CostPoint = { time: string; 'Cost (USD)': number };

// Exposes a role="img" + aria-label summary, since the underlying Recharts SVG
// has no accessible description. The breakdown tables remain the full data
// fallback for assistive tech.
function ChartFrame({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div role="img" aria-label={label}>
      {children}
    </div>
  );
}

export function TokenChart({
  data,
  metricsData,
  heightClass = 'h-36',
}: {
  data: TokenPoint[];
  metricsData: TokenMetricsResponse | null;
  heightClass?: string;
}) {
  const peak = data.reduce((m, d) => Math.max(m, d['Input Tokens'] + d['Output Tokens']), 0);
  const summary = `Token usage over time: ${data.length} points, peak ${formatCompactNumber(peak)} tokens per interval.`;
  return (
    <div className="rounded-lg bg-surface-elevated/60 border border-border/30 p-3">
      <div className="flex items-center justify-between mb-1">
        <span className="text-[11px] font-medium text-text-secondary">Token Usage Over Time</span>
        {metricsData && (
          <span className="text-[9px] text-text-muted font-mono">
            {metricsData.data_points?.length ?? 0} points &middot; {metricsData.interval} interval
          </span>
        )}
      </div>
      <ChartFrame label={summary}>
        <AreaChart
          data={data}
          index="time"
          categories={['Input Tokens', 'Output Tokens']}
          colors={['teal', 'amber']}
          type="stacked"
          fill="gradient"
          showLegend
          showGridLines
          showYAxis
          yAxisWidth={48}
          valueFormatter={(v: number) => formatCompactNumber(v)}
          className={heightClass}
        />
      </ChartFrame>
    </div>
  );
}

export function CostChart({
  data,
  costData,
  heightClass = 'h-32',
}: {
  data: CostPoint[];
  costData: CostMetricsResponse | null;
  heightClass?: string;
}) {
  const peak = data.reduce((m, d) => Math.max(m, d['Cost (USD)']), 0);
  const summary = `Estimated cost over time: ${data.length} points, peak ${formatUSD(peak)} per interval.`;
  return (
    <div className="rounded-lg bg-surface-elevated/60 border border-border/30 p-3">
      <div className="flex items-center justify-between mb-1">
        <span className="text-[11px] font-medium text-text-secondary inline-flex items-center gap-1.5">
          <DollarSign size={11} className="text-emerald-400" />
          Cost Over Time
        </span>
        {costData && (
          <span className="text-[9px] text-text-muted font-mono">
            {costData.data_points?.length ?? 0} points &middot; {costData.interval} interval
          </span>
        )}
      </div>
      <ChartFrame label={summary}>
        <AreaChart
          data={data}
          index="time"
          categories={['Cost (USD)']}
          colors={['emerald']}
          type="default"
          fill="gradient"
          // Legend on so cost is labeled by text, not color alone.
          showLegend
          showGridLines
          showYAxis
          yAxisWidth={56}
          valueFormatter={(v: number) => formatUSD(v)}
          className={heightClass}
        />
      </ChartFrame>
    </div>
  );
}

// Ranked horizontal bars for the model-mix (preferred over pie/donut for many
// categories). Each row is a model with its cost share, read as text + length.
export function ModelMixBars({ mix }: { mix: ModelShare[] }) {
  if (mix.length === 0) {
    return (
      <p className="px-3 py-3 text-[11px] text-text-muted/70 leading-relaxed">
        No priced traffic yet. Declare a client, server, or default pricing model to populate the
        model mix.
      </p>
    );
  }
  const max = mix[0]?.share ?? 1;
  return (
    <ul className="px-3 py-2 space-y-1.5" aria-label="Cost by model">
      {mix.map((m) => (
        <li key={m.model} className="flex items-center gap-2">
          <span className="w-40 flex-shrink-0 truncate font-mono text-[10px] text-text-secondary" title={m.model}>
            {m.model}
          </span>
          <span className="relative flex-1 h-3 rounded-sm bg-surface-highlight/40 overflow-hidden">
            <span
              className="absolute inset-y-0 left-0 rounded-sm bg-emerald-500/40"
              style={{ width: `${max > 0 ? (m.share / max) * 100 : 0}%` }}
            />
          </span>
          <span className="w-12 flex-shrink-0 text-right tabular-nums text-[10px] text-text-muted">
            {sharePct(m.share)}
          </span>
          <span className="w-16 flex-shrink-0 text-right tabular-nums text-[10px] text-emerald-400/90">
            {formatUSD(m.cost_usd)}
          </span>
        </li>
      ))}
    </ul>
  );
}

// ---------------------------------------------------------------------------
// Table primitives
// ---------------------------------------------------------------------------

export function PanelHeader({
  icon: Icon,
  label,
  children,
  right,
}: {
  icon: LucideIcon | ComponentType<{ size?: number; className?: string }>;
  label: string;
  children: ReactNode;
  right?: ReactNode;
}) {
  return (
    <div className="rounded-lg bg-surface-elevated/60 border border-border/30 overflow-hidden">
      <div className="flex items-center gap-1.5 px-3 py-1.5 border-b border-border/30 bg-surface-highlight/30">
        <Icon size={11} className="text-text-muted" />
        <span className="text-[11px] font-medium text-text-secondary">{label}</span>
        {right && <span className="ml-auto">{right}</span>}
      </div>
      {children}
    </div>
  );
}

export function SortableHeader({
  label,
  column,
  sortColumn,
  sortDirection,
  onSort,
  align = 'left',
}: {
  label: string;
  column: BreakdownSortColumn;
  sortColumn: BreakdownSortColumn;
  sortDirection: SortDirection;
  onSort: (column: BreakdownSortColumn) => void;
  align?: 'left' | 'right';
}) {
  const isActive = sortColumn === column;
  const SortIcon = isActive ? (sortDirection === 'asc' ? ArrowUp : ArrowDown) : ArrowUpDown;
  return (
    <th
      className={cn(
        'px-3 py-2 font-medium text-text-muted cursor-pointer hover:text-text-secondary transition-colors select-none',
        align === 'right' && 'text-right',
      )}
      tabIndex={0}
      role="columnheader"
      aria-sort={isActive ? (sortDirection === 'asc' ? 'ascending' : 'descending') : 'none'}
      onClick={() => onSort(column)}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          onSort(column);
        }
      }}
    >
      <span className="inline-flex items-center gap-1">
        {label}
        <SortIcon size={10} className={isActive ? 'text-primary' : 'text-text-muted/40'} />
      </span>
    </th>
  );
}

// ScrollableBreakdown bounds a long BreakdownTable (the per-tool list runs to
// 100+ rows on a real stack) to an in-place scroll region with a sticky
// header, shared by the Metrics workspace Tools scope and the detached
// window's Per-Tool panel.
export function ScrollableBreakdown({ children }: { children: ReactNode }) {
  return (
    <div className="max-h-[60vh] overflow-y-auto scrollbar-dark [&_thead_th]:sticky [&_thead_th]:top-0 [&_thead_th]:z-[1] [&_thead_th]:bg-surface">
      {children}
    </div>
  );
}

// InspectorStat renders one labeled KPI number, shared by the Metrics
// inspector and the Tools detail panel so per-entity numbers read the same
// in both workspaces.
export function InspectorStat({ label, value, className }: { label: string; value: string; className?: string }) {
  return (
    <div className="rounded-lg bg-surface-elevated/60 border border-border/30 p-2.5">
      <span className="text-[9px] text-text-muted uppercase tracking-wider block mb-0.5">{label}</span>
      <span className={cn('text-sm font-bold tabular-nums', className)}>{value}</span>
    </div>
  );
}

// BreakdownTable renders the shared client/server breakdown. Each host injects
// a Model cell via `renderModel`, shows a Cost column when `showCost`, and may
// make rows selectable (`onSelectRow` + `selectedName`) to drive a detail
// inspector. The Model cell stops propagation so editing never selects the row.
export function BreakdownTable({
  rows,
  nameLabel,
  sortColumn,
  sortDirection,
  onSort,
  renderModel,
  showCost = false,
  selectedName,
  onSelectRow,
}: {
  rows: BreakdownRow[];
  nameLabel: string;
  sortColumn: BreakdownSortColumn;
  sortDirection: SortDirection;
  onSort: (column: BreakdownSortColumn) => void;
  renderModel?: (row: BreakdownRow) => ReactNode;
  showCost?: boolean;
  selectedName?: string | null;
  onSelectRow?: (name: string) => void;
}) {
  return (
    <table className="w-full text-xs">
      <thead>
        <tr className="border-b border-border/30">
          <SortableHeader label={nameLabel} column="name" sortColumn={sortColumn} sortDirection={sortDirection} onSort={onSort} />
          {renderModel && (
            <th className="px-3 py-1.5 text-left text-[10px] font-medium text-text-muted uppercase tracking-wider">Model</th>
          )}
          <SortableHeader label="Input" column="input" sortColumn={sortColumn} sortDirection={sortDirection} onSort={onSort} align="right" />
          <SortableHeader label="Output" column="output" sortColumn={sortColumn} sortDirection={sortDirection} onSort={onSort} align="right" />
          <SortableHeader label="Total" column="total" sortColumn={sortColumn} sortDirection={sortDirection} onSort={onSort} align="right" />
          {showCost && (
            <SortableHeader label="Cost" column="cost" sortColumn={sortColumn} sortDirection={sortDirection} onSort={onSort} align="right" />
          )}
        </tr>
      </thead>
      <tbody>
        {rows.map((row) => {
          const selected = selectedName === row.name;
          return (
            <tr
              key={row.name}
              aria-selected={onSelectRow ? selected : undefined}
              onClick={onSelectRow ? () => onSelectRow(row.name) : undefined}
              // Selectable rows are keyboard-reachable in place (Enter/Space),
              // complementing the workspace-level arrow-key navigation. Only
              // keydowns on the row itself count: events bubbling out of nested
              // interactive elements (the inline model editor) must keep their
              // native behavior.
              tabIndex={onSelectRow ? 0 : undefined}
              onKeyDown={
                onSelectRow
                  ? (e) => {
                      if (e.target === e.currentTarget && (e.key === 'Enter' || e.key === ' ')) {
                        e.preventDefault();
                        onSelectRow(row.name);
                      }
                    }
                  : undefined
              }
              className={cn(
                'border-b border-border/20 last:border-0 transition-colors',
                onSelectRow && 'cursor-pointer focus-visible:outline focus-visible:outline-1 focus-visible:outline-primary/60',
                selected ? 'bg-primary/[0.07]' : 'hover:bg-surface-highlight/30',
              )}
            >
              <td className="px-3 py-2 font-medium text-text-primary font-mono">
                {row.server && row.tool ? (
                  <>
                    <span className="text-text-muted/70">{row.server}</span>
                    <span className="text-text-muted/50" aria-hidden="true">{' › '}</span>
                    {row.tool}
                  </>
                ) : (
                  row.name
                )}
              </td>
              {renderModel && (
                <td className="px-3 py-2" onClick={(e) => e.stopPropagation()}>
                  {renderModel(row)}
                </td>
              )}
              <td className="px-3 py-2 text-right text-secondary tabular-nums">{formatCompactNumber(row.input)}</td>
              <td className="px-3 py-2 text-right text-primary tabular-nums">{formatCompactNumber(row.output)}</td>
              <td className="px-3 py-2 text-right text-text-primary font-semibold tabular-nums">{formatCompactNumber(row.total)}</td>
              {showCost && (
                <td className={cn('px-3 py-2 text-right tabular-nums', row.cost === undefined ? 'text-text-muted' : 'text-emerald-400')}>
                  {row.cost === undefined ? '—' : formatUSD(row.cost)}
                </td>
              )}
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}
