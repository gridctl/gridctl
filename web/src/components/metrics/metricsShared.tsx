import { type ComponentType, type ReactNode } from 'react';
import { ArrowDown, ArrowUp, ArrowUpDown, Recycle } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { cn } from '../../lib/cn';
import { formatCompactNumber } from '../../lib/format';
import { AreaChart } from '../chart/AreaChart';
import type { AvailableChartColorsKeys } from '../chart/chartColors';
import type { TokenMetricsResponse } from '../../types';
import type {
  BreakdownRow,
  BreakdownSortColumn,
  SessionKpis,
  SortDirection,
  WindowTotals,
} from './metricsData';

// Presentational atoms shared by every metrics surface (Metrics workspace,
// detached window). Pure data helpers and types live in metricsData.ts — this
// file is components only so Fast Refresh stays happy.

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

// SavingsKPICard — tokens saved by the gateway's output-format conversion
// (TOON/CSV), measured from its own before/after counts. Session-cumulative
// (no windowed savings series exists), so the label says so explicitly to
// stand apart from the window-scoped token cards beside it. Renders an
// em-dash until a conversion has actually saved something — never a
// fabricated number.
export function SavingsKPICard({ savingsPercent, savedTokens }: { savingsPercent: number; savedTokens: number }) {
  const hasSavings = savingsPercent > 0;
  return (
    <div className="rounded-lg bg-surface-elevated/60 border border-border/30 p-3">
      <span className="text-[10px] text-text-muted uppercase tracking-wider flex items-center gap-1 mb-1">
        <Recycle size={10} className="text-text-muted/70" />
        Format Savings <span className="text-text-muted/50 normal-case tracking-normal">· session</span>
      </span>
      <span className={cn('text-lg font-bold tabular-nums', hasSavings ? 'text-emerald-400' : 'text-text-muted')}>
        {hasSavings ? `${Math.round(savingsPercent)}%` : '—'}
      </span>
      {hasSavings && (
        <span className="block mt-1 text-[9px] leading-snug text-text-muted/60">
          {formatCompactNumber(savedTokens)} tokens saved by output-format conversion
        </span>
      )}
    </div>
  );
}

// The full KPI grid, identical across surfaces. The token cards are
// window-scoped — summed from the same ranged series the charts draw, labeled
// by `windowLabel` — so the range control owns every headline number. The
// cumulative session total renders once, on its own explicitly labeled line,
// and never inside the window chrome.
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
  return (
    <div className="space-y-1.5">
      <span className="block text-[10px] uppercase tracking-[0.18em] text-text-muted/70">{windowLabel}</span>
      <div className="grid gap-3 grid-cols-4">
        <KPICard label="Input Tokens" value={windowTotals.input} colorClass="text-secondary" />
        <KPICard label="Output Tokens" value={windowTotals.output} colorClass="text-primary" />
        <KPICard label="Total Tokens" value={windowTotals.total} colorClass="text-text-primary" />
        <SavingsKPICard savingsPercent={kpis.savingsPercent} savedTokens={kpis.savedTokens} />
      </div>
      {focusLine && <p className="text-[10px] font-mono text-text-secondary">{focusLine}</p>}
      <p className="text-[10px] font-mono text-text-muted">
        Session total: {kpis.total.toLocaleString()} tokens
      </p>
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

// Chart rows are open records: the fleet charts carry the fixed token keys,
// and the focused variants add a fleet-context category.
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

// BreakdownTable renders the shared client/server breakdown. Hosts may make
// rows selectable (`onSelectRow` + `selectedName`) to drive a detail
// inspector. `renderNameExtra` renders below the name cell's text (the limits
// overlay mounts its consumption annotation there); returning null/undefined
// adds nothing.
export function BreakdownTable({
  rows,
  nameLabel,
  sortColumn,
  sortDirection,
  onSort,
  renderNameExtra,
  selectedName,
  onSelectRow,
}: {
  rows: BreakdownRow[];
  nameLabel: string;
  sortColumn: BreakdownSortColumn;
  sortDirection: SortDirection;
  onSort?: (column: BreakdownSortColumn) => void;
  renderNameExtra?: (row: BreakdownRow) => ReactNode;
  selectedName?: string | null;
  onSelectRow?: (name: string) => void;
}) {
  return (
    <table className="w-full text-xs">
      <thead>
        <tr className="border-b border-border/30">
          <SortableHeader label={nameLabel} column="name" sortColumn={sortColumn} sortDirection={sortDirection} onSort={onSort} />
          <SortableHeader label="Input" column="input" sortColumn={sortColumn} sortDirection={sortDirection} onSort={onSort} align="right" />
          <SortableHeader label="Output" column="output" sortColumn={sortColumn} sortDirection={sortDirection} onSort={onSort} align="right" />
          <SortableHeader label="Total" column="total" sortColumn={sortColumn} sortDirection={sortDirection} onSort={onSort} align="right" />
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
              // interactive elements must keep their native behavior.
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
              <td className="px-3 py-2 text-right text-secondary tabular-nums">{formatCompactNumber(row.input)}</td>
              <td className="px-3 py-2 text-right text-primary tabular-nums">{formatCompactNumber(row.output)}</td>
              <td className="px-3 py-2 text-right text-text-primary font-semibold tabular-nums">{formatCompactNumber(row.total)}</td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}
