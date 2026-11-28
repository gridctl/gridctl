import { useMemo, useState } from 'react';
import { BarChart3, Users, Server, Wrench } from 'lucide-react';
import { ErrorBoundary } from '../components/ui/ErrorBoundary';
import { FailedState, FailureBanner } from '../components/ui/EmptyState';
import { DetachedWindowFrame } from '../components/detached/DetachedWindowFrame';
import { refreshPhase } from '../lib/refreshPhase';
import { useSharedStatus } from '../hooks/useSharedStatus';
import { formatCompactNumber } from '../lib/format';
import { apiRangeFor, useMetricsSeries, windowLabelFor, type MetricsTimeRange } from '../hooks/useMetricsSeries';
import { useToolUsage } from '../hooks/useToolUsage';
import { useLimits } from '../hooks/useLimits';
import { LimitsPanel } from '../components/metrics/LimitsShared';
import { deriveLimitsSummary } from '../components/metrics/limitsData';
import { MetricsControls } from '../components/metrics/MetricsControls';
import {
  MetricsKpiRow,
  TokenChart,
  PanelHeader,
  BreakdownTable,
  ExportCsvButton,
  ScrollableBreakdown,
  WindowColumnCaption,
  WindowEmptyNote,
} from '../components/metrics/metricsShared';
import {
  buildTokenChartData,
  derivePerServerRows,
  derivePerClientRows,
  derivePerToolRows,
  deriveSessionKpis,
  deriveWindowTotals,
  hasMetricsData,
  sortBreakdownRows,
  SERVER_COLUMNS,
  TOOL_COLUMNS,
  type BreakdownSortColumn,
  type SortDirection,
} from '../components/metrics/metricsData';
import type { TokenUsage } from '../types';
import { useAuthStore } from '../stores/useAuthStore';

