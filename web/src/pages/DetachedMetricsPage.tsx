import { useEffect, useState, useCallback, Component, type ComponentType, type ReactNode } from 'react';
import {
  BarChart3,
  AlertCircle,
  Maximize2,
  Minimize2,
  DollarSign,
  Users,
} from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { cn } from '../lib/cn';
import { IconButton } from '../components/ui/IconButton';
import { useDetachedWindowSync } from '../hooks/useBroadcastChannel';
import { fetchStatus, fetchTokenMetrics, fetchCostMetrics, clearTokenMetrics } from '../lib/api';
import { formatCompactNumber, formatUSD } from '../lib/format';
import { POLLING } from '../lib/constants';
import { AreaChart } from '../components/chart/AreaChart';
import { ClientModelCell } from '../components/pricing/ClientModelCell';
import { ServerModelCell } from '../components/pricing/ServerModelCell';
import { PricingManagerSlideOver } from '../components/pricing/PricingManagerSlideOver';
import { ATTRIBUTION_HINT, MIXED_PROVENANCE_NOTE } from '../components/pricing/constants';
import type { GatewayStatus, TokenMetricsResponse, CostMetricsResponse, TokenUsage, CostUsage, EffectiveModel } from '../types';
import {
  Pause,
  Play,
  Trash2,
  RefreshCw,
  ArrowUpDown,
  ArrowUp,
  ArrowDown,
} from 'lucide-react';

// Error boundary for detached window
interface ErrorBoundaryState {
  hasError: boolean;
  error: Error | null;
}

class DetachedErrorBoundary extends Component<{ children: ReactNode }, ErrorBoundaryState> {
  constructor(props: { children: ReactNode }) {
    super(props);
    this.state = { hasError: false, error: null };
  }

  static getDerivedStateFromError(error: Error) {
    return { hasError: true, error };
  }

  render() {
    if (this.state.hasError) {
      return (
        <div className="h-screen w-screen bg-background flex items-center justify-center">
          <div className="text-center p-8 max-w-md">
            <div className="p-4 rounded-xl bg-status-error/10 border border-status-error/20 inline-block mb-4">
              <AlertCircle size={32} className="text-status-error" />
            </div>
            <h1 className="text-lg text-status-error mb-2">Something went wrong</h1>
            <pre className="text-xs text-text-muted bg-surface p-4 rounded-lg overflow-auto max-h-32 mb-4">
              {this.state.error?.message}
            </pre>
            <button
              onClick={() => window.location.reload()}
              className="px-4 py-2 bg-primary text-background rounded-lg font-medium hover:bg-primary-light transition-colors"
            >
              Reload Window
            </button>
          </div>
        </div>
      );
    }
    return this.props.children;
  }
}

type TimeRange = 'live' | '1h' | '6h' | '24h' | '7d';
type SortColumn = 'name' | 'input' | 'output' | 'total';
type SortDirection = 'asc' | 'desc';
type ClientSortColumn = 'name' | 'input' | 'output' | 'total' | 'cost';

const TIME_RANGES: { value: TimeRange; label: string }[] = [
  { value: 'live', label: 'Live' },
  { value: '1h', label: '1h' },
  { value: '6h', label: '6h' },
  { value: '24h', label: '24h' },
  { value: '7d', label: '7d' },
];

