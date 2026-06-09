import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { BarChart3, AlertCircle, ArrowRight } from 'lucide-react';
import { useUIStore } from '../../stores/useUIStore';
import { useStackStore } from '../../stores/useStackStore';
import { useWindowManager } from '../../hooks/useWindowManager';
import { useMetricsSeries, type MetricsTimeRange } from '../../hooks/useMetricsSeries';
import { PopoutButton } from '../ui/PopoutButton';
import { PersistedFromMarker } from '../telemetry/PersistedFromMarker';
import { MetricsControls } from './MetricsControls';
import { MetricsKpiRow, TokenChart } from './metricsShared';
import { buildTokenChartData, deriveSessionKpis, hasMetricsData } from './metricsData';

// MetricsTab is the bottom-panel glance surface: the session KPI row and a
// single token sparkline, sized to coexist with the canvas. The full
// breakdown (cost chart, per-client / per-server / model tables, attribution
// editing) lives in the Metrics workspace — "View all" routes there. All three
// surfaces (this tab, the workspace, the detached window) render the same
// shared atoms from metricsShared so cost stays defined in one place.
export function MetricsTab() {
  const navigate = useNavigate();
  const bottomPanelOpen = useUIStore((s) => s.bottomPanelOpen);
  const bottomPanelTab = useUIStore((s) => s.bottomPanelTab);
  const metricsDetached = useUIStore((s) => s.metricsDetached);
  const setPricingManagerOpen = useUIStore((s) => s.setPricingManagerOpen);

  const tokenUsage = useStackStore((s) => s.tokenUsage);
  const costUsage = useStackStore((s) => s.costUsage);
  const costAttribution = useStackStore((s) => s.costAttribution);
  const effectiveClientModels = useStackStore((s) => s.effectiveClientModels);
  const effectiveServerModels = useStackStore((s) => s.effectiveServerModels);

  const { openDetachedWindow } = useWindowManager();
  const [timeRange, setTimeRange] = useState<MetricsTimeRange>('live');
  const [isPaused, setIsPaused] = useState(false);

  const isVisible = bottomPanelOpen && bottomPanelTab === 'metrics';
  const { metricsData, costData, isLoading, error, reload, clear } = useMetricsSeries({
    timeRange,
    enabled: isVisible,
    paused: isPaused,
  });

  const kpis = deriveSessionKpis(
    tokenUsage,
    costUsage,
    costAttribution,
    effectiveClientModels,
    effectiveServerModels,
  );
  const chartData = buildTokenChartData(metricsData);
  const hasData = hasMetricsData(kpis, metricsData, costData);

  return (
    <div className="flex flex-col h-full">
      <div className="flex items-center px-4 h-9 flex-shrink-0 border-b border-border/30 bg-surface-elevated/20">
        <MetricsControls
          className="w-full"
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
          right={<PopoutButton onClick={() => openDetachedWindow('metrics')} disabled={metricsDetached} />}
        />
      </div>

      <div className="flex-1 overflow-auto scrollbar-dark min-h-0 p-4">
        {isLoading && !metricsData && (
          <div className="space-y-4 animate-pulse">
            <div className="grid grid-cols-4 gap-3">
              {[1, 2, 3, 4].map((i) => (
                <div key={i} className="h-16 rounded-lg bg-surface-elevated/60 border border-border/30" />
              ))}
            </div>
            <div className="h-36 rounded-lg bg-surface-elevated/60 border border-border/30" />
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
            <BarChart3 size={24} className="text-text-muted/30" />
            <span className="text-xs">No token data yet</span>
            <span className="text-[10px] text-text-muted/60">Metrics will appear after tool calls</span>
          </div>
        )}

        {!error && hasData && (
          <div className="space-y-4">
            <PersistedFromMarker serverName={null} signal="metrics" />
            <MetricsKpiRow kpis={kpis} />
            <TokenChart data={chartData} metricsData={metricsData} />
            <button
              type="button"
              onClick={() => navigate('/metrics')}
              className="w-full flex items-center justify-center gap-1.5 rounded-lg border border-border/40 bg-background/40 py-2 text-[11px] font-medium text-text-secondary hover:text-text-primary hover:border-primary/40 transition-colors"
            >
              View all in Metrics
              <ArrowRight size={12} />
            </button>
          </div>
        )}
      </div>
    </div>
  );
}