function DetachedMetricsPageContent() {
  const generation = useAuthStore(s => s.generation);
  const authRequired = useAuthStore(s => s.authRequired);
  // Real-time status snapshot — the detached window lives outside the app
  // shell, so it owns its own status poll (the in-shell surfaces read the app
  // store instead). The time-series come from the shared useMetricsSeries hook.
  const { status, settled, error: statusError, refresh: refreshStatus } = useSharedStatus({ enabled: !authRequired });
  const tokenUsage = useMemo<TokenUsage | null>(() => {
    if (!status || useAuthStore.getState().generation !== generation) return null;
    return status.token_usage ?? null;
  }, [status, generation]);
  const serverNames = useMemo(() => {
    if (!status || useAuthStore.getState().generation !== generation) return [];
    return (status['mcp-servers'] ?? []).map((s) => s.name);
  }, [status, generation]);
  const [timeRange, setTimeRange] = useState<MetricsTimeRange>('live');
  const [isPaused, setIsPaused] = useState(false);
  const [sortColumn, setSortColumn] = useState<BreakdownSortColumn>('total');
  const [sortDirection, setSortDirection] = useState<SortDirection>('desc');
  const [clientSortColumn, setClientSortColumn] = useState<BreakdownSortColumn>('total');
  const [clientSortDirection, setClientSortDirection] = useState<SortDirection>('desc');
  const [toolSortColumn, setToolSortColumn] = useState<BreakdownSortColumn>('total');
  const [toolSortDirection, setToolSortDirection] = useState<SortDirection>('desc');

  // Per-tool usage for the Per-Tool panel — the same GET /api/tools/usage
  // pipeline the Tools workspace and Metrics Tools scope consume.
  const { usage: toolUsageData, error: toolUsageError } = useToolUsage(true);

  const { metricsData, error, lastLoadedAt, reload, clear } = useMetricsSeries({
    timeRange,
    paused: isPaused,
  });
  const phase = refreshPhase({ error, hasLoaded: metricsData !== null });
  const showContent = phase === 'ready' || phase === 'stale';

  const handleSort = (column: BreakdownSortColumn) => {
    if (sortColumn === column) setSortDirection((d) => (d === 'asc' ? 'desc' : 'asc'));
    else {
      setSortColumn(column);
      setSortDirection('desc');
    }
  };

  const handleClientSort = (column: BreakdownSortColumn) => {
    if (clientSortColumn === column) setClientSortDirection((d) => (d === 'asc' ? 'desc' : 'asc'));
    else {
      setClientSortColumn(column);
      setClientSortDirection('desc');
    }
  };

  const handleToolSort = (column: BreakdownSortColumn) => {
    if (toolSortColumn === column) setToolSortDirection((d) => (d === 'asc' ? 'desc' : 'asc'));
    else {
      setToolSortColumn(column);
      setToolSortDirection('desc');
    }
  };

  const kpis = deriveSessionKpis(tokenUsage);
  const sortedServers = sortBreakdownRows(
    derivePerServerRows(tokenUsage, { serverNames, toolUsage: toolUsageData, metricsData }),
    sortColumn,
    sortDirection,
  );
  const sortedClients = sortBreakdownRows(derivePerClientRows(tokenUsage), clientSortColumn, clientSortDirection);
  const sortedTools = sortBreakdownRows(derivePerToolRows(toolUsageData), toolSortColumn, toolSortDirection);

  // Rate-limit overlay — same source and presentation as the in-shell
  // workspace (parity is the point of the shared core).
  const { report: limitsReport } = useLimits(true);
  const limitsSummary = deriveLimitsSummary(limitsReport);

  const chartData = buildTokenChartData(metricsData);
  const hasData = hasMetricsData(kpis, metricsData);
  // Same windowed-KPI presentation as the in-shell workspace; the detached
  // window keeps its range local (solo window, nothing to deep-link).
  const windowTotals = deriveWindowTotals(metricsData);
  const windowLabel = windowLabelFor(timeRange);

  // The metrics error block already reports an outage; the banner would repeat it.
  const lost = (statusError != null || (settled && status == null)) && error == null;

  return (
    <DetachedWindowFrame
      windowType="metrics"
      fullscreen
      header={{
        icon: (
          <div className="p-1.5 rounded-lg border bg-primary/10 border-primary/20">
            <BarChart3 size={14} className="text-primary" />
          </div>
        ),
        start: <span className="text-sm font-semibold text-text-primary">Metrics</span>,
        actions: (
          <MetricsControls
            timeRange={timeRange}
            onTimeRange={(r) => {
              setTimeRange(r);
              if (r === 'live') setIsPaused(false);
            }}
            isPaused={isPaused}
            onTogglePause={() => setIsPaused((p) => !p)}
            onRefresh={reload}
            onClear={() => void clear()}
          />
        ),
      }}
      connection={{ lost, message: statusError ?? undefined, onRetry: () => void refreshStatus() }}
      footerStart={
        <>
          {kpis.total > 0 ? `Session total: ${formatCompactNumber(kpis.total)} tokens` : 'No data'}
          {isPaused ? ' (paused)' : ''}
        </>
      }
    >
      <main className="flex-1 overflow-auto bg-background scrollbar-dark min-h-0 p-4">
        {phase === 'loading' && (
          <div className="@container space-y-4 animate-pulse">
            <div className="grid grid-cols-2 @2xl:grid-cols-4 gap-3">
              {[1, 2, 3, 4].map((i) => (
                <div key={i} className="h-16 rounded-lg bg-surface-elevated/60 border border-border/30" />
              ))}
            </div>
            <div className="h-48 rounded-lg bg-surface-elevated/60 border border-border/30" />
          </div>
        )}

        {phase === 'failed' && (
          <FailedState feature="Metrics" message={error ?? undefined} onRetry={reload} />
        )}

        {phase === 'stale' && error && (
          <FailureBanner
            message={error}
            onRetry={reload}
            lastUpdated={lastLoadedAt}
            className="mx-0 mt-0 mb-4"
          />
        )}

        {showContent && !hasData && (
          <div className="flex flex-col items-center justify-center h-full text-text-muted gap-2">
            <BarChart3 size={32} className="text-text-muted" />
            <span className="text-sm">No token data yet</span>
            <span className="text-xs text-text-muted">Metrics will appear after tool calls</span>
          </div>
        )}

        {showContent && hasData && (
          <div className="space-y-4">
            <MetricsKpiRow kpis={kpis} windowTotals={windowTotals} windowLabel={windowLabel} />
            <TokenChart data={chartData} metricsData={metricsData} heightClass="h-48" />
            <WindowEmptyNote windowTotals={windowTotals} sessionTotal={kpis.total} loaded={metricsData !== null} />


            <LimitsPanel summary={limitsSummary} />

            {/* Tables are session totals (same labeling as the in-shell
                workspace), except the Servers window column, which the caption
                under that table names. Parity decision: the workspace's
                findings card and top-5 previews are deliberately absent here —
                the previews would duplicate the full tables below, and the
                finding links navigate the /metrics URL scheme, which this solo
                window does not host. */}
            {sortedClients.length > 0 && (
              <PanelHeader
                icon={Users}
                label="Top Clients · session totals"
                right={<ExportCsvButton rows={sortedClients} kind="clients" />}
              >
                <BreakdownTable
                  rows={sortedClients}
                  nameLabel="Client"
                  sortColumn={clientSortColumn}
                  sortDirection={clientSortDirection}
                  onSort={handleClientSort}
                />
              </PanelHeader>
            )}

            {sortedServers.length > 0 && (
              <PanelHeader
                icon={Server}
                label="Per-Server · session totals"
                right={<ExportCsvButton rows={sortedServers} kind="servers" window={apiRangeFor(timeRange)} />}
              >
                <div className="overflow-x-auto scrollbar-dark">
                  <BreakdownTable
                    rows={sortedServers}
                    nameLabel="Server"
                    sortColumn={sortColumn}
                    sortDirection={sortDirection}
                    onSort={handleSort}
                    columns={SERVER_COLUMNS}
                    windowLabel={windowLabel}
                  />
                </div>
                <WindowColumnCaption windowLabel={windowLabel} />
              </PanelHeader>
            )}

            {(sortedTools.length > 0 || toolUsageError) && (
              <PanelHeader
                icon={Wrench}
                label="Per-Tool · session totals"
                right={<ExportCsvButton rows={sortedTools} kind="tools" />}
              >
                {/* A failed fetch is not "no usage" — say the source is
                    unavailable instead of silently dropping the panel. A
                    retained snapshot keeps rendering (stale beats blank). */}
                {toolUsageError && sortedTools.length === 0 ? (
                  <p role="alert" className="text-2xs text-status-error px-1 py-2">
                    Tool usage unavailable: {toolUsageError}
                  </p>
                ) : (
                  <>
                    {toolUsageError && (
                      <p role="alert" className="text-2xs text-status-error px-1 pb-2">
                        Usage refresh failed: {toolUsageError} — showing the last loaded
                        snapshot
                      </p>
                    )}
                    <ScrollableBreakdown>
                      <BreakdownTable
                        rows={sortedTools}
                        nameLabel="Tool"
                        sortColumn={toolSortColumn}
                        sortDirection={toolSortDirection}
                        onSort={handleToolSort}
                        columns={TOOL_COLUMNS}
                      />
                    </ScrollableBreakdown>
                  </>
                )}
              </PanelHeader>
            )}
          </div>
        )}
      </main>
    </DetachedWindowFrame>
  );
}

export function DetachedMetricsPage() {
  return (
    <ErrorBoundary variant="window">
      <DetachedMetricsPageContent />
    </ErrorBoundary>
  );
}
