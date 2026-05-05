import { useEffect, useRef, useState, useCallback, useMemo } from 'react';
import {
  Pause,
  Play,
  Trash2,
  BarChart3,
  ArrowUpDown,
  ArrowUp,
  ArrowDown,
  AlertCircle,
  RefreshCw,
} from 'lucide-react';
import { cn } from '../../lib/cn';
import { IconButton } from '../ui/IconButton';
import { PopoutButton } from '../ui/PopoutButton';
import { useUIStore } from '../../stores/useUIStore';
import { useStackStore } from '../../stores/useStackStore';
import { fetchTokenMetrics, clearTokenMetrics } from '../../lib/api';
import { useWindowManager } from '../../hooks/useWindowManager';
import { formatCompactNumber } from '../../lib/format';
import { POLLING } from '../../lib/constants';
import { AreaChart } from '../chart/AreaChart';
import { PersistedFromMarker } from '../telemetry/PersistedFromMarker';
import type { TokenMetricsResponse } from '../../types';

type TimeRange = 'live' | '1h' | '6h' | '24h' | '7d';
type SortColumn = 'name' | 'input' | 'output' | 'total';
type SortDirection = 'asc' | 'desc';

const TIME_RANGES: { value: TimeRange; label: string }[] = [
  { value: 'live', label: 'Live' },
  { value: '1h', label: '1h' },
  { value: '6h', label: '6h' },
  { value: '24h', label: '24h' },
  { value: '7d', label: '7d' },
];

