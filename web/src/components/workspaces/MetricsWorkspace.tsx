import { useCallback, useId, useMemo, useRef, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router';
import { BarChart3, Server, Users, Wrench, X } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { cn } from '../../lib/cn';
import { GATEWAY_NODE_ID } from '../../lib/graph';
import { WORKSPACE_LABELS } from '../../types/workspace';
import { useUIStore } from '../../stores/useUIStore';
import { useStackStore } from '../../stores/useStackStore';
import { useWindowManager } from '../../hooks/useWindowManager';
import { useListNav } from '../../hooks/useListNav';
import { useToolUsage } from '../../hooks/useToolUsage';
import { useHeldValue } from '../../hooks/useHeldValue';
import { usePollingStatusRefresh } from '../../hooks/usePolling';
import { useLimits } from '../../hooks/useLimits';
import { useOptimize } from '../../hooks/useOptimize';
import {
  useMetricsSeries,
  normalizeMetricsTimeRangeParam,
  apiRangeFor,
  windowLabelFor,
  type MetricsTimeRange,
} from '../../hooks/useMetricsSeries';
import { WorkspaceShell } from '../layout/WorkspaceShell';
import { EmptyState, FailedState, FailureBanner } from '../ui/EmptyState';
import { refreshPhase } from '../../lib/refreshPhase';
import { PopoutButton } from '../ui/PopoutButton';
import { WorkspaceHeader } from '../ui/WorkspaceHeader';
import { PersistedFromMarker } from '../telemetry/PersistedFromMarker';
import { MetricsControls } from '../metrics/MetricsControls';
import { MetricsInspector } from '../metrics/MetricsInspector';
import { SavingsCard } from '../metrics/SavingsCard';
import { ToolsFilterBar } from '../metrics/ToolsFilterBar';
import {
  MetricsKpiRow,
  TokenChart,
  PanelHeader,
  BreakdownTable,
  ScrollableBreakdown,
  ExportCsvButton,
  ViewAllButton,
  WindowColumnCaption,
  WindowEmptyNote,
} from '../metrics/metricsShared';
import { LimitsPanel } from '../metrics/LimitsShared';
import { deriveLimitsSummary } from '../metrics/limitsData';
import {
  buildTokenChartData,
  buildFocusedTokenChartData,
  deriveFocusedTotals,
  derivePerServerRows,
  derivePerClientRows,
  derivePerToolRows,
  deriveSessionKpis,
  deriveWindowTotals,
  findingTarget,
  hasMetricsData,
  sortBreakdownRows,
  FLEET_TOKEN_CATEGORY,
  SERVER_COLUMNS,
  TOOL_COLUMNS,
  type BreakdownRow,
  type BreakdownSortColumn,
  type SortDirection,
} from '../metrics/metricsData';
import type { OptimizeFinding } from '../../types';

type Scope = 'overview' | 'clients' | 'servers' | 'tools';
const SCOPES: Scope[] = ['overview', 'clients', 'servers', 'tools'];

function isScope(v: string | null): v is Scope {
  return v != null && (SCOPES as string[]).includes(v);
}

// The tools filters are one concept spread over two params; every writer
// that clears them goes through here so a future facet is added once.
function clearToolParams(params: URLSearchParams): void {
  params.delete('q');
  params.delete('server');
  // Legacy param from the removed priced/unpriced facet: unread, but dropped
  // here so pre-removal bookmarks do not carry it forever.
  params.delete('priced');
}

// MetricsWorkspace is the first-class token/usage observability surface,
// sibling to Stack, Library, Variables, and Tools. The left rail is a scope
// navigator (overview / clients / servers / tools); the center carries the
// window-scoped KPI row, the token trend chart, and the active scope's
// breakdown; the right rail inspects the selected client, server, or tool.
// Overview doubles as a home: optimize findings plus top-5 server/tool
// previews that jump into their scopes. Selecting a server refocuses the
// center chart on that entity with the fleet as dashed context. Scope,
// selection, time range (?range=), and the tools filters (?q= / ?server=)
// are URL-synced so reload and deep links survive. The dashboard body is
// shared with the detached window via metricsShared.
export function MetricsWorkspace() {
  const headingId = useId();
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const compact = useUIStore((s) => s.compactMode.metrics);
  const metricsDetached = useUIStore((s) => s.metricsDetached);

  const liveTokenUsage = useStackStore((s) => s.tokenUsage);
  const mcpServers = useStackStore((s) => s.mcpServers);

  const { openDetachedWindow, focusDetachedWindow } = useWindowManager();

  // ---- URL state ----------------------------------------------------------
  const scope: Scope = isScope(searchParams.get('scope')) ? (searchParams.get('scope') as Scope) : 'overview';
  const selected = searchParams.get('selected');
  // Range mirrors ?range= (absent = live) so reload, share, and deep links
  // restore the window like scope/selection. Pause stays local on purpose (a
  // shared link must not arrive frozen), matching the Logs workspace.
  const timeRange = normalizeMetricsTimeRangeParam(searchParams.get('range'));
  // Tools-scope filters, Library idiom: omitted at their defaults so bare
  // links stay canonical.
  const toolQuery = searchParams.get('q') ?? '';
  const toolServerFacet = searchParams.get('server');

  const setScope = useCallback(
    (next: Scope) => {
      setSearchParams(
        (prev) => {
          const params = new URLSearchParams(prev);
          if (next === 'overview') params.delete('scope');
          else params.set('scope', next);
          // Selection and the tools filters are scope-local — drop them when
          // the axis changes.
          params.delete('selected');
          clearToolParams(params);
          return params;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  const setSelected = useCallback(
    (name: string | null) => {
      setSearchParams(
        (prev) => {
          const params = new URLSearchParams(prev);
          if (name) params.set('selected', name);
          else params.delete('selected');
          return params;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  const setTimeRange = useCallback(
    (next: MetricsTimeRange) => {
      setSearchParams(
        (prev) => {
          const params = new URLSearchParams(prev);
          // Live is the default — drop the param so bare links stay canonical.
          if (next === 'live') params.delete('range');
          else params.set('range', next);
          return params;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  // Tools-filter writers. Each is one composed setSearchParams call —
  // sequential calls would read the same stale snapshot and clobber each
  // other (see the ToolsWorkspace note on composed updates).
  const setToolQuery = useCallback(
    (q: string) => {
      setSearchParams(
        (prev) => {
          const params = new URLSearchParams(prev);
          if (q) params.set('q', q);
          else params.delete('q');
          return params;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  const setToolServerFacet = useCallback(
    (server: string | null) => {
      setSearchParams(
        (prev) => {
          const params = new URLSearchParams(prev);
          if (server) params.set('server', server);
          else params.delete('server');
          return params;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  const clearToolFilters = useCallback(() => {
    setSearchParams(
      (prev) => {
        const params = new URLSearchParams(prev);
        clearToolParams(params);
        return params;
      },
      { replace: true },
    );
  }, [setSearchParams]);

  // Jump from an Overview preview row (or an optimize finding) into a scope
  // with the row selected — one composed update so scope, selection, and the
  // cleared tools filters land atomically.
  const openInScope = useCallback(
    (next: Scope, name: string) => {
      setSearchParams(
        (prev) => {
          const params = new URLSearchParams(prev);
          params.set('scope', next);
          params.set('selected', name);
          clearToolParams(params);
          return params;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  // Optimize deep links resolve through findingTarget, the same predicate
  // SavingsCard uses to decide whether a finding's title is a button, so a
  // clickable title always has somewhere to land. Filters are cleared so the
  // target row is never hidden by an active facet.
  const openFinding = useCallback(
    (finding: OptimizeFinding) => {
      const target = findingTarget(finding);
      if (target) openInScope(target.scope, target.selected);
    },
    [openInScope],
  );

  const openAllFindings = useCallback(() => {
    useStackStore.getState().selectNode(GATEWAY_NODE_ID);
    useUIStore.getState().setSidebarOpen(true);
    navigate('/stack');
  }, [navigate]);

  const [isPaused, setIsPaused] = useState(false);
  const [serverSort, setServerSort] = useState<{ col: BreakdownSortColumn; dir: SortDirection }>({ col: 'total', dir: 'desc' });
  const [clientSort, setClientSort] = useState<{ col: BreakdownSortColumn; dir: SortDirection }>({ col: 'total', dir: 'desc' });
  const [toolSort, setToolSort] = useState<{ col: BreakdownSortColumn; dir: SortDirection }>({ col: 'total', dir: 'desc' });
  // Polite announcement for range changes, read by screen readers.
  const [liveMsg, setLiveMsg] = useState('');
  // Pause holds the snapshot-fed tables with the chart. Only Live is held:
  // isPaused survives a switch to another range, which shows no Pause control.
  const holdTables = isPaused && timeRange === 'live';
  const [tablesEpoch, setTablesEpoch] = useState(0);
  const refreshStatus = usePollingStatusRefresh();

  const { metricsData, error, lastLoadedAt, reload, clear } = useMetricsSeries({
    timeRange,
    paused: isPaused,
  });
  const phase = refreshPhase({ error, hasLoaded: metricsData !== null });
  const showContent = phase === 'ready' || phase === 'stale';

  // Per-tool usage powers the Tools scope (and its rail count badge), so the
  // poll runs whenever the workspace is mounted — same 15s cadence Audit Mode
  // uses, against the same single per-tool data source.
  const { usage: liveToolUsage, error: toolUsageError, reload: reloadToolUsage } = useToolUsage(true);
  const tokenUsage = useHeldValue(liveTokenUsage, holdTables, tablesEpoch);
  const toolUsageData = useHeldValue(liveToolUsage, holdTables, tablesEpoch);

  // Rate-limit state feeds the Limits panel. A stack without a limits: block
  // reports configured: false and renders nothing anywhere.
  const { report: limitsReport } = useLimits(true);
  const limitsSummary = useMemo(() => deriveLimitsSummary(limitsReport), [limitsReport]);

  // Optimize findings feed the Overview card (same hook and cadence as the
  // gateway sidebar section).
  const { report: optimizeReport } = useOptimize(true);

  // ---- Derived data -------------------------------------------------------
  const kpis = deriveSessionKpis(tokenUsage);
  const windowTotals = useMemo(() => deriveWindowTotals(metricsData), [metricsData]);
  const windowLabel = windowLabelFor(timeRange);
  const chartData = useMemo(() => buildTokenChartData(metricsData), [metricsData]);
  const hasData = hasMetricsData(kpis, metricsData);

  // Server rows union in stack servers with no recorded traffic (zero rows)
  // so an unused server — the optimize report's headline finding — is
  // selectable in the breakdown rather than absent from it.
  const serverNames = useMemo(() => mcpServers.map((s) => s.name), [mcpServers]);
  const serverRows = useMemo(
    () =>
      sortBreakdownRows(
        derivePerServerRows(tokenUsage, { serverNames, toolUsage: toolUsageData, metricsData }),
        serverSort.col,
        serverSort.dir,
      ),
    [tokenUsage, serverNames, toolUsageData, metricsData, serverSort],
  );
  const clientRows = useMemo(
    () => sortBreakdownRows(derivePerClientRows(tokenUsage), clientSort.col, clientSort.dir),
    [tokenUsage, clientSort],
  );
  const toolRows = useMemo(
    () => sortBreakdownRows(derivePerToolRows(toolUsageData), toolSort.col, toolSort.dir),
    [toolUsageData, toolSort],
  );

  // Overview previews: top entities by total tokens, hard-capped at five
  // rows. Re-sorts of the scope rows (sortBreakdownRows copies) — one base
  // derivation per axis.
  const topServerRows = useMemo(
    () => sortBreakdownRows(serverRows, 'total', 'desc').slice(0, 5),
    [serverRows],
  );
  const topToolRows = useMemo(
    () => sortBreakdownRows(toolRows, 'total', 'desc').slice(0, 5),
    [toolRows],
  );

  // Tools-scope filtering: search over tool and server names plus the server
  // facet. The filtered array is what renders AND what drives keyboard nav,
  // so the two can never disagree.
  const toolServers = useMemo(
    () => Array.from(new Set(toolRows.map((r) => r.server).filter((s): s is string => Boolean(s)))).sort(),
    [toolRows],
  );
  const filteredToolRows = useMemo(() => {
    const q = toolQuery.trim().toLowerCase();
    return toolRows.filter((r) => {
      if (toolServerFacet && r.server !== toolServerFacet) return false;
      if (q) {
        // r.name is the composite server__tool key — matching it too means a
        // pasted deep-link value finds its row.
        const tool = (r.tool ?? r.name).toLowerCase();
        const server = (r.server ?? '').toLowerCase();
        if (!tool.includes(q) && !server.includes(q) && !r.name.toLowerCase().includes(q)) return false;
      }
      return true;
    });
  }, [toolRows, toolQuery, toolServerFacet]);

  // Rows for the active selectable scope (clients/servers/tools), used by the
  // inspector lookup and keyboard navigation. Tools uses the FILTERED rows so
  // arrows never step through hidden entries; a ?selected row excluded by a
  // filter yields findIndex -1, so the next ArrowDown lands on row 0 and
  // ArrowUp on the last row (useListNav's no-selection rule).
  const activeRows: BreakdownRow[] = useMemo(
    () =>
      scope === 'servers' ? serverRows : scope === 'clients' ? clientRows : scope === 'tools' ? filteredToolRows : [],
    [scope, serverRows, clientRows, filteredToolRows],
  );
  const selectedRow = useMemo(
    () => activeRows.find((r) => r.name === selected) ?? null,
    [activeRows, selected],
  );

  // Keyboard nav over the active breakdown (clients/servers/tools).
  const selectedIndex = useMemo(
    () => activeRows.findIndex((r) => r.name === selected),
    [activeRows, selected],
  );
  const toolSearchRef = useRef<HTMLInputElement | null>(null);
  useListNav({
    itemCount: activeRows.length,
    selectedIndex,
    setSelectedIndex: (i) => {
      const next = activeRows[i];
      if (next) setSelected(next.name);
    },
    // '/' focuses the tools filter (the Logs precedent); typing in the input
    // is already exempt from j/k via isEditableTarget.
    onSlash: scope === 'tools' ? () => toolSearchRef.current?.focus() : undefined,
    enabled: scope === 'clients' || scope === 'servers' || scope === 'tools',
  });

  const sortServers = (col: BreakdownSortColumn) =>
    setServerSort((s) => (s.col === col ? { col, dir: s.dir === 'asc' ? 'desc' : 'asc' } : { col, dir: 'desc' }));
  const sortClients = (col: BreakdownSortColumn) =>
    setClientSort((s) => (s.col === col ? { col, dir: s.dir === 'asc' ? 'desc' : 'asc' } : { col, dir: 'desc' }));
  const sortTools = (col: BreakdownSortColumn) =>
    setToolSort((s) => (s.col === col ? { col, dir: s.dir === 'asc' ? 'desc' : 'asc' } : { col, dir: 'desc' }));

  // ---- Inspector wiring ---------------------------------------------------
  const inspectorScope = scope === 'servers' ? 'servers' : scope === 'tools' ? 'tools' : 'clients';
  const inspectorTokenPoints =
    selectedRow && scope === 'servers' ? metricsData?.per_server?.[selectedRow.name] : undefined;

  // ---- Focused center chart -----------------------------------------------
  // Selecting a server refocuses the main chart on that entity, with the
  // fleet series as dashed context. The entity data is the same per-entity
  // ranged series the inspector sparkline consumes. An entirely empty entity
  // series keeps the fleet chart + an honest note instead of a flat zero
  // line (see the zero-fill note in metricsData: zeros are only real when the
  // entity has at least one bucket in the window).
  const focusedName = selectedRow && scope === 'servers' ? selectedRow.name : null;
  const focusHasTokens = focusedName !== null && (inspectorTokenPoints?.length ?? 0) > 0;
  const focusedTokenChartData = useMemo(
    () => (focusHasTokens && inspectorTokenPoints ? buildFocusedTokenChartData(metricsData, inspectorTokenPoints) : []),
    [focusHasTokens, inspectorTokenPoints, metricsData],
  );
  const focusedTotals = useMemo(
    () => (focusedName ? deriveFocusedTotals(inspectorTokenPoints, windowTotals) : null),
    [focusedName, inspectorTokenPoints, windowTotals],
  );
  // Each term renders only when measurable — "0 tokens" for a client (no
  // per-client token series exists) would contradict the honesty notes
  // beside the chart. With nothing measurable the line is suppressed.
  const focusLineParts =
    focusedName && focusedTotals
      ? [
          ...(focusedTotals.tokens !== undefined ? [`${focusedTotals.tokens.toLocaleString()} tokens`] : []),
          ...(focusedTotals.share !== undefined
            ? [`${Math.round(focusedTotals.share * 100)}% of window`]
            : []),
        ]
      : [];
  const focusLine = focusLineParts.length > 0 ? `${focusedName}: ${focusLineParts.join(' · ')}` : undefined;

  // Honest note whenever a selection cannot focus the chart, so fleet data is
  // never silently presented as the entity's.
  let focusNote: string | null = null;
  if (selectedRow && scope === 'tools') {
    focusNote = 'Charts show the whole stack; per-tool time series is not recorded yet.';
  } else if (focusedName && scope === 'servers' && !focusHasTokens) {
    focusNote = `No samples for ${focusedName} in this window. The chart shows the whole stack as context.`;
  } else if (selectedRow && scope === 'clients') {
    focusNote = 'The chart shows the whole stack; per-client token series is not recorded.';
  }

  const inspectorRow = scope === 'clients' || scope === 'servers' || scope === 'tools' ? selectedRow : null;
  const inspectorRowKey = inspectorRow ? `${scope}:${inspectorRow.name}` : null;

  const inspector = (
    <MetricsInspector
      scope={inspectorScope}
      row={inspectorRow}
      onClose={() => setSelected(null)}
      tokenPoints={inspectorTokenPoints}
      range={metricsData?.range}
      interval={metricsData?.interval}
    />
  );

  const leftRail = (
    <ScopeRail
      compact={compact}
      scope={scope}
      onSelectScope={setScope}
      clientCount={clientRows.length}
      serverCount={serverRows.length}
      toolCount={toolRows.length}
    />
  );

  const onTimeRange = (r: MetricsTimeRange) => {
    setTimeRange(r);
    if (r === 'live') setIsPaused(false);
    setLiveMsg(`Showing ${r === 'live' ? 'live' : r} metrics`);
  };

  const onTogglePause = () => {
    // The series poll does not fire when re-enabled, so Resume fetches now to
    // keep the chart in step with the released tables.
    if (isPaused) void reload();
    setIsPaused(!isPaused);
  };

  const onRefresh = async () => {
    const [ok] = await Promise.all([reload(), refreshStatus(), reloadToolUsage()]);
    setTablesEpoch((n) => n + 1);
    return ok;
  };

  const onClear = async () => {
    await clear(() => Promise.all([refreshStatus(), reloadToolUsage()]));
    setTablesEpoch((n) => n + 1);
  };

  return (
    <div className="absolute inset-0 flex flex-col bg-background text-text-primary overflow-hidden">
      <span className="sr-only" role="status" aria-live="polite">{liveMsg}</span>
      <WorkspaceShell
        workspace="metrics"
        defaultLeftPct={20}
        defaultRightPct={30}
        left={leftRail}
        right={inspector}
        leftKey={scope}
        rightKey={inspectorRowKey}
        minLeftPx={200}
        minRightPx={300}
      >
        <section aria-labelledby={headingId} className="flex flex-col h-full overflow-hidden">
          <WorkspaceHeader
            title={WORKSPACE_LABELS.metrics}
            headingId={headingId}
            compact={compact}
            summary={(
              <span className="font-mono">
                {kpis.total > 0 || windowTotals.total > 0
                  ? `${windowLabel} · ${windowTotals.total.toLocaleString()} tokens`
                  : 'no traffic yet'}
              </span>
            )}
            actions={(
              <MetricsControls
                timeRange={timeRange}
                onTimeRange={onTimeRange}
                isPaused={isPaused}
                onTogglePause={onTogglePause}
                onRefresh={onRefresh}
                onClear={onClear}
                right={(
                  <PopoutButton
                    onClick={() => (metricsDetached ? focusDetachedWindow('metrics') : openDetachedWindow('metrics'))}
                    detached={metricsDetached}
                  />
                )}
              />
            )}
          />

          <div className="flex-1 min-h-0 overflow-y-auto scrollbar-dark px-6 py-4 flex flex-col">
            {/* Mutually exclusive states by refresh phase: failed, loading,
                then empty or data (with a banner when stale). The headline
                numbers are window-scoped, so the skeleton holds until the
                ranged series lands (or a range switch resolves); rendering
                zeros from an unresolved fetch would present "loading" as
                "no activity". */}
            {phase === 'failed' && (
              <FailedState feature="Metrics" message={error ?? undefined} onRetry={reload} className="flex-1" />
            )}

            {phase === 'loading' && <LoadingState />}

            {phase === 'stale' && error && (
              <FailureBanner
                message={error}
                onRetry={reload}
                lastUpdated={lastLoadedAt}
                className="mx-0 mt-0 mb-4 max-w-7xl flex-shrink-0"
              />
            )}

            {showContent && !hasData && (
              <EmptyState
                icon={BarChart3}
                tone="primary"
                title="Your metrics home"
                description="Metrics populate as tools are called: token usage per server, client, and tool, plus call activity and format-conversion savings."
                className="flex-1"
              />
            )}

            {showContent && hasData && (
              <div className="space-y-4 max-w-7xl">
                <PersistedFromMarker serverName={null} signal="metrics" />
                <MetricsKpiRow kpis={kpis} windowTotals={windowTotals} windowLabel={windowLabel} focusLine={focusLine} />
                {focusedName && focusHasTokens && (
                  <button
                    type="button"
                    onClick={() => setSelected(null)}
                    aria-label={`Clear focus on ${focusedName}`}
                    className="inline-flex items-center gap-1.5 rounded-full border border-primary/30 bg-primary/10 px-2.5 py-0.5 text-3xs font-medium text-primary hover:bg-primary/20 transition-colors"
                  >
                    Focused: {focusedName} <X size={10} aria-hidden="true" />
                  </button>
                )}
                {focusedName && focusHasTokens ? (
                  <TokenChart
                    data={focusedTokenChartData}
                    metricsData={metricsData}
                    subject={focusedName}
                    categories={['Input Tokens', 'Output Tokens', FLEET_TOKEN_CATEGORY]}
                    colors={['teal', 'amber', 'gray']}
                    chartType="default"
                    dashedCategories={[FLEET_TOKEN_CATEGORY]}
                  />
                ) : (
                  <TokenChart data={chartData} metricsData={metricsData} />
                )}
                {focusNote && <p className="text-2xs text-text-muted">{focusNote}</p>}
                <WindowEmptyNote windowTotals={windowTotals} sessionTotal={kpis.total} loaded={metricsData !== null} />


                {scope === 'overview' && <LimitsPanel summary={limitsSummary} />}

                {scope === 'overview' && (
                  <SavingsCard
                    report={optimizeReport}
                    onOpenFinding={openFinding}
                    onOpenAllFindings={openAllFindings}
                  />
                )}

                {/* Top-5 previews jump into the full scope on row click or
                    View all. Hard-capped at five rows and session-scoped like
                    every breakdown table. */}
                {scope === 'overview' && topServerRows.length > 0 && (
                  <PanelHeader
                    icon={Server}
                    label="Top Servers · session totals"
                    right={<ViewAllButton onClick={() => setScope('servers')} />}
                  >
                    <BreakdownTable
                      rows={topServerRows}
                      nameLabel="Server"
                      sortColumn="total"
                      sortDirection="desc"
                      onSelectRow={(name) => openInScope('servers', name)}
                    />
                  </PanelHeader>
                )}

                {scope === 'overview' && topToolRows.length > 0 && (
                  <PanelHeader
                    icon={Wrench}
                    label="Top Tools · session totals"
                    right={<ViewAllButton onClick={() => setScope('tools')} />}
                  >
                    <BreakdownTable
                      rows={topToolRows}
                      nameLabel="Tool"
                      sortColumn="total"
                      sortDirection="desc"
                      onSelectRow={(name) => openInScope('tools', name)}
                    />
                  </PanelHeader>
                )}

                {/* Breakdown tables are session totals. The one exception is
                    the Servers window column, fed by the ranged per-server
                    series and named by the caption under that table. */}
                {scope === 'tools' && (
                  <PanelHeader
                    icon={Wrench}
                    label="Per-Tool · session totals"
                    right={<ExportCsvButton rows={filteredToolRows} kind="tools" />}
                  >
                    {toolRows.length > 0 ? (
                      <>
                        <ToolsFilterBar
                          query={toolQuery}
                          onQuery={setToolQuery}
                          servers={toolServers}
                          activeServer={toolServerFacet}
                          onServer={setToolServerFacet}
                          onClearAll={clearToolFilters}
                          matchCount={filteredToolRows.length}
                          totalCount={toolRows.length}
                          searchInputRef={toolSearchRef}
                        />
                        {filteredToolRows.length > 0 ? (
                          <ScrollableBreakdown>
                            <BreakdownTable
                              rows={filteredToolRows}
                              nameLabel="Tool"
                              sortColumn={toolSort.col}
                              sortDirection={toolSort.dir}
                              onSort={sortTools}
                              selectedName={selected}
                              onSelectRow={setSelected}
                              columns={TOOL_COLUMNS}
                            />
                          </ScrollableBreakdown>
                        ) : (
                          // Filtered-empty is not "no usage" — the stack has
                          // tool rows, the filters just exclude them all.
                          <div className="px-3 py-3 flex items-center gap-2 text-2xs text-text-muted">
                            No tools match the current filters.
                            <button type="button" onClick={clearToolFilters} className="text-primary hover:underline">
                              Clear filters
                            </button>
                          </div>
                        )}
                      </>
                    ) : toolUsageError ? (
                      // A failed fetch is not "no usage" — say the data source
                      // is unavailable instead of implying calls went unrecorded.
                      <EmptyScopeNote text={`Tool usage unavailable: ${toolUsageError}`} />
                    ) : (
                      <EmptyScopeNote text="No per-tool usage recorded yet. Tool rows appear after the first tool call." />
                    )}
                  </PanelHeader>
                )}

                {scope === 'clients' && (
                  <PanelHeader
                    icon={Users}
                    label="Top Clients · session totals"
                    right={<ExportCsvButton rows={clientRows} kind="clients" />}
                  >
                    {clientRows.length > 0 ? (
                      <BreakdownTable
                        rows={clientRows}
                        nameLabel="Client"
                        sortColumn={clientSort.col}
                        sortDirection={clientSort.dir}
                        onSort={sortClients}
                        selectedName={selected}
                        onSelectRow={setSelected}
                      />
                    ) : (
                      <EmptyScopeNote text="No per-client attribution yet. Calls carry a client identity once an MCP client connects." />
                    )}
                  </PanelHeader>
                )}

                {scope === 'servers' && (
                  <PanelHeader
                    icon={Server}
                    label="Per-Server · session totals"
                    right={<ExportCsvButton rows={serverRows} kind="servers" window={apiRangeFor(timeRange)} />}
                  >
                    {serverRows.length > 0 ? (
                      <>
                        <div className="overflow-x-auto scrollbar-dark">
                          <BreakdownTable
                            rows={serverRows}
                            nameLabel="Server"
                            sortColumn={serverSort.col}
                            sortDirection={serverSort.dir}
                            onSort={sortServers}
                            selectedName={selected}
                            onSelectRow={setSelected}
                            columns={SERVER_COLUMNS}
                            windowLabel={windowLabel}
                          />
                        </div>
                        <WindowColumnCaption windowLabel={windowLabel} />
                      </>
                    ) : (
                      <EmptyScopeNote text="No per-server traffic recorded yet." />
                    )}
                  </PanelHeader>
                )}
              </div>
            )}
          </div>
        </section>
      </WorkspaceShell>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Left rail — scope navigator
// ---------------------------------------------------------------------------

interface ScopeRailProps {
  compact: boolean;
  scope: Scope;
  onSelectScope: (s: Scope) => void;
  clientCount: number;
  serverCount: number;
  toolCount: number;
}

function ScopeRail({ compact, scope, onSelectScope, clientCount, serverCount, toolCount }: ScopeRailProps) {
  return (
    <aside aria-label="Metrics rail" className="h-full flex flex-col bg-surface border-r border-border-subtle">
      <div className={cn('flex-shrink-0 px-3 border-b border-border-subtle/60', compact ? 'py-2' : 'py-3')}>
        <div className="text-3xs font-medium text-text-muted uppercase tracking-[0.3em]">breakdown</div>
      </div>
      <div className="flex-1 min-h-0 overflow-y-auto scrollbar-dark px-2 py-2 space-y-0.5">
        <ScopePill label="Overview" icon={BarChart3} active={scope === 'overview'} onClick={() => onSelectScope('overview')} />
        <ScopePill label="Clients" icon={Users} count={clientCount} active={scope === 'clients'} onClick={() => onSelectScope('clients')} />
        <ScopePill label="Servers" icon={Server} count={serverCount} active={scope === 'servers'} onClick={() => onSelectScope('servers')} />
        <ScopePill label="Tools" icon={Wrench} count={toolCount} active={scope === 'tools'} onClick={() => onSelectScope('tools')} />
      </div>
    </aside>
  );
}

function ScopePill({
  label,
  icon: Icon,
  count,
  active,
  onClick,
}: {
  label: string;
  icon: LucideIcon;
  count?: number;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <button
      onClick={onClick}
      aria-current={active}
      className={cn(
        'group w-full flex items-center gap-2 px-3 py-1.5 rounded-md text-left transition-colors',
        active ? 'bg-primary/10 text-primary' : 'text-text-secondary hover:bg-surface-highlight/50 hover:text-text-primary',
      )}
    >
      <Icon size={13} className={active ? 'text-primary' : 'text-text-muted'} aria-hidden="true" />
      <span className={cn('flex-1 min-w-0 text-xs font-medium truncate', active && 'text-primary')}>{label}</span>
      {count !== undefined && (
        <span
          className={cn(
            'flex-shrink-0 text-3xs font-mono px-1.5 py-0.5 rounded tabular-nums',
            active ? 'bg-primary/15 text-primary' : 'bg-surface-elevated text-text-muted',
          )}
        >
          {count}
        </span>
      )}
    </button>
  );
}

// ---------------------------------------------------------------------------
// States
// ---------------------------------------------------------------------------

function EmptyScopeNote({ text }: { text: string }) {
  return <p className="px-3 py-3 text-2xs text-text-muted leading-relaxed">{text}</p>;
}

function LoadingState() {
  return (
    <div className="@container space-y-4 max-w-7xl animate-pulse">
      <div className="grid grid-cols-2 @2xl:grid-cols-4 gap-3">
        {[1, 2, 3, 4].map((i) => (
          <div key={i} className="h-16 rounded-lg bg-surface-elevated/60 border border-border/30" />
        ))}
      </div>
      <div className="h-44 rounded-lg bg-surface-elevated/60 border border-border/30" />
    </div>
  );
}

export default MetricsWorkspace;
