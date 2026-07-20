import { useCallback, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { BarChart3, Boxes, Layers, Server, Users, Wrench } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { cn } from '../../lib/cn';
import { useUIStore } from '../../stores/useUIStore';
import { useStackStore } from '../../stores/useStackStore';
import { useWindowManager } from '../../hooks/useWindowManager';
import { useListNav } from '../../hooks/useListNav';
import { useToolUsage } from '../../hooks/useToolUsage';
import { useLimits } from '../../hooks/useLimits';
import { useMetricsSeries, type MetricsTimeRange } from '../../hooks/useMetricsSeries';
import { WorkspaceShell } from '../layout/WorkspaceShell';
import { PopoutButton } from '../ui/PopoutButton';
import { PersistedFromMarker } from '../telemetry/PersistedFromMarker';
import { ClientModelCell } from '../pricing/ClientModelCell';
import { ServerModelCell } from '../pricing/ServerModelCell';
import { MetricsControls } from '../metrics/MetricsControls';
import { MetricsInspector } from '../metrics/MetricsInspector';
import {
  MetricsKpiRow,
  TokenChart,
  CostChart,
  PanelHeader,
  BreakdownTable,
  ModelMixBars,
  ScrollableBreakdown,
} from '../metrics/metricsShared';
import { BudgetBar, LimitsPanel } from '../metrics/LimitsShared';
import { budgetForRow, deriveLimitsSummary, type LimitRowScope } from '../metrics/limitsData';
import {
  aggregateModelMix,
  buildTokenChartData,
  buildCostChartData,
  derivePerServerRows,
  derivePerClientRows,
  derivePerToolRows,
  deriveSessionKpis,
  hasMetricsData,
  sortBreakdownRows,
  type BreakdownRow,
  type BreakdownSortColumn,
  type SortDirection,
} from '../metrics/metricsData';

type Scope = 'overview' | 'clients' | 'servers' | 'tools' | 'models';
const SCOPES: Scope[] = ['overview', 'clients', 'servers', 'tools', 'models'];

function isScope(v: string | null): v is Scope {
  return v != null && (SCOPES as string[]).includes(v);
}

// MetricsWorkspace is the first-class cost/token observability surface, sibling
// to Stack, Library, Variables, and Tools. The left rail is a scope
// navigator (overview / clients / servers / tools / models); the center carries the
// session KPI row, the trend charts, and the active scope's breakdown; the
// right rail inspects the selected client or server (and hosts its inline
// pricing-model editor). Scope and selection are URL-synced so reload and
// deep-links survive. The full dashboard body is shared with the bottom
// glance tab and the detached window via metricsShared.
export function MetricsWorkspace() {
  const [searchParams, setSearchParams] = useSearchParams();
  const compact = useUIStore((s) => s.compactMode.metrics);
  const setPricingManagerOpen = useUIStore((s) => s.setPricingManagerOpen);
  const metricsDetached = useUIStore((s) => s.metricsDetached);

  const tokenUsage = useStackStore((s) => s.tokenUsage);
  const costUsage = useStackStore((s) => s.costUsage);
  const costAttribution = useStackStore((s) => s.costAttribution);
  const clientModels = useStackStore((s) => s.clientModels);
  const effectiveClientModels = useStackStore((s) => s.effectiveClientModels);
  const effectiveServerModels = useStackStore((s) => s.effectiveServerModels);
  const mcpServers = useStackStore((s) => s.mcpServers);
  const defaultModel = useStackStore((s) => s.defaultModel);
  const setClientModelLocal = useStackStore((s) => s.setClientModelLocal);
  const setServerModelLocal = useStackStore((s) => s.setServerModelLocal);

  const { openDetachedWindow } = useWindowManager();

  const [timeRange, setTimeRange] = useState<MetricsTimeRange>('live');
  const [isPaused, setIsPaused] = useState(false);
  const [serverSort, setServerSort] = useState<{ col: BreakdownSortColumn; dir: SortDirection }>({ col: 'total', dir: 'desc' });
  const [clientSort, setClientSort] = useState<{ col: BreakdownSortColumn; dir: SortDirection }>({ col: 'cost', dir: 'desc' });
  // Cost-descending default so the most expensive tools surface first.
  const [toolSort, setToolSort] = useState<{ col: BreakdownSortColumn; dir: SortDirection }>({ col: 'cost', dir: 'desc' });
  // Polite announcement for range/refresh, read by screen readers.
  const [liveMsg, setLiveMsg] = useState('');

  const { metricsData, costData, isLoading, error, reload, clear } = useMetricsSeries({
    timeRange,
    paused: isPaused,
    perClient: true,
  });

  // Per-tool usage powers the Tools scope (and its rail count badge), so the
  // poll runs whenever the workspace is mounted — same 15s cadence Audit Mode
  // uses, against the same single per-tool data source.
  const { usage: toolUsageData, error: toolUsageError } = useToolUsage(true);

  // Limit consumption overlays the breakdown rows and the Limits panel. A
  // stack without a limits: block reports configured: false and renders
  // nothing anywhere.
  const { report: limitsReport } = useLimits(true);
  const limitsSummary = useMemo(() => deriveLimitsSummary(limitsReport), [limitsReport]);
  // Renders the consumption bar under a row's name when a budget governs it.
  const limitBarFor = useCallback(
    (scope: LimitRowScope) => (row: BreakdownRow) => {
      const entry = budgetForRow(limitsSummary.entries, scope, row.name);
      return entry ? <BudgetBar entry={entry} className="mt-1" /> : null;
    },
    [limitsSummary.entries],
  );

  // ---- URL state ----------------------------------------------------------
  const scope: Scope = isScope(searchParams.get('scope')) ? (searchParams.get('scope') as Scope) : 'overview';
  const selected = searchParams.get('selected');

  const setScope = useCallback(
    (next: Scope) => {
      setSearchParams(
        (prev) => {
          const params = new URLSearchParams(prev);
          if (next === 'overview') params.delete('scope');
          else params.set('scope', next);
          // Selection is scope-local — drop it when the axis changes.
          params.delete('selected');
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

  // ---- Derived data -------------------------------------------------------
  const kpis = deriveSessionKpis(tokenUsage, costUsage, costAttribution, effectiveClientModels, effectiveServerModels);
  const chartData = useMemo(() => buildTokenChartData(metricsData), [metricsData]);
  const costChartData = useMemo(() => buildCostChartData(costData), [costData]);
  const costSeriesHasData = costChartData.some((d) => d['Cost (USD)'] > 0);
  const hasData = hasMetricsData(kpis, metricsData, costData);

  const declaredServerModels = useMemo(() => {
    const out: Record<string, string> = {};
    for (const s of mcpServers) if (s.model) out[s.name] = s.model;
    return out;
  }, [mcpServers]);

  const serverRows = useMemo(
    () => sortBreakdownRows(derivePerServerRows(tokenUsage, costUsage), serverSort.col, serverSort.dir),
    [tokenUsage, costUsage, serverSort],
  );
  const clientRows = useMemo(
    () => sortBreakdownRows(derivePerClientRows(tokenUsage, costUsage), clientSort.col, clientSort.dir),
    [tokenUsage, costUsage, clientSort],
  );
  const toolRows = useMemo(
    () => sortBreakdownRows(derivePerToolRows(toolUsageData), toolSort.col, toolSort.dir),
    [toolUsageData, toolSort],
  );
  const modelMix = useMemo(
    () => aggregateModelMix(effectiveServerModels, effectiveClientModels),
    [effectiveServerModels, effectiveClientModels],
  );

  // Rows for the active selectable scope (clients/servers/tools), used by the
  // inspector lookup and keyboard navigation.
  const activeRows: BreakdownRow[] = useMemo(
    () =>
      scope === 'servers' ? serverRows : scope === 'clients' ? clientRows : scope === 'tools' ? toolRows : [],
    [scope, serverRows, clientRows, toolRows],
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
  useListNav({
    itemCount: activeRows.length,
    selectedIndex,
    setSelectedIndex: (i) => {
      const next = activeRows[i];
      if (next) setSelected(next.name);
    },
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
  const inspectorCostPoints =
    selectedRow && scope === 'servers'
      ? costData?.per_server?.[selectedRow.name]
      : selectedRow && scope === 'clients'
        ? costData?.per_client?.[selectedRow.name]
        : undefined;
  // Tools have no pricing model of their own — their cost inherits the
  // client/server attribution — so the model editor wiring stays scoped to
  // clients/servers.
  const inspectorDeclared =
    scope === 'servers' ? declaredServerModels[selectedRow?.name ?? ''] : clientModels[selectedRow?.name ?? ''];
  const inspectorEffective =
    scope === 'servers'
      ? effectiveServerModels[selectedRow?.name ?? '']
      : scope === 'tools'
        ? undefined
        : effectiveClientModels[selectedRow?.name ?? ''];

  const inspector = (
    <MetricsInspector
      scope={inspectorScope}
      row={scope === 'clients' || scope === 'servers' || scope === 'tools' ? selectedRow : null}
      effective={inspectorEffective}
      declaredModel={inspectorDeclared}
      defaultModel={defaultModel}
      costAttribution={costAttribution}
      onClientSaved={setClientModelLocal}
      onServerSaved={setServerModelLocal}
      onOpenManager={() => setPricingManagerOpen(true)}
      onClose={() => setSelected(null)}
      tokenPoints={inspectorTokenPoints}
      costPoints={inspectorCostPoints}
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
      modelCount={modelMix.length}
    />
  );

  const onTimeRange = (r: MetricsTimeRange) => {
    setTimeRange(r);
    if (r === 'live') setIsPaused(false);
    setLiveMsg(`Showing ${r === 'live' ? 'live' : r} metrics`);
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
        minLeftPx={200}
        minRightPx={300}
      >
        <main className="flex flex-col h-full overflow-hidden">
          <header
            className={cn(
              'flex-shrink-0 bg-surface/30 backdrop-blur-sm border-b border-border-subtle flex items-center justify-between gap-3 px-6',
              compact ? 'py-2' : 'py-3',
            )}
          >
            <div className="flex items-center gap-3 min-w-0">
              <div className="font-sans text-text-muted/60 text-[10px] uppercase tracking-[0.4em]">metrics</div>
              <div className="font-mono text-[10px] text-text-muted truncate">
                {kpis.total > 0 ? `${kpis.total.toLocaleString()} tokens` : 'no traffic yet'}
              </div>
            </div>
            <MetricsControls
              timeRange={timeRange}
              onTimeRange={onTimeRange}
              isPaused={isPaused}
              onTogglePause={() => setIsPaused((p) => !p)}
              onRefresh={() => {
                reload();
                setLiveMsg('Metrics refreshed');
              }}
              onClear={() => void clear()}
              onOpenPricing={() => setPricingManagerOpen(true)}
              right={<PopoutButton onClick={() => openDetachedWindow('metrics')} disabled={metricsDetached} />}
            />
          </header>

          <div className="flex-1 min-h-0 overflow-y-auto scrollbar-dark px-6 py-4">
            {/* Mutually exclusive states: error → first-load skeleton (only when
                nothing is showable yet) → empty → data. Store totals make
                hasData true even before the series lands, so the data view wins
                over the skeleton once any traffic exists. */}
            {error && !isLoading && (
              <ErrorState message={error} onRetry={reload} />
            )}

            {!error && !hasData && isLoading && !metricsData && <LoadingState />}

            {!error && !hasData && !(isLoading && !metricsData) && (
              <MetricsEmptyState onOpenPricing={() => setPricingManagerOpen(true)} />
            )}

            {!error && hasData && (
              <div className="space-y-4 max-w-5xl">
                <PersistedFromMarker serverName={null} signal="metrics" />
                <MetricsKpiRow kpis={kpis} />
                <div className="grid gap-4 xl:grid-cols-2">
                  <TokenChart data={chartData} metricsData={metricsData} />
                  {(kpis.hasCost || costSeriesHasData) && <CostChart data={costChartData} costData={costData} />}
                </div>

                {scope === 'overview' && <LimitsPanel summary={limitsSummary} />}

                {scope === 'overview' && (
                  <PanelHeader icon={Layers} label="Cost by Model">
                    <ModelMixBars mix={modelMix} />
                  </PanelHeader>
                )}

                {scope === 'models' && (
                  <PanelHeader icon={Layers} label="Cost by Model">
                    <ModelMixBars mix={modelMix} />
                  </PanelHeader>
                )}

                {scope === 'tools' && (
                  <PanelHeader icon={Wrench} label="Per-Tool">
                    {toolRows.length > 0 ? (
                      <ScrollableBreakdown>
                        <BreakdownTable
                          rows={toolRows}
                          nameLabel="Tool"
                          sortColumn={toolSort.col}
                          sortDirection={toolSort.dir}
                          onSort={sortTools}
                          showCost
                          selectedName={selected}
                          onSelectRow={setSelected}
                          renderNameExtra={limitBarFor('tool')}
                        />
                      </ScrollableBreakdown>
                    ) : toolUsageError ? (
                      // A failed fetch is not "no usage" — say the data source
                      // is unavailable instead of implying calls went unrecorded.
                      <EmptyScopeNote text={`Tool usage unavailable: ${toolUsageError}`} />
                    ) : (
                      <EmptyScopeNote text="No per-tool usage recorded yet. Tool rows appear after the first tool call; cost needs a pricing model." />
                    )}
                  </PanelHeader>
                )}

                {scope === 'clients' && (
                  <PanelHeader icon={Users} label="Top Clients">
                    {clientRows.length > 0 ? (
                      <BreakdownTable
                        rows={clientRows}
                        nameLabel="Client"
                        sortColumn={clientSort.col}
                        sortDirection={clientSort.dir}
                        onSort={sortClients}
                        showCost
                        selectedName={selected}
                        onSelectRow={setSelected}
                        renderNameExtra={limitBarFor('client')}
                        renderModel={(row) => (
                          <ClientModelCell
                            client={row.name}
                            declaredModel={clientModels[row.name]}
                            effective={effectiveClientModels[row.name]}
                            costAttribution={costAttribution}
                            onSaved={setClientModelLocal}
                            onOpenManager={() => setPricingManagerOpen(true)}
                          />
                        )}
                      />
                    ) : (
                      <EmptyScopeNote text="No per-client attribution yet. Calls carry a client identity once an MCP client connects." />
                    )}
                  </PanelHeader>
                )}

                {scope === 'servers' && (
                  <PanelHeader icon={Server} label="Per-Server">
                    {serverRows.length > 0 ? (
                      <BreakdownTable
                        rows={serverRows}
                        nameLabel="Server"
                        sortColumn={serverSort.col}
                        sortDirection={serverSort.dir}
                        onSort={sortServers}
                        showCost
                        selectedName={selected}
                        onSelectRow={setSelected}
                        renderNameExtra={limitBarFor('server')}
                        renderModel={(row) => (
                          <ServerModelCell
                            server={row.name}
                            declaredModel={declaredServerModels[row.name]}
                            defaultModel={defaultModel}
                            effective={effectiveServerModels[row.name]}
                            onSaved={setServerModelLocal}
                            onOpenManager={() => setPricingManagerOpen(true)}
                          />
                        )}
                      />
                    ) : (
                      <EmptyScopeNote text="No per-server traffic recorded yet." />
                    )}
                  </PanelHeader>
                )}
              </div>
            )}
          </div>
        </main>
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
  modelCount: number;
}

function ScopeRail({ compact, scope, onSelectScope, clientCount, serverCount, toolCount, modelCount }: ScopeRailProps) {
  return (
    <aside className="h-full flex flex-col bg-surface border-r border-border-subtle">
      <div className={cn('flex-shrink-0 px-3 border-b border-border-subtle/60', compact ? 'py-2' : 'py-3')}>
        <div className="text-[10px] font-medium text-text-muted/60 uppercase tracking-[0.3em]">breakdown</div>
      </div>
      <div className="flex-1 min-h-0 overflow-y-auto scrollbar-dark px-2 py-2 space-y-0.5">
        <ScopePill label="Overview" icon={BarChart3} active={scope === 'overview'} onClick={() => onSelectScope('overview')} />
        <ScopePill label="Clients" icon={Users} count={clientCount} active={scope === 'clients'} onClick={() => onSelectScope('clients')} />
        <ScopePill label="Servers" icon={Server} count={serverCount} active={scope === 'servers'} onClick={() => onSelectScope('servers')} />
        <ScopePill label="Tools" icon={Wrench} count={toolCount} active={scope === 'tools'} onClick={() => onSelectScope('tools')} />
        <ScopePill label="Models" icon={Boxes} count={modelCount} active={scope === 'models'} onClick={() => onSelectScope('models')} />
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
            'flex-shrink-0 text-[10px] font-mono px-1.5 py-0.5 rounded tabular-nums',
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
  return <p className="px-3 py-3 text-[11px] text-text-muted/70 leading-relaxed">{text}</p>;
}

function LoadingState() {
  return (
    <div className="space-y-4 max-w-5xl animate-pulse">
      <div className="grid grid-cols-4 gap-3">
        {[1, 2, 3, 4].map((i) => (
          <div key={i} className="h-16 rounded-lg bg-surface-elevated/60 border border-border/30" />
        ))}
      </div>
      <div className="grid gap-4 xl:grid-cols-2">
        <div className="h-44 rounded-lg bg-surface-elevated/60 border border-border/30" />
        <div className="h-44 rounded-lg bg-surface-elevated/60 border border-border/30" />
      </div>
    </div>
  );
}

function ErrorState({ message, onRetry }: { message: string; onRetry: () => void }) {
  return (
    <div className="flex flex-col items-center justify-center h-full gap-3">
      <span className="text-xs text-status-error">{message}</span>
      <button onClick={onRetry} className="text-xs text-primary hover:underline">
        Retry
      </button>
    </div>
  );
}

function MetricsEmptyState({ onOpenPricing }: { onOpenPricing: () => void }) {
  return (
    <div className="h-full flex items-center justify-center px-6 py-12">
      <div className="max-w-md w-full text-center space-y-5 animate-fade-in-scale">
        <div className="relative mx-auto w-16 h-16">
          <div className="absolute inset-0 rounded-2xl bg-primary/10 border border-primary/20 flex items-center justify-center">
            <BarChart3 size={26} className="text-primary/70" />
          </div>
          <div className="absolute -inset-2 rounded-3xl bg-primary/5 blur-2xl -z-10" />
        </div>
        <div className="space-y-1.5">
          <h2 className="text-base font-semibold text-text-primary">Your metrics home</h2>
          <p className="text-xs text-text-muted leading-relaxed">
            Token usage appears here after the first tool call. Estimated cost needs a pricing model:
            declare one per client or server, or set a gateway default.
          </p>
        </div>
        <button
          onClick={onOpenPricing}
          className="inline-flex items-center gap-1.5 px-4 py-2 text-xs font-semibold rounded-lg bg-gradient-to-r from-primary to-primary-dark text-background shadow-[0_1px_12px_rgba(245,158,11,0.3)] hover:shadow-[0_2px_18px_rgba(245,158,11,0.4)] hover:-translate-y-0.5 active:translate-y-0 transition-all duration-200"
        >
          Edit pricing models
        </button>
      </div>
    </div>
  );
}

export default MetricsWorkspace;
