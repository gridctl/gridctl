import { type ComponentType, type ReactNode } from 'react';
import { ArrowDown, ArrowUp, ArrowUpDown, DollarSign } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { cn } from '../../lib/cn';
import { formatCompactNumber, formatUSD } from '../../lib/format';
import { AreaChart } from '../chart/AreaChart';
import type { AvailableChartColorsKeys } from '../chart/chartColors';
import { ATTRIBUTION_HINT, MIXED_PROVENANCE_NOTE } from '../pricing/constants';
import { sharePct } from '../pricing/effectiveModel';
import type { ModelProvenance, ModelShare, TokenMetricsResponse, CostMetricsResponse } from '../../types';
import type {
  BreakdownRow,
  BreakdownSortColumn,
  ModelRow,
  SessionKpis,
  SortDirection,
  WindowTotals,
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

// CostKPICard — USD spend for the active window. Renders an em-dash when
// nothing has been priced (hasCost) OR when the window cost is simply unknown
// (usd undefined, cost series not loaded) — never a fabricated number. Cost is
// conveyed by the "$" icon and the "Cost" label, not color alone. The honesty
// subline points at the config requirement (showHint) or the mixed-provenance
// caveat.
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
  const known = hasCost && usd !== undefined;
  return (
    <div className="rounded-lg bg-surface-elevated/60 border border-border/30 p-3">
      <span className="text-[10px] text-text-muted uppercase tracking-wider flex items-center gap-1 mb-1">
        <DollarSign size={10} className="text-text-muted/70" />
        Cost <span className="text-text-muted/50 normal-case tracking-normal">· est.</span>
      </span>
      <span className={cn('text-lg font-bold tabular-nums', known ? 'text-emerald-400' : 'text-text-muted')}>
        {usd !== undefined && hasCost ? formatUSD(usd) : '—'}
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

// The full KPI grid, identical across surfaces. The cards are window-scoped —
// summed from the same ranged series the charts draw, labeled by
// `windowLabel` — so the range control owns every headline number. The
// cumulative session totals render once, on their own explicitly labeled
// line, and never inside the window chrome. Format savings is
// session-cumulative (no windowed series exists), so it lives on the session
// line too. Cost pricing state (hasCost / hints) stays session-derived: a
// priced stack with an idle window honestly shows $0.00 for the window.
export function MetricsKpiRow({
  kpis,
  windowTotals,
  windowLabel,
  focusLine,
}: {
  kpis: SessionKpis;
  windowTotals: WindowTotals;
  windowLabel: string;
  // Rendered between the window cards and the session line while an entity is
  // focused, so the focused charts below never contradict an unqualified
  // fleet number above them.
  focusLine?: string;
}) {
  const sessionParts = [
    `Session total: ${kpis.total.toLocaleString()} tokens`,
    ...(kpis.hasCost ? [`${formatUSD(kpis.costUSD ?? 0)} est.`] : []),
    ...(kpis.savingsPercent > 0
      ? [`${Math.round(kpis.savingsPercent)}% format savings (${formatCompactNumber(kpis.savedTokens)} saved)`]
      : []),
  ];
  return (
    <div className="space-y-1.5">
      <span className="block text-[10px] uppercase tracking-[0.18em] text-text-muted/70">{windowLabel}</span>
      <div className="grid gap-3 grid-cols-4">
        <KPICard label="Input Tokens" value={windowTotals.input} colorClass="text-secondary" />
        <KPICard label="Output Tokens" value={windowTotals.output} colorClass="text-primary" />
        <KPICard label="Total Tokens" value={windowTotals.total} colorClass="text-text-primary" />
        <CostKPICard
          usd={windowTotals.costUSD}
          hasCost={kpis.hasCost}
          showHint={kpis.showAttributionHint}
          showMixedNote={kpis.hasMixedProvenance}
        />
      </div>
      {focusLine && <p className="text-[10px] font-mono text-text-secondary">{focusLine}</p>}
      <p className="text-[10px] font-mono text-text-muted">{sessionParts.join(' · ')}</p>
    </div>
  );
}

// The honest idle-window note, shared by the workspace and the detached
// window. Rendered only once the ranged series has actually loaded — an
// unresolved fetch must never be presented as "no activity".
export function WindowEmptyNote({
  windowTotals,
  sessionTotal,
  loaded,
}: {
  windowTotals: WindowTotals;
  sessionTotal: number;
  loaded: boolean;
}) {
  if (!loaded || !windowTotals.isEmpty || sessionTotal === 0) return null;
  return (
    <p className="text-[11px] text-text-muted/70">
      No activity in this window. The session line above covers the full recorded history.
    </p>
  );
}

// ---------------------------------------------------------------------------
// Charts (with screen-reader text alternatives)
// ---------------------------------------------------------------------------

// Chart rows are open records: the fleet charts carry the fixed token/cost
// keys, and the focused variants add a fleet-context category.
type ChartRow = { time: string } & Record<string, number | string>;

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

// Peak per interval for the aria summary: stacked charts peak on the category
// sum, overlapping (focused) charts on the largest single category. Dashed
// context categories are excluded — the summary describes the subject, and
// the fleet total would otherwise be announced as the entity's peak.
function peakFor(
  data: ChartRow[],
  categories: string[],
  stacked: boolean,
  dashedCategories?: string[],
): number {
  const primary = categories.filter((c) => !dashedCategories?.includes(c));
  return data.reduce((m, d) => {
    const values = primary.map((c) => Number(d[c]) || 0);
    return Math.max(m, stacked ? values.reduce((s, v) => s + v, 0) : Math.max(...values, 0));
  }, 0);
}

export function TokenChart({
  data,
  metricsData,
  heightClass = 'h-36',
  subject,
  categories = ['Input Tokens', 'Output Tokens'],
  colors = ['teal', 'amber'],
  chartType = 'stacked',
  dashedCategories,
}: {
  data: ChartRow[];
  metricsData: TokenMetricsResponse | null;
  heightClass?: string;
  // Focused-entity variant: `subject` names the entity (deriving both the
  // visible title and the aria summary so they cannot drift), categories add
  // the fleet-context series (dashed), and the chart drops stacking so the
  // context line overlaps instead of stacking on top.
  subject?: string;
  categories?: string[];
  colors?: AvailableChartColorsKeys[];
  chartType?: 'stacked' | 'default';
  dashedCategories?: string[];
}) {
  const title = subject ? `${subject} · Token Usage` : 'Token Usage Over Time';
  const peak = peakFor(data, categories, chartType === 'stacked', dashedCategories);
  const summary = `Token usage over time${subject ? ` for ${subject}` : ''}: ${data.length} points, peak ${formatCompactNumber(peak)} tokens per interval.`;
  return (
    <div className="rounded-lg bg-surface-elevated/60 border border-border/30 p-3">
      <div className="flex items-center justify-between mb-1">
        <span className="text-[11px] font-medium text-text-secondary">{title}</span>
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
          categories={categories}
          colors={colors}
          type={chartType}
          dashedCategories={dashedCategories}
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
  subject,
  categories = ['Cost (USD)'],
  colors = ['emerald'],
  dashedCategories,
}: {
  data: ChartRow[];
  costData: CostMetricsResponse | null;
  heightClass?: string;
  // Focused-entity variant, mirroring TokenChart's props (cost is never
  // stacked, so there is no chartType here).
  subject?: string;
  categories?: string[];
  colors?: AvailableChartColorsKeys[];
  dashedCategories?: string[];
}) {
  const title = subject ? `${subject} · Cost` : 'Cost Over Time';
  const peak = peakFor(data, categories, false, dashedCategories);
  const summary = `Estimated cost over time${subject ? ` for ${subject}` : ''}: ${data.length} points, peak ${formatUSD(peak)} per interval.`;
  return (
    <div className="rounded-lg bg-surface-elevated/60 border border-border/30 p-3">
      <div className="flex items-center justify-between mb-1">
        <span className="text-[11px] font-medium text-text-secondary inline-flex items-center gap-1.5">
          <DollarSign size={11} className="text-emerald-400" />
          {title}
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
          categories={categories}
          colors={colors}
          type="default"
          dashedCategories={dashedCategories}
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

// Small header-slot link for preview panels ("Top Servers" and friends):
// jumps to the full scope view via the host's setScope.
export function ViewAllButton({ onClick }: { onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="text-[10px] font-medium text-primary hover:underline"
    >
      View all
    </button>
  );
}

function entityCellLabel(entities: string[]): string {
  if (entities.length <= 2) return entities.join(', ');
  return `${entities.slice(0, 2).join(', ')} +${entities.length - 2}`;
}

function provenanceCellLabel(provenance: Record<ModelProvenance, number>): string {
  const parts = (Object.entries(provenance) as Array<[ModelProvenance, number]>)
    .filter(([, count]) => count > 0)
    .map(([kind, count]) => `${count} ${kind}`);
  return parts.join(' · ') || '—';
}

// The Models scope breakdown: one row per model with its cost share, the
// entities it priced, and their provenance mix. Static cost-descending
// (aggregateModelRows sorts) — a model has nothing to inspect in the right
// rail yet, so rows are not selectable in v1. Distinct from BreakdownTable,
// whose columns are hardwired to token counts.
export function ModelBreakdownTable({ rows }: { rows: ModelRow[] }) {
  return (
    <table className="w-full text-xs">
      <thead>
        <tr className="border-b border-border/30">
          <th className="px-3 py-2 text-left font-medium text-text-muted">Model</th>
          <th className="px-3 py-2 text-right font-medium text-text-muted">Share</th>
          <th className="px-3 py-2 text-right font-medium text-text-muted">Cost</th>
          <th className="px-3 py-2 text-left font-medium text-text-muted">Entities</th>
          <th className="px-3 py-2 text-left font-medium text-text-muted">Provenance</th>
        </tr>
      </thead>
      <tbody>
        {rows.map((row) => (
          <tr key={row.model} className="border-b border-border/20 last:border-0">
            <td className="px-3 py-2 font-mono text-text-primary" title={row.model}>
              {row.model}
            </td>
            <td className="px-3 py-2 text-right tabular-nums text-text-secondary">{sharePct(row.share)}</td>
            <td className="px-3 py-2 text-right tabular-nums text-emerald-400">{formatUSD(row.cost_usd)}</td>
            <td className="px-3 py-2 font-mono text-[10px] text-text-secondary" title={row.entities.join(', ')}>
              {entityCellLabel(row.entities)}
            </td>
            <td className="px-3 py-2 text-[10px] text-text-muted">{provenanceCellLabel(row.provenance)}</td>
          </tr>
        ))}
      </tbody>
    </table>
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
  // Absent for fixed-order tables (the Overview previews): the header renders
  // as plain text instead of a dead, focusable, sort-announcing control.
  onSort?: (column: BreakdownSortColumn) => void;
  align?: 'left' | 'right';
}) {
  if (!onSort) {
    return (
      <th className={cn('px-3 py-2 font-medium text-text-muted select-none', align === 'right' && 'text-right')}>
        {label}
      </th>
    );
  }
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
// `renderNameExtra` renders below the name cell's text (the limits overlay
// mounts its consumption bar there); returning null/undefined adds nothing.
export function BreakdownTable({
  rows,
  nameLabel,
  sortColumn,
  sortDirection,
  onSort,
  renderModel,
  renderNameExtra,
  showCost = false,
  selectedName,
  onSelectRow,
}: {
  rows: BreakdownRow[];
  nameLabel: string;
  sortColumn: BreakdownSortColumn;
  sortDirection: SortDirection;
  onSort?: (column: BreakdownSortColumn) => void;
  renderModel?: (row: BreakdownRow) => ReactNode;
  renderNameExtra?: (row: BreakdownRow) => ReactNode;
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
                {renderNameExtra?.(row)}
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
