import { useEffect, useState, useCallback } from 'react';
import { BarChart3, AlertCircle, Maximize2, Minimize2, Users, Server, Wrench } from 'lucide-react';
import { IconButton } from '../components/ui/IconButton';
import { ErrorBoundary } from '../components/ui/ErrorBoundary';
import { useDetachedWindowSync } from '../hooks/useBroadcastChannel';
import { fetchStatus } from '../lib/api';
import { formatCompactNumber, formatUSD } from '../lib/format';
import { POLLING } from '../lib/constants';
import { ClientModelCell } from '../components/pricing/ClientModelCell';
import { ServerModelCell } from '../components/pricing/ServerModelCell';
import { PricingManagerSlideOver } from '../components/pricing/PricingManagerSlideOver';
import { useMetricsSeries, type MetricsTimeRange } from '../hooks/useMetricsSeries';
import { useToolUsage } from '../hooks/useToolUsage';
import { useLimits } from '../hooks/useLimits';
import { BudgetBar, LimitsPanel } from '../components/metrics/LimitsShared';
import { budgetForRow, deriveLimitsSummary, type LimitRowScope } from '../components/metrics/limitsData';
import { MetricsControls } from '../components/metrics/MetricsControls';
import { MetricsKpiRow, TokenChart, CostChart, PanelHeader, BreakdownTable, ScrollableBreakdown } from '../components/metrics/metricsShared';
import {
  buildTokenChartData,
  buildCostChartData,
  derivePerServerRows,
  derivePerClientRows,
  derivePerToolRows,
  deriveSessionKpis,
  hasMetricsData,
  sortBreakdownRows,
  type BreakdownSortColumn,
  type SortDirection,
} from '../components/metrics/metricsData';
import type { GatewayStatus, TokenUsage, CostUsage, EffectiveModel } from '../types';