export function MetricsTab() {
  const bottomPanelOpen = useUIStore((s) => s.bottomPanelOpen);
  const bottomPanelTab = useUIStore((s) => s.bottomPanelTab);
  const tokenUsage = useStackStore((s) => s.tokenUsage);
  const metricsDetached = useUIStore((s) => s.metricsDetached);
  const { openDetachedWindow } = useWindowManager();
  const [timeRange, setTimeRange] = useState<TimeRange>('live');
  const [isPaused, setIsPaused] = useState(false);
  const [metricsData, setMetricsData] = useState<TokenMetricsResponse | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showClearConfirm, setShowClearConfirm] = useState(false);
  const [sortColumn, setSortColumn] = useState<SortColumn>('total');
  const [sortDirection, setSortDirection] = useState<SortDirection>('desc');

  const intervalRef = useRef<number | null>(null);
  const isVisible = bottomPanelOpen && bottomPanelTab === 'metrics';

  // Map time range to API range param
  const apiRange = timeRange === 'live' ? '30m' : timeRange;

  const loadMetrics = useCallback(async () => {
    try {
      const data = await fetchTokenMetrics(apiRange);
      setMetricsData(data);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch metrics');
    } finally {
      setIsLoading(false);
    }
  }, [apiRange]);

  // Fetch on mount and when range changes
  useEffect(() => {
    if (!isVisible) return;
    setIsLoading(true);
    loadMetrics();
  }, [isVisible, apiRange, loadMetrics]);

  // Auto-refresh in live mode
  useEffect(() => {
    if (!isVisible || isPaused || timeRange !== 'live') {
      if (intervalRef.current) {
        clearInterval(intervalRef.current);
        intervalRef.current = null;
      }
      return;
    }

    intervalRef.current = window.setInterval(loadMetrics, POLLING.METRICS);

    return () => {
      if (intervalRef.current) {
        clearInterval(intervalRef.current);
        intervalRef.current = null;
      }
    };
  }, [isVisible, isPaused, timeRange, loadMetrics]);

  const handleClearMetrics = async () => {
    try {
      await clearTokenMetrics();
      setMetricsData(null);
      setShowClearConfirm(false);
      setIsLoading(true);
      loadMetrics();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to clear metrics');
    }
  };

  const handleSort = (column: SortColumn) => {
    if (sortColumn === column) {
      setSortDirection((d) => (d === 'asc' ? 'desc' : 'asc'));
    } else {
      setSortColumn(column);
      setSortDirection('desc');
    }
  };

  // Per-server data from the real-time status (for KPI + table)
  const perServerEntries = useMemo(() => {
    if (!tokenUsage?.per_server) return [];
    return Object.entries(tokenUsage.per_server).map(([name, counts]) => ({
      name,
      input: counts.input_tokens,
      output: counts.output_tokens,
      total: counts.total_tokens,
    }));
  }, [tokenUsage]);

  // Sort per-server entries
  const sortedServers = useMemo(() => {
    const sorted = [...perServerEntries];
    sorted.sort((a, b) => {
      const dir = sortDirection === 'asc' ? 1 : -1;
      if (sortColumn === 'name') return dir * a.name.localeCompare(b.name);
      return dir * (a[sortColumn] - b[sortColumn]);
    });
    return sorted;
  }, [perServerEntries, sortColumn, sortDirection]);

  // Session totals from real-time status
  const sessionInput = tokenUsage?.session.input_tokens ?? 0;
  const sessionOutput = tokenUsage?.session.output_tokens ?? 0;
  const sessionTotal = tokenUsage?.session.total_tokens ?? 0;
  const savingsPercent = tokenUsage?.format_savings.savings_percent ?? 0;
  const savedTokens = tokenUsage?.format_savings.saved_tokens ?? 0;

  const hasData = sessionTotal > 0 || (metricsData?.data_points?.length ?? 0) > 0;

  // Transform API data points to Recharts format
  const chartData = useMemo(() => {
    if (!metricsData?.data_points) return [];
    return metricsData.data_points.map((dp) => ({
      time: new Date(dp.timestamp).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
      "Input Tokens": dp.input_tokens,
      "Output Tokens": dp.output_tokens,
    }));
  }, [metricsData]);

  return (
    <div className="flex flex-col h-full">
      {/* Control bar */}
      <div className="flex items-center justify-between px-4 h-9 flex-shrink-0 border-b border-border/30 bg-surface-elevated/20">
        <div className="flex items-center gap-2">
          {/* Time range selector */}
          <div className="flex items-center bg-background/60 rounded-md border border-border/40 overflow-hidden">
            {TIME_RANGES.map((range) => (
              <button
                key={range.value}
                onClick={() => {
                  setTimeRange(range.value);
                  if (range.value === 'live') setIsPaused(false);
                }}
                className={cn(
                  'px-2.5 py-1 text-[10px] font-medium transition-colors',
                  timeRange === range.value
                    ? 'bg-primary/15 text-primary'
                    : 'text-text-muted hover:text-text-secondary hover:bg-surface-highlight/40'
                )}
              >
                {range.label}
              </button>
            ))}
          </div>

          {/* Live indicator */}
          {timeRange === 'live' && !isPaused && (
            <span className="flex items-center gap-1 text-[9px] text-status-running font-medium">
              <span className="w-1.5 h-1.5 rounded-full bg-status-running animate-pulse" />
              Live
            </span>
          )}
        </div>

        <div className="flex items-center gap-1">
          {timeRange === 'live' && (
            <IconButton
              icon={isPaused ? Play : Pause}
              onClick={() => setIsPaused(!isPaused)}
              tooltip={isPaused ? 'Resume live updates' : 'Pause live updates'}
              size="sm"
              variant="ghost"
              className={isPaused ? 'text-status-running hover:text-status-running' : ''}
            />
          )}
          <IconButton
            icon={RefreshCw}
            onClick={() => { setIsLoading(true); loadMetrics(); }}
            tooltip="Refresh"
            size="sm"
            variant="ghost"
          />

          {/* Clear metrics */}
          <div className="relative">
            <IconButton
              icon={Trash2}
              onClick={() => setShowClearConfirm(true)}
              tooltip="Clear Metrics"
              size="sm"
              variant="ghost"
              className="hover:text-status-error"
            />
            {showClearConfirm && (
              <div className="absolute right-0 top-full mt-1 z-50 p-3 rounded-lg bg-surface-elevated border border-border/50 shadow-xl min-w-[220px]">
                <p className="text-xs text-text-primary font-medium mb-2">Clear all token metrics?</p>
                <p className="text-[10px] text-text-muted mb-3">This cannot be undone.</p>
                <div className="flex items-center gap-2 justify-end">
                  <button
                    onClick={() => setShowClearConfirm(false)}
                    className="px-2.5 py-1 text-[10px] font-medium rounded-md bg-surface-highlight text-text-secondary hover:text-text-primary transition-colors"
                  >
                    Cancel
                  </button>
                  <button
                    onClick={handleClearMetrics}
                    className="px-2.5 py-1 text-[10px] font-medium rounded-md bg-status-error/15 text-status-error hover:bg-status-error/25 transition-colors"
                  >
                    Clear
                  </button>
                </div>
              </div>
            )}
          </div>

          <div className="w-px h-4 bg-border/50 mx-0.5" />
          <PopoutButton
            onClick={() => openDetachedWindow('metrics')}
            disabled={metricsDetached}
          />
        </div>
      </div>

      {/* Content */}
      <div className="flex-1 overflow-auto scrollbar-dark min-h-0 p-4">
        {/* Loading skeleton */}
        {isLoading && !metricsData && (
          <div className="space-y-4 animate-pulse">
            {/* KPI skeleton */}
            <div className="grid grid-cols-3 gap-3">
              {[1, 2, 3].map((i) => (
                <div key={i} className="h-16 rounded-lg bg-surface-elevated/60 border border-border/30" />
              ))}
            </div>
            {/* Chart skeleton */}
            <div className="h-40 rounded-lg bg-surface-elevated/60 border border-border/30" />
            {/* Table skeleton */}
            <div className="h-24 rounded-lg bg-surface-elevated/60 border border-border/30" />
          </div>
        )}

        {/* Error state */}
        {error && !isLoading && (
          <div className="flex flex-col items-center justify-center h-full gap-3">
            <AlertCircle size={24} className="text-status-error" />
            <span className="text-xs text-status-error">{error}</span>
            <button
              onClick={() => { setError(null); setIsLoading(true); loadMetrics(); }}
              className="text-xs text-primary hover:underline"
            >
              Retry
            </button>
          </div>
        )}

        {/* Empty state */}
        {!isLoading && !error && !hasData && (
          <div className="flex flex-col items-center justify-center h-full text-text-muted gap-2">
            <BarChart3 size={24} className="text-text-muted/30" />
            <span className="text-xs">No token data yet</span>
            <span className="text-[10px] text-text-muted/60">Metrics will appear after tool calls</span>
          </div>
        )}

        {/* Data view — shown even while loading if we have stale data to prevent flash */}
        {!error && hasData && (
          <div className="space-y-4">
            {/* Aggregate persisted-from boundary — only renders when at
                least one server has metrics persistence enabled and files
                exist on disk. Sits above the KPI cards so it reads as a
                provenance hint for the data below. */}
            <PersistedFromMarker serverName={null} signal="metrics" />

            {/* KPI Cards */}
            <div className={cn('grid gap-3', savingsPercent > 0 ? 'grid-cols-4' : 'grid-cols-3')}>
              <KPICard label="Input Tokens" value={sessionInput} colorClass="text-secondary" />
              <KPICard label="Output Tokens" value={sessionOutput} colorClass="text-primary" />
              <KPICard label="Total Tokens" value={sessionTotal} colorClass="text-text-primary" />
              {savingsPercent > 0 && (
                <div className="rounded-lg bg-surface-elevated/60 border border-border/30 p-3">
                  <span className="text-[10px] text-text-muted uppercase tracking-wider block mb-1">Format Savings</span>
                  <div className="flex items-baseline gap-2">
                    <span className="text-lg font-bold text-status-running tabular-nums">{Math.round(savingsPercent)}%</span>
                    <span className="text-[10px] text-text-muted">{formatCompactNumber(savedTokens)} saved</span>
                  </div>
                  {/* Savings bar */}
                  <div className="mt-2 h-1.5 rounded-full bg-surface-highlight overflow-hidden flex">
                    <div
                      className="h-full bg-primary rounded-full"
                      style={{ width: `${100 - savingsPercent}%` }}
                    />
                    <div
                      className="h-full bg-primary/20"
                      style={{ width: `${savingsPercent}%` }}
                    />
                  </div>
                </div>
              )}
            </div>

            {/* Area Chart */}
            <div className="rounded-lg bg-surface-elevated/60 border border-border/30 p-3">
              <div className="flex items-center justify-between mb-1">
                <span className="text-[11px] font-medium text-text-secondary">Token Usage Over Time</span>
                {metricsData && (
                  <span className="text-[9px] text-text-muted font-mono">
                    {metricsData.data_points?.length ?? 0} points &middot; {metricsData.interval} interval
                  </span>
                )}
              </div>
              <AreaChart
                data={chartData}
                index="time"
                categories={["Input Tokens", "Output Tokens"]}
                colors={["teal", "amber"]}
                type="stacked"
                fill="gradient"
                showLegend={true}
                showGridLines={true}
                showYAxis={true}
                yAxisWidth={48}
                valueFormatter={(v: number) => formatCompactNumber(v)}
                className="h-36"
              />
            </div>

            {/* Per-Server Breakdown Table */}
            {sortedServers.length > 0 && (
              <div className="rounded-lg bg-surface-elevated/60 border border-border/30 overflow-hidden">
                <table className="w-full text-xs">
                  <thead>
                    <tr className="border-b border-border/30">
                      <SortableHeader
                        label="Server"
                        column="name"
                        sortColumn={sortColumn}
                        sortDirection={sortDirection}
                        onSort={handleSort}
                      />
                      <SortableHeader
                        label="Input"
                        column="input"
                        sortColumn={sortColumn}
                        sortDirection={sortDirection}
                        onSort={handleSort}
                        align="right"
                      />
                      <SortableHeader
                        label="Output"
                        column="output"
                        sortColumn={sortColumn}
                        sortDirection={sortDirection}
                        onSort={handleSort}
                        align="right"
                      />
                      <SortableHeader
                        label="Total"
                        column="total"
                        sortColumn={sortColumn}
                        sortDirection={sortDirection}
                        onSort={handleSort}
                        align="right"
                      />
                    </tr>
                  </thead>
                  <tbody>
                    {sortedServers.map((server) => (
                      <tr key={server.name} className="border-b border-border/20 hover:bg-surface-highlight/30 transition-colors">
                        <td className="px-3 py-2 font-medium text-text-primary font-mono">{server.name}</td>
                        <td className="px-3 py-2 text-right text-secondary tabular-nums">{formatCompactNumber(server.input)}</td>
                        <td className="px-3 py-2 text-right text-primary tabular-nums">{formatCompactNumber(server.output)}</td>
                        <td className="px-3 py-2 text-right text-text-primary font-semibold tabular-nums">{formatCompactNumber(server.total)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        )}
      </div>

      {/* Click-away handler for clear confirm */}
      {showClearConfirm && (
        <div className="fixed inset-0 z-40" onClick={() => setShowClearConfirm(false)} />
      )}
    </div>
  );
}

// KPI Card component
function KPICard({ label, value, colorClass }: { label: string; value: number; colorClass: string }) {
  return (
    <div className="rounded-lg bg-surface-elevated/60 border border-border/30 p-3">
      <span className="text-[10px] text-text-muted uppercase tracking-wider block mb-1">{label}</span>
      <span className={cn('text-lg font-bold tabular-nums', colorClass)}>{formatCompactNumber(value)}</span>
    </div>
  );
}

// Sortable table header
function SortableHeader({
  label,
  column,
  sortColumn,
  sortDirection,
  onSort,
  align = 'left',
}: {
  label: string;
  column: SortColumn;
  sortColumn: SortColumn;
  sortDirection: SortDirection;
  onSort: (column: SortColumn) => void;
  align?: 'left' | 'right';
}) {
  const isActive = sortColumn === column;
  const SortIcon = isActive
    ? sortDirection === 'asc' ? ArrowUp : ArrowDown
    : ArrowUpDown;

  return (
    <th
      className={cn(
        'px-3 py-2 font-medium text-text-muted cursor-pointer hover:text-text-secondary transition-colors select-none',
        align === 'right' && 'text-right'
      )}
      tabIndex={0}
      role="columnheader"
      aria-sort={isActive ? (sortDirection === 'asc' ? 'ascending' : 'descending') : 'none'}
      onClick={() => onSort(column)}
      onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onSort(column); } }}
    >
      <span className="inline-flex items-center gap-1">
        {label}
        <SortIcon size={10} className={isActive ? 'text-primary' : 'text-text-muted/40'} />
      </span>
    </th>
  );
}
