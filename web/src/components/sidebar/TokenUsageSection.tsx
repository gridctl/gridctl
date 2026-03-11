import { useState, useEffect, useCallback, useRef, useMemo } from 'react';
import { ArrowDown } from 'lucide-react';
import { useStackStore } from '../../stores/useStackStore';
import { useUIStore } from '../../stores/useUIStore';
import { fetchTokenMetrics } from '../../lib/api';
import { formatCompactNumber } from '../../lib/format';
import { SparkChart } from '../chart/SparkChart';
import { POLLING } from '../../lib/constants';
import type { TokenDataPoint } from '../../types';

interface TokenUsageSectionProps {
  serverName: string;
}

export function TokenUsageSection({ serverName }: TokenUsageSectionProps) {
  const tokenUsage = useStackStore((s) => s.tokenUsage);
  const sidebarOpen = useUIStore((s) => s.sidebarOpen);
  const [sparkData, setSparkData] = useState<TokenDataPoint[]>([]);
  const intervalRef = useRef<number | null>(null);

  const serverTokens = tokenUsage?.per_server[serverName];

  // Fetch per-server time-series for sparkline (30m window)
  const loadSparkline = useCallback(async () => {
    try {
      const data = await fetchTokenMetrics('30m');
      const serverPoints = data.per_server?.[serverName];
      if (serverPoints) {
        setSparkData(serverPoints);
      }
    } catch {
      // Sparkline is best-effort — don't surface errors
    }
  }, [serverName]);

  useEffect(() => {
    if (!sidebarOpen || !serverTokens) return;

    loadSparkline();
    intervalRef.current = window.setInterval(loadSparkline, POLLING.METRICS);

    return () => {
      if (intervalRef.current) {
        clearInterval(intervalRef.current);
        intervalRef.current = null;
      }
    };
  }, [sidebarOpen, serverTokens, loadSparkline]);

  const chartData = useMemo(() => {
    if (!sparkData.length) return [];
    return sparkData.map((dp) => ({
      time: dp.timestamp,
      Input: dp.input_tokens,
      Output: dp.output_tokens,
    }));
  }, [sparkData]);

  // Don't render if no token data for this server
  if (!serverTokens || serverTokens.total_tokens === 0) return null;

  const savingsPercent = tokenUsage?.format_savings.savings_percent ?? 0;
  const savedTokens = tokenUsage?.format_savings.saved_tokens ?? 0;

  return (
    <div className="space-y-3">
      {/* Token counts */}
      <div className="space-y-2">
        <div className="flex justify-between items-center">
          <span className="text-sm text-text-muted">Input</span>
          <span className="text-sm text-secondary font-semibold tabular-nums">
            {formatCompactNumber(serverTokens.input_tokens)}
          </span>
        </div>
        <div className="flex justify-between items-center">
          <span className="text-sm text-text-muted">Output</span>
          <span className="text-sm text-primary font-semibold tabular-nums">
            {formatCompactNumber(serverTokens.output_tokens)}
          </span>
        </div>
        <div className="flex justify-between items-center">
          <span className="text-sm text-text-muted">Total</span>
          <span className="text-sm text-text-primary font-bold tabular-nums">
            {formatCompactNumber(serverTokens.total_tokens)}
          </span>
        </div>
      </div>

      {/* Sparkline */}
      {chartData.length > 1 && (
        <div className="pt-1">
          <span className="text-[9px] text-text-muted/60 uppercase tracking-wider block mb-1">Last 30 min</span>
          <div className="rounded-md bg-background/50 p-1.5" role="img" aria-label={`Token usage trend for ${serverName} over last 30 minutes`}>
            <SparkChart
              data={chartData}
              index="time"
              categories={["Input", "Output"]}
              colors={["teal", "amber"]}
              type="stacked"
            />
          </div>
        </div>
      )}

      {/* Format savings (conditional) */}
      {savingsPercent > 0 && (
        <div className="pt-1 border-t border-border/30">
          <div className="flex items-center gap-1.5 mb-2">
            <ArrowDown size={10} className="text-status-running" />
            <span className="text-[10px] text-text-muted uppercase tracking-wider">Format Savings</span>
          </div>
          <div className="flex items-baseline gap-2 mb-1.5">
            <span className="text-sm font-bold text-status-running tabular-nums">
              {Math.round(savingsPercent)}%
            </span>
            <span className="text-[10px] text-text-muted">
              {formatCompactNumber(savedTokens)} tokens saved
            </span>
          </div>
          {/* Savings bar */}
          <div
            className="h-1 rounded-full bg-surface-highlight overflow-hidden flex"
            role="progressbar"
            aria-valuenow={Math.round(savingsPercent)}
            aria-valuemin={0}
            aria-valuemax={100}
            aria-label={`${Math.round(savingsPercent)}% format savings`}
          >
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
  );
}