function DetachedMetricsPageContent() {
  // Real-time status snapshot — the detached window lives outside the app
  // shell, so it owns its own status poll (the in-shell surfaces read the app
  // store instead). The time-series come from the shared useMetricsSeries hook.
  const [tokenUsage, setTokenUsage] = useState<TokenUsage | null>(null);
  const [costUsage, setCostUsage] = useState<CostUsage | null>(null);
  const [costAttribution, setCostAttribution] = useState(false);
  const [clientModels, setClientModels] = useState<Record<string, string>>({});
  const [serverDeclared, setServerDeclared] = useState<Record<string, string>>({});
  const [effectiveClientModels, setEffectiveClientModels] = useState<Record<string, EffectiveModel>>({});
  const [effectiveServerModels, setEffectiveServerModels] = useState<Record<string, EffectiveModel>>({});
  const [serverNames, setServerNames] = useState<string[]>([]);
  const [defaultModel, setDefaultModel] = useState('');
  const [pricingManagerOpen, setPricingManagerOpen] = useState(false);
  const [timeRange, setTimeRange] = useState<MetricsTimeRange>('live');
  const [isPaused, setIsPaused] = useState(false);
  const [sortColumn, setSortColumn] = useState<BreakdownSortColumn>('total');
  const [sortDirection, setSortDirection] = useState<SortDirection>('desc');
  const [clientSortColumn, setClientSortColumn] = useState<BreakdownSortColumn>('cost');
  const [clientSortDirection, setClientSortDirection] = useState<SortDirection>('desc');
  const [toolSortColumn, setToolSortColumn] = useState<BreakdownSortColumn>('cost');
  const [toolSortDirection, setToolSortDirection] = useState<SortDirection>('desc');
  const [isFullscreen, setIsFullscreen] = useState(false);

  useDetachedWindowSync('metrics');

  // Per-tool usage for the Per-Tool panel — the same GET /api/tools/usage
  // pipeline the Tools workspace and Metrics Tools scope consume.
  const { usage: toolUsageData } = useToolUsage(true);

  const { metricsData, costData, isLoading, error, reload, clear } = useMetricsSeries({
    timeRange,
    paused: isPaused,
  });

  // Poll status for real-time token + cost usage
  useEffect(() => {
    const pollStatus = async () => {
      try {
        const status: GatewayStatus = await fetchStatus();
        setTokenUsage(status.token_usage ?? null);
        setCostUsage(status.cost ?? null);
        setCostAttribution(status.cost_attribution ?? false);
        setClientModels(status.client_models ?? {});
        setEffectiveClientModels(status.effective_client_models ?? {});
        setEffectiveServerModels(status.effective_server_models ?? {});
        setDefaultModel(status.default_model ?? '');
        const declared: Record<string, string> = {};
        for (const s of status['mcp-servers'] ?? []) {
          if (s.model) declared[s.name] = s.model;
        }
        setServerDeclared(declared);
        setServerNames((status['mcp-servers'] ?? []).map((s) => s.name));
      } catch {
        // Ignore status errors
      }
    };

    pollStatus();
    const interval = window.setInterval(pollStatus, POLLING.STATUS);
    return () => clearInterval(interval);
  }, []);

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

  // Optimistic local updates after a successful model save. The detached
  // window owns local state (not the main window's store); the next status
  // poll confirms from the backend, and the main window catches up on its own.
  const handleClientModelSaved = useCallback((client: string, model: string) => {
    setClientModels((prev) => {
      const next = { ...prev };
      if (model === '') delete next[client];
      else next[client] = model;
      return next;
    });
  }, []);

  const handleServerModelSaved = useCallback((server: string, model: string) => {
    setServerDeclared((prev) => {
      const next = { ...prev };
      if (model === '') delete next[server];
      else next[server] = model;
      return next;
    });
  }, []);

  const handleDefaultModelSaved = useCallback((model: string) => {
    setDefaultModel(model);
  }, []);

  const kpis = deriveSessionKpis(
    tokenUsage,
    costUsage,
    costAttribution,
    effectiveClientModels,
    effectiveServerModels,
  );
  const sortedServers = sortBreakdownRows(derivePerServerRows(tokenUsage), sortColumn, sortDirection);
  const sortedClients = sortBreakdownRows(
    derivePerClientRows(tokenUsage, costUsage),
    clientSortColumn,
    clientSortDirection,
  );
  const sortedTools = sortBreakdownRows(derivePerToolRows(toolUsageData), toolSortColumn, toolSortDirection);

  // Limit consumption overlay — same source and presentation as the in-shell
  // workspace (parity is the point of the shared core).
  const { report: limitsReport } = useLimits(true);
  const limitsSummary = deriveLimitsSummary(limitsReport);
  const limitBarFor = (scope: LimitRowScope) => (row: { name: string }) => {
    const entry = budgetForRow(limitsSummary.entries, scope, row.name);
    return entry ? <BudgetBar entry={entry} className="mt-1" /> : null;
  };

  const chartData = buildTokenChartData(metricsData);
  const costChartData = buildCostChartData(costData);
  const costSeriesHasData = costChartData.some((d) => d['Cost (USD)'] > 0);
  const hasData = hasMetricsData(kpis, metricsData, costData);

  const toggleFullscreen = async () => {
    if (!document.fullscreenElement) {
      await document.documentElement.requestFullscreen();
      setIsFullscreen(true);
    } else {
      await document.exitFullscreen();
      setIsFullscreen(false);
    }
  };

  useEffect(() => {
    const handler = () => setIsFullscreen(!!document.fullscreenElement);
    document.addEventListener('fullscreenchange', handler);
    return () => document.removeEventListener('fullscreenchange', handler);
  }, []);

  return (
    <div className="h-screen w-screen bg-background flex flex-col overflow-hidden">
      {/* Background grain */}
      <div
        className="fixed inset-0 pointer-events-none z-0 opacity-[0.015]"
        style={{
          backgroundImage: `url("data:image/svg+xml,%3Csvg viewBox='0 0 256 256' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='noise'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.9' numOctaves='4' stitchTiles='stitch'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23noise)'/%3E%3C/svg%3E")`,
        }}
      />

      {/* Header */}
      <header className="h-12 flex-shrink-0 bg-surface/90 backdrop-blur-xl border-b border-border/50 flex items-center justify-between px-4 z-10 relative">
        <div className="absolute top-0 left-0 right-0 h-px bg-gradient-to-r from-transparent via-primary/30 to-transparent" />

        <div className="flex items-center gap-3">
          <div className="p-1.5 rounded-lg border bg-primary/10 border-primary/20">
            <BarChart3 size={14} className="text-primary" />
          </div>
          <span className="text-sm font-semibold text-text-primary">Token Metrics</span>
        </div>

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
          onOpenPricing={() => setPricingManagerOpen(true)}
          right={
            <IconButton
              icon={isFullscreen ? Minimize2 : Maximize2}
              onClick={toggleFullscreen}
              tooltip={isFullscreen ? 'Exit Fullscreen' : 'Fullscreen'}
              size="sm"
              variant="ghost"
            />
          }
        />
      </header>

      {/* Content */}
      <main className="flex-1 overflow-auto bg-background scrollbar-dark min-h-0 p-4">
        {isLoading && !metricsData && (
          <div className="space-y-4 animate-pulse">
            <div className="grid grid-cols-4 gap-3">
              {[1, 2, 3, 4].map((i) => (
                <div key={i} className="h-16 rounded-lg bg-surface-elevated/60 border border-border/30" />
              ))}
            </div>
            <div className="h-48 rounded-lg bg-surface-elevated/60 border border-border/30" />
            <div className="h-32 rounded-lg bg-surface-elevated/60 border border-border/30" />
          </div>
        )}

        {error && !isLoading && (
          <div className="flex flex-col items-center justify-center h-full gap-3">
            <AlertCircle size={24} className="text-status-error" />
            <span className="text-xs text-status-error">{error}</span>
            <button onClick={reload} className="text-xs text-primary hover:underline">
              Retry
            </button>
          </div>
        )}

        {!isLoading && !error && !hasData && (
          <div className="flex flex-col items-center justify-center h-full text-text-muted gap-2">
            <BarChart3 size={32} className="text-text-muted/30" />
            <span className="text-sm">No token data yet</span>
            <span className="text-xs text-text-muted/60">Metrics will appear after tool calls</span>
          </div>
        )}

        {!error && hasData && (
          <div className="space-y-4">
            <MetricsKpiRow kpis={kpis} />
            <TokenChart data={chartData} metricsData={metricsData} heightClass="h-48" />
            {(kpis.hasCost || costSeriesHasData) && (
              <CostChart data={costChartData} costData={costData} heightClass="h-40" />
            )}

            <LimitsPanel summary={limitsSummary} />

            {sortedClients.length > 0 && (
              <PanelHeader icon={Users} label="Top Clients">
                <BreakdownTable
                  rows={sortedClients}
                  nameLabel="Client"
                  sortColumn={clientSortColumn}
                  sortDirection={clientSortDirection}
                  onSort={handleClientSort}
                  showCost
                  renderNameExtra={limitBarFor('client')}
                  renderModel={(row) => (
                    <ClientModelCell
                      client={row.name}
                      declaredModel={clientModels[row.name]}
                      effective={effectiveClientModels[row.name]}
                      costAttribution={costAttribution}
                      onSaved={handleClientModelSaved}
                      onOpenManager={() => setPricingManagerOpen(true)}
                    />
                  )}
                />
              </PanelHeader>
            )}

            {sortedServers.length > 0 && (
              <PanelHeader icon={Server} label="Per-Server">
                <BreakdownTable
                  rows={sortedServers}
                  nameLabel="Server"
                  sortColumn={sortColumn}
                  sortDirection={sortDirection}
                  onSort={handleSort}
                  renderNameExtra={limitBarFor('server')}
                  renderModel={(row) => (
                    <ServerModelCell
                      server={row.name}
                      declaredModel={serverDeclared[row.name]}
                      defaultModel={defaultModel}
                      effective={effectiveServerModels[row.name]}
                      onSaved={handleServerModelSaved}
                      onOpenManager={() => setPricingManagerOpen(true)}
                    />
                  )}
                />
              </PanelHeader>
            )}

            {sortedTools.length > 0 && (
              <PanelHeader icon={Wrench} label="Per-Tool">
                <ScrollableBreakdown>
                  <BreakdownTable
                    rows={sortedTools}
                    nameLabel="Tool"
                    sortColumn={toolSortColumn}
                    sortDirection={toolSortDirection}
                    onSort={handleToolSort}
                    showCost
                    renderNameExtra={limitBarFor('tool')}
                  />
                </ScrollableBreakdown>
              </PanelHeader>
            )}
          </div>
        )}
      </main>

      {/* Footer */}
      <footer className="h-6 flex-shrink-0 bg-surface/90 backdrop-blur-xl border-t border-border/50 flex items-center justify-between px-4 text-[10px] text-text-muted">
        <span className="flex items-center gap-2">
          {kpis.total > 0 ? `${formatCompactNumber(kpis.total)} total tokens` : 'No data'}
          {kpis.hasCost && (
            <>
              <span className="text-text-muted/50">·</span>
              <span className="text-emerald-400/80">{formatUSD(kpis.costUSD ?? 0)}</span>
            </>
          )}
          {isPaused ? ' (paused)' : ''}
        </span>
        <span className="flex items-center gap-1">
          <span className="w-1.5 h-1.5 rounded-full bg-text-muted animate-pulse motion-reduce:animate-none" />
          Detached Window
        </span>
      </footer>

      {/* Pricing models manager — detached host: data from this window's local
          status poll, optimistic updates into the same local state. */}
      <PricingManagerSlideOver
        open={pricingManagerOpen}
        onClose={() => setPricingManagerOpen(false)}
        defaultModel={defaultModel}
        servers={serverNames.map((name) => ({ name, declaredModel: serverDeclared[name] }))}
        clients={[...new Set([
          ...Object.keys(clientModels),
          ...Object.keys(tokenUsage?.per_client ?? {}),
          ...Object.keys(costUsage?.per_client ?? {}),
        ])].sort().map((name) => ({ name, declaredModel: clientModels[name] }))}
        costAttribution={costAttribution}
        effectiveClientModels={effectiveClientModels}
        effectiveServerModels={effectiveServerModels}
        onClientSaved={handleClientModelSaved}
        onServerSaved={handleServerModelSaved}
        onDefaultSaved={handleDefaultModelSaved}
      />
    </div>
  );
}

export function DetachedMetricsPage() {
  return (
    <ErrorBoundary variant="window">
      <DetachedMetricsPageContent />
    </ErrorBoundary>
  );
}
