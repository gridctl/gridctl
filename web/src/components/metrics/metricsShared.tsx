import { type ComponentType, type ReactNode, useEffect, useRef } from 'react';
import { ArrowDown, ArrowUp, ArrowUpDown, Download, Recycle } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { cn } from '../../lib/cn';
import { listNavProps, listNavRowProps } from '../../hooks/useListNav';
import { formatCompactNumber } from '../../lib/format';
import { formatLastUsed } from '../../lib/toolAudit';
import { downloadTextFile, metricsExportFilename } from '../../lib/download';
import { showToast } from '../ui/Toast';
import { AreaChart } from '../chart/AreaChart';
import type { AvailableChartColorsKeys } from '../chart/chartColors';
import type { TokenMetricsResponse } from '../../types';
import {
  avgTokensPerCall,
  breakdownRowsToCsv,
  SERVER_AVG_NOTE,
  TOKEN_COLUMNS,
  windowColumnCaption,
  windowColumnLabel,
} from './metricsData';
import type {
  BreakdownColumn,
  BreakdownExportKind,
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
      <span className="text-3xs text-text-muted uppercase tracking-wider block mb-1">{label}</span>
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
      <span className="text-3xs text-text-muted uppercase tracking-wider flex items-center gap-1 mb-1">
        <Recycle size={10} className="text-text-muted" />
        Format Savings <span className="text-text-muted normal-case tracking-normal">· session</span>
      </span>
      <span className={cn('text-lg font-bold tabular-nums', hasSavings ? 'text-status-running' : 'text-text-muted')}>
        {hasSavings ? `${Math.round(savingsPercent)}%` : '—'}
      </span>
      {hasSavings && (
        <span className="block mt-1 text-3xs leading-snug text-text-muted">
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
    <div className="@container space-y-1.5">
      <span className="block text-3xs uppercase tracking-[0.18em] text-text-muted">{windowLabel}</span>
      <div className="grid gap-3 grid-cols-2 @2xl:grid-cols-4">
        <KPICard label="Input Tokens" value={windowTotals.input} colorClass="text-secondary" />
        <KPICard label="Output Tokens" value={windowTotals.output} colorClass="text-primary" />
        <KPICard label="Total Tokens" value={windowTotals.total} colorClass="text-text-primary" />
        <SavingsKPICard savingsPercent={kpis.savingsPercent} savedTokens={kpis.savedTokens} />
      </div>
      {focusLine && <p className="text-3xs font-mono text-text-secondary">{focusLine}</p>}
      <p className="text-3xs font-mono text-text-muted">
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
    <p className="text-2xs text-text-muted">
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
        <span className="text-2xs font-medium text-text-secondary">{title}</span>
        {metricsData && (
          <span className="text-3xs text-text-muted font-mono">
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
        <span className="text-2xs font-medium text-text-secondary">{label}</span>
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
      className="text-3xs font-medium text-primary hover:underline"
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
  padClass = 'px-3',
  title,
}: {
  label: string;
  column: BreakdownSortColumn;
  sortColumn: BreakdownSortColumn;
  sortDirection: SortDirection;
  // Absent for fixed-order tables (the Overview previews): the header renders
  // as plain text instead of a dead, focusable, sort-announcing control.
  onSort?: (column: BreakdownSortColumn) => void;
  align?: 'left' | 'right';
  padClass?: string;
  // Optional note on what the column means; also exposed as the header's
  // accessible description.
  title?: string;
}) {
  if (!onSort) {
    return (
      <th
        className={cn(padClass, 'py-2 font-medium text-text-muted select-none whitespace-nowrap', align === 'right' && 'text-right')}
        title={title}
        aria-description={title}
      >
        {label}
      </th>
    );
  }
  const isActive = sortColumn === column;
  const SortIcon = isActive ? (sortDirection === 'asc' ? ArrowUp : ArrowDown) : ArrowUpDown;
  return (
    <th
      className={cn(
        padClass,
        'py-2 font-medium text-text-muted cursor-pointer hover:text-text-secondary transition-colors select-none whitespace-nowrap',
        align === 'right' && 'text-right',
      )}
      title={title}
      aria-description={title}
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
        <SortIcon size={10} className={isActive ? 'text-primary' : 'text-text-muted'} />
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
    <div className="max-h-[60vh] overflow-y-auto overflow-x-auto scrollbar-dark [&_thead_th]:sticky [&_thead_th]:top-0 [&_thead_th]:z-sticky [&_thead_th]:bg-surface">
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
      <span className="text-3xs text-text-muted uppercase tracking-wider block mb-0.5">{label}</span>
      <span className={cn('text-sm font-bold tabular-nums', className)}>{value}</span>
    </div>
  );
}

// Dash placeholder for a value whose source is absent; the hidden phrase
// keeps the meaning available to assistive tech.
function Placeholder({ label }: { label: string }) {
  return (
    <span title={label}>
      <span aria-hidden="true">–</span>
      <span className="sr-only">{label}</span>
    </span>
  );
}

const COLUMN_HEADERS: Record<Exclude<BreakdownColumn, 'window'>, string> = {
  calls: 'Calls',
  input: 'Input',
  output: 'Output',
  total: 'Total',
  avg: 'Avg/call',
  lastUsed: 'Last used',
};

const CELL_TEXT_CLASS: Record<BreakdownColumn, string> = {
  calls: 'text-text-primary',
  window: 'text-text-primary',
  input: 'text-secondary',
  output: 'text-primary',
  total: 'text-text-primary font-semibold',
  avg: 'text-text-secondary',
  lastUsed: 'text-text-secondary whitespace-nowrap',
};

function BreakdownCell({ row, column, cellPad }: { row: BreakdownRow; column: BreakdownColumn; cellPad: string }) {
  let content: ReactNode;
  let title: string | undefined;
  switch (column) {
    case 'calls':
      content = row.calls === undefined ? <Placeholder label="calls unavailable" /> : formatCompactNumber(row.calls);
      break;
    case 'window':
      content =
        row.windowTotal === undefined ? <Placeholder label="window not loaded" /> : formatCompactNumber(row.windowTotal);
      break;
    case 'avg': {
      const avg = avgTokensPerCall(row);
      content = avg === undefined ? <Placeholder label="no recorded calls" /> : formatCompactNumber(avg);
      break;
    }
    case 'lastUsed': {
      const t = row.lastUsed ? Date.parse(row.lastUsed) : NaN;
      if (Number.isNaN(t)) {
        content = <Placeholder label="no recorded calls" />;
      } else {
        content = formatLastUsed(row.lastUsed);
        title = new Date(t).toLocaleString();
      }
      break;
    }
    default:
      content = formatCompactNumber(row[column]);
  }
  return (
    <td className={cn(cellPad, 'py-2 text-right tabular-nums', CELL_TEXT_CLASS[column])} title={title}>
      {content}
    </td>
  );
}

function BreakdownTableRow({
  row,
  columns,
  cellPad,
  selected,
  onSelectRow,
  renderNameExtra,
}: {
  row: BreakdownRow;
  columns: BreakdownColumn[];
  cellPad: string;
  selected: boolean;
  onSelectRow?: (name: string) => void;
  renderNameExtra?: (row: BreakdownRow) => ReactNode;
}) {
  const ref = useRef<HTMLTableRowElement>(null);
  const selectable = onSelectRow !== undefined;

  // Keep the selected row visible when selection moves. Keyed on the
  // selection only: rows re-render on every poll and must not re-scroll.
  useEffect(() => {
    if (selectable && selected) ref.current?.scrollIntoView?.({ block: 'nearest' });
  }, [selectable, selected]);

  return (
    <tr
      ref={ref}
      aria-selected={onSelectRow ? selected : undefined}
      {...(onSelectRow ? listNavRowProps : {})}
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
        // The scroll margin clears the sticky header when a row is scrolled
        // into view from below it.
        'border-b border-border/20 last:border-0 transition-colors scroll-mt-9',
        onSelectRow && 'cursor-pointer focus-visible:-outline-offset-2',
        selected ? 'bg-primary/[0.07]' : 'hover:bg-surface-highlight/30',
      )}
    >
      <td className={cn(cellPad, 'py-2 font-medium text-text-primary font-mono')}>
        {row.server && row.tool ? (
          <>
            <span className="text-text-muted">{row.server}</span>
            <span className="text-text-muted" aria-hidden="true">{' › '}</span>
            {row.tool}
          </>
        ) : (
          row.name
        )}
        {renderNameExtra?.(row)}
      </td>
      {columns.map((column) => (
        <BreakdownCell key={column} row={row} column={column} cellPad={cellPad} />
      ))}
    </tr>
  );
}

// BreakdownTable renders the shared client/server/tool breakdown. `columns`
// picks the non-name columns (default: tokens only); `windowLabel` names the
// window column when it is shown. Hosts may make rows selectable
// (`onSelectRow` + `selectedName`) to drive a detail inspector.
// `renderNameExtra` renders below the name cell's text (the limits overlay
// mounts its consumption annotation there); returning null/undefined adds
// nothing.
export function BreakdownTable({
  rows,
  nameLabel,
  sortColumn,
  sortDirection,
  onSort,
  renderNameExtra,
  selectedName,
  onSelectRow,
  columns = TOKEN_COLUMNS,
  windowLabel,
}: {
  rows: BreakdownRow[];
  nameLabel: string;
  sortColumn: BreakdownSortColumn;
  sortDirection: SortDirection;
  onSort?: (column: BreakdownSortColumn) => void;
  renderNameExtra?: (row: BreakdownRow) => ReactNode;
  selectedName?: string | null;
  onSelectRow?: (name: string) => void;
  columns?: BreakdownColumn[];
  windowLabel?: string;
}) {
  // Wide column sets tighten the padding so the Servers table fits the
  // center pane; the wrapper scrolls horizontally below that.
  const cellPad = columns.length > TOKEN_COLUMNS.length ? 'px-2' : 'px-3';
  // The window column only appears in the Servers set, whose average comes
  // from a different source than its Total and so carries a note.
  const avgTitle = columns.includes('window') ? SERVER_AVG_NOTE : undefined;
  const sortProps = { sortColumn, sortDirection, onSort, padClass: cellPad };
  return (
    <table className="w-full text-xs">
      <thead>
        <tr className="border-b border-border/30">
          <SortableHeader label={nameLabel} column="name" {...sortProps} />
          {columns.map((column) => (
            <SortableHeader
              key={column}
              label={column === 'window' ? windowColumnLabel(windowLabel ?? 'Window') : COLUMN_HEADERS[column]}
              column={column}
              title={column === 'avg' ? avgTitle : undefined}
              align="right"
              {...sortProps}
            />
          ))}
        </tr>
      </thead>
      <tbody {...(onSelectRow ? listNavProps : {})}>
        {rows.map((row) => (
          <BreakdownTableRow
            key={row.name}
            row={row}
            columns={columns}
            cellPad={cellPad}
            selected={selectedName === row.name}
            onSelectRow={onSelectRow}
            renderNameExtra={renderNameExtra}
          />
        ))}
      </tbody>
    </table>
  );
}

// Caption under the Servers table naming the one range-scoped column.
export function WindowColumnCaption({ windowLabel }: { windowLabel: string }) {
  return (
    <p className="px-3 py-1.5 text-2xs text-text-muted border-t border-border/20">
      {windowColumnCaption(windowLabel)}
    </p>
  );
}

// Header-slot export for a breakdown panel. `rows` must be the array the
// table renders, so the file matches what the operator sees.
export function ExportCsvButton({
  rows,
  kind,
  window,
}: {
  rows: BreakdownRow[];
  kind: BreakdownExportKind;
  window?: string;
}) {
  const disabled = rows.length === 0;
  return (
    <button
      type="button"
      disabled={disabled}
      onClick={() => {
        if (rows.length === 0) return;
        downloadTextFile(breakdownRowsToCsv(rows, kind, { window }), metricsExportFilename(kind), 'text/csv;charset=utf-8');
        showToast('success', `Exported ${rows.length} ${rows.length === 1 ? 'row' : 'rows'} as CSV`);
      }}
      className="inline-flex items-center gap-1 text-3xs font-medium text-primary hover:underline disabled:text-text-muted disabled:no-underline disabled:cursor-not-allowed"
    >
      <Download size={10} aria-hidden="true" />
      Export CSV
    </button>
  );
}
