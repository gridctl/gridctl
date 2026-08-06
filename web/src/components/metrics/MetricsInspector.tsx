import { X } from 'lucide-react';
import { formatCompactNumber } from '../../lib/format';
import { AreaChart } from '../chart/AreaChart';
import { InspectorStat } from './metricsShared';
import { PaneAnchor } from '../inspector';
import { toTokenPoints, type BreakdownRow } from './metricsData';
import type { TokenDataPoint } from '../../types';

export type MetricsInspectorScope = 'clients' | 'servers' | 'tools';

interface MetricsInspectorProps {
  scope: MetricsInspectorScope;
  // The selected entity's KPI row, or null to show the overview note.
  row: BreakdownRow | null;
  onClose: () => void;
  // Per-entity series for the inspector sparkline, when available.
  tokenPoints?: TokenDataPoint[];
}

// MetricsInspector is the workspace right rail: a per-entity detail view for
// the selected client, server, or tool — the entity's KPI numbers plus a
// per-entity token sparkline where a series exists. With nothing selected it
// falls back to a short usage note.
export function MetricsInspector({ scope, row, onClose, tokenPoints }: MetricsInspectorProps) {
  if (!row) return <InspectorOverview />;

  const tokenSeries = toTokenPoints(tokenPoints ?? []);

  return (
    <aside className="relative h-full flex flex-col bg-surface-elevated border-l border-border">
      <PaneAnchor />
      <div className="flex-shrink-0 flex items-center gap-2 px-4 py-3 border-b border-border-subtle">
        <div className="min-w-0">
          <div className="text-[10px] uppercase tracking-[0.3em] text-text-muted/60">
            {scope === 'clients' ? 'client' : scope === 'tools' ? 'tool' : 'server'}
          </div>
          <div className="font-mono text-sm text-text-primary truncate" title={row.name}>
            {row.tool ?? row.name}
          </div>
          {scope === 'tools' && row.server && (
            <div className="font-mono text-[10px] text-text-muted truncate" title={row.server}>
              {row.server}
            </div>
          )}
        </div>
        <button
          onClick={onClose}
          aria-label="Close inspector"
          className="ml-auto p-1 rounded hover:bg-surface-highlight transition-colors"
        >
          <X size={14} className="text-text-muted" />
        </button>
      </div>

      <div className="flex-1 min-h-0 overflow-y-auto scrollbar-dark p-4 space-y-4">
        {/* KPI numbers */}
        <section className="grid grid-cols-2 gap-2">
          {row.calls !== undefined && (
            <InspectorStat label="Calls" value={formatCompactNumber(row.calls)} className="text-text-primary" />
          )}
          <InspectorStat label="Input" value={formatCompactNumber(row.input)} className="text-secondary" />
          <InspectorStat label="Output" value={formatCompactNumber(row.output)} className="text-primary" />
          <InspectorStat label="Total" value={formatCompactNumber(row.total)} className="text-text-primary" />
        </section>

        {/* Token sparkline */}
        {tokenSeries.length > 0 && (
          <section className="space-y-1">
            <h3 className="text-[10px] uppercase tracking-[0.18em] text-text-muted/70">Tokens over time</h3>
            <div role="img" aria-label={`Token usage over time for ${row.name}`}>
              <AreaChart
                data={tokenSeries}
                index="time"
                categories={['Input Tokens', 'Output Tokens']}
                colors={['teal', 'amber']}
                type="stacked"
                fill="gradient"
                showLegend={false}
                showGridLines={false}
                showYAxis={false}
                valueFormatter={(v: number) => formatCompactNumber(v)}
                className="h-24"
              />
            </div>
          </section>
        )}

        {/* When no time series can render, say why instead of silently
            omitting the section — the KPI numbers above are session totals,
            so a bare inspector otherwise implies the entity has no history at
            all. Clients get structural wording: no per-client token series
            exists. */}
        {tokenSeries.length === 0 && (
          <p className="text-[10px] leading-snug text-text-muted/70">
            {scope === 'tools'
              ? 'Per-tool time series is not recorded yet; the numbers above are session totals.'
              : scope === 'clients'
                ? 'Per-client token series is not recorded; the numbers above are session totals.'
                : `No samples for ${row.name} in this window. The numbers above are session totals.`}
          </p>
        )}
      </div>
    </aside>
  );
}

// Shown when nothing is selected — a short orientation note rather than an
// empty rail.
function InspectorOverview() {
  return (
    <aside className="relative h-full flex flex-col bg-surface-elevated border-l border-border">
      <div className="flex-shrink-0 px-4 py-3 border-b border-border-subtle">
        <div className="text-[10px] uppercase tracking-[0.3em] text-text-muted/60">inspector</div>
      </div>
      <div className="flex-1 min-h-0 overflow-y-auto scrollbar-dark p-4 space-y-3 text-[11px] leading-relaxed text-text-muted">
        <p>Select a client, server, or tool to inspect its token usage and call activity.</p>
        <p>
          Numbers here are measured at the gateway: tokens counted on tool arguments and results,
          calls counted per tool. Servers additionally record a windowed time series.
        </p>
      </div>
    </aside>
  );
}

export default MetricsInspector;