function DetachedMetricsPageContent() {
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
  const [timeRange, setTimeRange] = useState<TimeRange>('live');
  const [isPaused, setIsPaused] = useState(false);
  const [metricsData, setMetricsData] = useState<TokenMetricsResponse | null>(null);
  const [costData, setCostData] = useState<CostMetricsResponse | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showClearConfirm, setShowClearConfirm] = useState(false);
  const [sortColumn, setSortColumn] = useState<SortColumn>('total');
  const [sortDirection, setSortDirection] = useState<SortDirection>('desc');
  const [clientSortColumn, setClientSortColumn] = useState<ClientSortColumn>('cost');
  const [clientSortDirection, setClientSortDirection] = useState<SortDirection>('desc');
  const [isFullscreen, setIsFullscreen] = useState(false);

  useDetachedWindowSync('metrics');

  const apiRange = timeRange === 'live' ? '30m' : timeRange;

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

  const loadMetrics = useCallback(async () => {
    try {
      const [tokenResult, costResult] = await Promise.allSettled([
        fetchTokenMetrics(apiRange),
        fetchCostMetrics(apiRange),
      ]);
      if (tokenResult.status === 'fulfilled') setMetricsData(tokenResult.value);
      if (costResult.status === 'fulfilled') setCostData(costResult.value);
      const firstFailure =
        (tokenResult.status === 'rejected' && tokenResult.reason) ||
        (costResult.status === 'rejected' && costResult.reason);
      if (firstFailure) {
        setError(firstFailure instanceof Error ? firstFailure.message : 'Failed to fetch metrics');
      } else {
        setError(null);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch metrics');
    } finally {
      setIsLoading(false);
    }
  }, [apiRange]);

  // Fetch on mount and range change
  useEffect(() => {
    setIsLoading(true);
    loadMetrics();
  }, [apiRange, loadMetrics]);

  // Auto-refresh in live mode
  useEffect(() => {
    if (isPaused || timeRange !== 'live') return;

    const interval = window.setInterval(loadMetrics, POLLING.METRICS);
    return () => clearInterval(interval);
  }, [isPaused, timeRange, loadMetrics]);

  const handleClearMetrics = async () => {
    try {
      await clearTokenMetrics();
      setMetricsData(null);
      setCostData(null);
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

  const handleClientSort = (column: ClientSortColumn) => {
    if (clientSortColumn === column) {
      setClientSortDirection((d) => (d === 'asc' ? 'desc' : 'asc'));
    } else {
      setClientSortColumn(column);
      setClientSortDirection('desc');
    }
  };

  // Optimistic local updates after a successful model save. The detached
  // window owns local state (not the main window's store); the next status
  // poll confirms from the backend, and the main window catches up on its
  // own poll.
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

  // Per-server data
  const perServerEntries = tokenUsage?.per_server
    ? Object.entries(tokenUsage.per_server).map(([name, counts]) => ({
        name,
        input: counts.input_tokens,
        output: counts.output_tokens,
        total: counts.total_tokens,
      }))
    : [];

  const sortedServers = [...perServerEntries].sort((a, b) => {
    const dir = sortDirection === 'asc' ? 1 : -1;
    if (sortColumn === 'name') return dir * a.name.localeCompare(b.name);
    return dir * (a[sortColumn] - b[sortColumn]);
  });

  const sessionInput = tokenUsage?.session.input_tokens ?? 0;
  const sessionOutput = tokenUsage?.session.output_tokens ?? 0;
  const sessionTotal = tokenUsage?.session.total_tokens ?? 0;
  const savingsPercent = tokenUsage?.format_savings.savings_percent ?? 0;
  const savedTokens = tokenUsage?.format_savings.saved_tokens ?? 0;
  const sessionCostUSD = costUsage?.session.total_usd;
  const hasCost = sessionCostUSD !== undefined;
  // Mirrors MetricsTab: without model attribution the gateway cannot price
  // calls, so explain the config requirement instead of a bare dash/$0.00.
  const showAttributionHint = !costAttribution && !(sessionCostUSD && sessionCostUSD > 0);
  const hasMixedProvenance =
    Object.values(effectiveClientModels).some((e) => e.provenance === 'mixed') ||
    Object.values(effectiveServerModels).some((e) => e.provenance === 'mixed');
  const hasData =
    sessionTotal > 0 ||
    (metricsData?.data_points?.length ?? 0) > 0 ||
    (costData?.data_points?.length ?? 0) > 0;

  const chartData = metricsData?.data_points
    ? metricsData.data_points.map((dp) => ({
        time: new Date(dp.timestamp).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
        "Input Tokens": dp.input_tokens,
        "Output Tokens": dp.output_tokens,
      }))
    : [];

  const costChartData = costData?.data_points
    ? costData.data_points.map((dp) => ({
        time: new Date(dp.timestamp).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
        "Cost (USD)": dp.usd,
      }))
    : [];

  const costSeriesHasData = costChartData.some((d) => d['Cost (USD)'] > 0);

  const tokenClients = tokenUsage?.per_client ?? {};
  const costClients = costUsage?.per_client ?? {};
  const clientNames = new Set<string>([...Object.keys(tokenClients), ...Object.keys(costClients)]);
  const perClientRows = Array.from(clientNames).map((name) => {
    const tokens = tokenClients[name];
    const cost = costClients[name];
    return {
      name,
      input: tokens?.input_tokens ?? 0,
      output: tokens?.output_tokens ?? 0,
      total: tokens?.total_tokens ?? 0,
      cost: cost?.total_usd,
    };
  });
  const sortedClients = [...perClientRows].sort((a, b) => {
    const dir = clientSortDirection === 'asc' ? 1 : -1;
    if (clientSortColumn === 'name') return dir * a.name.localeCompare(b.name);
    if (clientSortColumn === 'cost') {
      const aCost = a.cost ?? -Infinity;
      const bCost = b.cost ?? -Infinity;
      return dir * (aCost - bCost);
    }
    return dir * (a[clientSortColumn] - b[clientSortColumn]);
  });

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

          {/* Time range selector */}
          <div className="flex items-center bg-background/60 rounded-md border border-border/40 overflow-hidden ml-2">
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

          {timeRange === 'live' && !isPaused && (
            <span className="flex items-center gap-1 text-[9px] text-status-running font-medium">
              <span className="w-1.5 h-1.5 rounded-full bg-status-running animate-pulse" />
              Live
            </span>
          )}
          {isPaused && (
            <span className="text-[10px] px-2 py-0.5 bg-status-pending/15 text-status-pending rounded-full font-medium border border-status-pending/20">
              Paused
            </span>
          )}
        </div>

        <div className="flex items-center gap-1">
          {timeRange === 'live' && (
            <IconButton
              icon={isPaused ? Play : Pause}
              onClick={() => setIsPaused(!isPaused)}
              tooltip={isPaused ? 'Resume' : 'Pause'}
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

          <IconButton
            icon={DollarSign}
            onClick={() => setPricingManagerOpen(true)}
            tooltip="Edit pricing models"
            size="sm"
            variant="ghost"
          />

          <div className="w-px h-4 bg-border/50 mx-1" />
          <IconButton
            icon={isFullscreen ? Minimize2 : Maximize2}
            onClick={toggleFullscreen}
            tooltip={isFullscreen ? 'Exit Fullscreen' : 'Fullscreen'}
            size="sm"
            variant="ghost"
          />
        </div>
      </header>

      {/* Content */}
      <main className="flex-1 overflow-auto bg-background scrollbar-dark min-h-0 p-4">
        {/* Loading skeleton */}
        {isLoading && !metricsData && (
          <div className="space-y-4 animate-pulse">
            <div className="grid grid-cols-3 gap-3">
              {[1, 2, 3].map((i) => (
                <div key={i} className="h-16 rounded-lg bg-surface-elevated/60 border border-border/30" />
              ))}
            </div>
            <div className="h-48 rounded-lg bg-surface-elevated/60 border border-border/30" />
            <div className="h-32 rounded-lg bg-surface-elevated/60 border border-border/30" />
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
            <BarChart3 size={32} className="text-text-muted/30" />
            <span className="text-sm">No token data yet</span>
            <span className="text-xs text-text-muted/60">Metrics will appear after tool calls</span>
          </div>
        )}

        {/* Data view */}
        {!error && hasData && (
          <div className="space-y-4">
            {/* KPI Cards */}
            <div className={cn('grid gap-3', savingsPercent > 0 ? 'grid-cols-5' : 'grid-cols-4')}>
              <KPICard label="Input Tokens" value={sessionInput} colorClass="text-secondary" />
              <KPICard label="Output Tokens" value={sessionOutput} colorClass="text-primary" />
              <KPICard label="Total Tokens" value={sessionTotal} colorClass="text-text-primary" />
              <CostKPICard usd={sessionCostUSD} hasCost={hasCost} showHint={showAttributionHint} showMixedNote={hasMixedProvenance} />
              {savingsPercent > 0 && (
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
                className="h-48"
              />
            </div>

            {(hasCost || costSeriesHasData) && (
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
                <AreaChart
                  data={costChartData}
                  index="time"
                  categories={["Cost (USD)"]}
                  colors={["emerald"]}
                  type="default"
                  fill="gradient"
                  showLegend={false}
                  showGridLines={true}
                  showYAxis={true}
                  yAxisWidth={56}
                  valueFormatter={(v: number) => formatUSD(v)}
                  className="h-40"
                />
              </div>
            )}

            {/* Top Clients */}
            {sortedClients.length > 0 && (
              <PanelHeader icon={Users} label="Top Clients">
                <table className="w-full text-xs">
                  <thead>
                    <tr className="border-b border-border/30">
                      <ClientSortableHeader label="Client" column="name" sortColumn={clientSortColumn} sortDirection={clientSortDirection} onSort={handleClientSort} />
                      <th className="px-3 py-1.5 text-left text-[10px] font-medium text-text-muted uppercase tracking-wider">Model</th>
                      <ClientSortableHeader label="Input" column="input" sortColumn={clientSortColumn} sortDirection={clientSortDirection} onSort={handleClientSort} align="right" />
                      <ClientSortableHeader label="Output" column="output" sortColumn={clientSortColumn} sortDirection={clientSortDirection} onSort={handleClientSort} align="right" />
                      <ClientSortableHeader label="Total" column="total" sortColumn={clientSortColumn} sortDirection={clientSortDirection} onSort={handleClientSort} align="right" />
                      <ClientSortableHeader label="Cost" column="cost" sortColumn={clientSortColumn} sortDirection={clientSortDirection} onSort={handleClientSort} align="right" />
                    </tr>
                  </thead>
                  <tbody>
                    {sortedClients.map((client) => (
                      <tr key={client.name} className="border-b border-border/20 last:border-0 hover:bg-surface-highlight/30 transition-colors">
                        <td className="px-3 py-2 font-medium text-text-primary font-mono">{client.name}</td>
                        <td className="px-3 py-2">
                          <ClientModelCell
                            client={client.name}
                            declaredModel={clientModels[client.name]}
                            effective={effectiveClientModels[client.name]}
                            costAttribution={costAttribution}
                            onSaved={handleClientModelSaved}
                            onOpenManager={() => setPricingManagerOpen(true)}
                          />
                        </td>
                        <td className="px-3 py-2 text-right text-secondary tabular-nums">{formatCompactNumber(client.input)}</td>
                        <td className="px-3 py-2 text-right text-primary tabular-nums">{formatCompactNumber(client.output)}</td>
                        <td className="px-3 py-2 text-right text-text-primary font-semibold tabular-nums">{formatCompactNumber(client.total)}</td>
                        <td className={cn('px-3 py-2 text-right tabular-nums', client.cost === undefined ? 'text-text-muted' : 'text-emerald-400')}>
                          {client.cost === undefined ? '—' : formatUSD(client.cost)}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </PanelHeader>
            )}

            {/* Per-Server Breakdown Table */}
            {sortedServers.length > 0 && (
              <div className="rounded-lg bg-surface-elevated/60 border border-border/30 overflow-hidden">
                <table className="w-full text-xs">
                  <thead>
                    <tr className="border-b border-border/30">
                      <SortableHeader label="Server" column="name" sortColumn={sortColumn} sortDirection={sortDirection} onSort={handleSort} />
                      <th className="px-3 py-1.5 text-left text-[10px] font-medium text-text-muted uppercase tracking-wider">Model</th>
                      <SortableHeader label="Input" column="input" sortColumn={sortColumn} sortDirection={sortDirection} onSort={handleSort} align="right" />
                      <SortableHeader label="Output" column="output" sortColumn={sortColumn} sortDirection={sortDirection} onSort={handleSort} align="right" />
                      <SortableHeader label="Total" column="total" sortColumn={sortColumn} sortDirection={sortDirection} onSort={handleSort} align="right" />
                    </tr>
                  </thead>
                  <tbody>
                    {sortedServers.map((server) => (
                      <tr key={server.name} className="border-b border-border/20 hover:bg-surface-highlight/30 transition-colors">
                        <td className="px-3 py-2 font-medium text-text-primary font-mono">{server.name}</td>
                        <td className="px-3 py-2">
                          <ServerModelCell
                            server={server.name}
                            declaredModel={serverDeclared[server.name]}
                            defaultModel={defaultModel}
                            effective={effectiveServerModels[server.name]}
                            onSaved={handleServerModelSaved}
                            onOpenManager={() => setPricingManagerOpen(true)}
                          />
                        </td>
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
      </main>

      {/* Footer */}
      <footer className="h-6 flex-shrink-0 bg-surface/90 backdrop-blur-xl border-t border-border/50 flex items-center justify-between px-4 text-[10px] text-text-muted">
        <span className="flex items-center gap-2">
          {sessionTotal > 0 ? `${formatCompactNumber(sessionTotal)} total tokens` : 'No data'}
          {hasCost && (
            <>
              <span className="text-text-muted/50">·</span>
              <span className="text-emerald-400/80">{formatUSD(sessionCostUSD ?? 0)}</span>
            </>
          )}
          {isPaused ? ' (paused)' : ''}
        </span>
        <span className="flex items-center gap-1">
          <span className="w-1.5 h-1.5 rounded-full bg-text-muted animate-pulse" />
          Detached Window
        </span>
      </footer>

      {showClearConfirm && (
        <div className="fixed inset-0 z-40" onClick={() => setShowClearConfirm(false)} />
      )}

      {/* Pricing models manager — detached host: data from this window's
          local status poll, optimistic updates into the same local state. */}
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

// KPI Card
function KPICard({ label, value, colorClass }: { label: string; value: number; colorClass: string }) {
  return (
    <div className="rounded-lg bg-surface-elevated/60 border border-border/30 p-3">
      <span className="text-[10px] text-text-muted uppercase tracking-wider block mb-1">{label}</span>
      <span className={cn('text-lg font-bold tabular-nums', colorClass)}>{formatCompactNumber(value)}</span>
    </div>
  );
}

function CostKPICard({
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
        Cost
      </span>
      <span
        className={cn(
          'text-lg font-bold tabular-nums',
          hasCost ? 'text-emerald-400' : 'text-text-muted',
        )}
      >
        {hasCost ? formatUSD(usd ?? 0) : '—'}
      </span>
      {showHint && (
        <span className="block mt-1 text-[9px] leading-snug text-text-muted/60">
          {ATTRIBUTION_HINT}
        </span>
      )}
      {!showHint && showMixedNote && (
        <span className="block mt-1 text-[9px] leading-snug text-text-muted/60">
          {MIXED_PROVENANCE_NOTE}
        </span>
      )}
    </div>
  );
}

function PanelHeader({
  icon: Icon,
  label,
  children,
}: {
  icon: LucideIcon | ComponentType<{ size?: number; className?: string }>;
  label: string;
  children: ReactNode;
}) {
  return (
    <div className="rounded-lg bg-surface-elevated/60 border border-border/30 overflow-hidden">
      <div className="flex items-center gap-1.5 px-3 py-1.5 border-b border-border/30 bg-surface-highlight/30">
        <Icon size={11} className="text-text-muted" />
        <span className="text-[11px] font-medium text-text-secondary">{label}</span>
      </div>
      {children}
    </div>
  );
}

function ClientSortableHeader({
  label, column, sortColumn, sortDirection, onSort, align = 'left',
}: {
  label: string;
  column: ClientSortColumn;
  sortColumn: ClientSortColumn;
  sortDirection: SortDirection;
  onSort: (column: ClientSortColumn) => void;
  align?: 'left' | 'right';
}) {
  const isActive = sortColumn === column;
  const SortIcon = isActive ? (sortDirection === 'asc' ? ArrowUp : ArrowDown) : ArrowUpDown;

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

// Sortable header
function SortableHeader({
  label, column, sortColumn, sortDirection, onSort, align = 'left',
}: {
  label: string; column: SortColumn; sortColumn: SortColumn; sortDirection: SortDirection;
  onSort: (column: SortColumn) => void; align?: 'left' | 'right';
}) {
  const isActive = sortColumn === column;
  const SortIcon = isActive ? (sortDirection === 'asc' ? ArrowUp : ArrowDown) : ArrowUpDown;

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

export function DetachedMetricsPage() {
  return (
    <DetachedErrorBoundary>
      <DetachedMetricsPageContent />
    </DetachedErrorBoundary>
  );
}
