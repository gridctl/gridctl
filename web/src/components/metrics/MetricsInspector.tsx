import { X, DollarSign } from 'lucide-react';
import { cn } from '../../lib/cn';
import { formatCompactNumber, formatUSD } from '../../lib/format';
import { AreaChart } from '../chart/AreaChart';
import { PaneAnchor } from '../inspector';
import { ClientModelCell } from '../pricing/ClientModelCell';
import { ServerModelCell } from '../pricing/ServerModelCell';
import {
  ATTRIBUTION_HINT,
  MIXED_PROVENANCE_NOTE,
  UNPRICED_NOTE,
  MODEL_PRECEDENCE_HINT,
} from '../pricing/constants';
import type { BreakdownRow } from './metricsData';
import type { CostDataPoint, EffectiveModel, TokenDataPoint } from '../../types';

export type MetricsInspectorScope = 'clients' | 'servers';

interface MetricsInspectorProps {
  scope: MetricsInspectorScope;
  // The selected entity's KPI row, or null to show the overview/legend.
  row: BreakdownRow | null;
  effective?: EffectiveModel;
  declaredModel?: string;
  defaultModel: string;
  costAttribution: boolean;
  onClientSaved: (client: string, model: string) => void;
  onServerSaved: (server: string, model: string) => void;
  onOpenManager: () => void;
  onClose: () => void;
  // Per-entity series for the inspector sparklines, when available.
  tokenPoints?: TokenDataPoint[];
  costPoints?: CostDataPoint[];
}

// MetricsInspector is the workspace right rail: a per-entity detail view for
// the selected client or server. It hosts the inline model editor (relocated
// here so the breakdown tables stay scannable), the entity's KPI numbers,
// per-entity sparklines, and the cost-provenance note. With nothing selected
// it falls back to a cost-provenance legend explaining the attribution model.
export function MetricsInspector({
  scope,
  row,
  effective,
  declaredModel,
  defaultModel,
  costAttribution,
  onClientSaved,
  onServerSaved,
  onOpenManager,
  onClose,
  tokenPoints,
  costPoints,
}: MetricsInspectorProps) {
  if (!row) return <InspectorOverview onOpenManager={onOpenManager} />;

  const tokenSeries = (tokenPoints ?? []).map((dp) => ({
    time: new Date(dp.timestamp).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
    'Input Tokens': dp.input_tokens,
    'Output Tokens': dp.output_tokens,
  }));
  const costSeries = (costPoints ?? []).map((dp) => ({
    time: new Date(dp.timestamp).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
    'Cost (USD)': dp.usd,
  }));
  const costSeriesHasData = costSeries.some((d) => d['Cost (USD)'] > 0);

  return (
    <aside className="relative h-full flex flex-col bg-surface-elevated border-l border-border">
      <PaneAnchor />
      <div className="flex-shrink-0 flex items-center gap-2 px-4 py-3 border-b border-border-subtle">
        <div className="min-w-0">
          <div className="text-[10px] uppercase tracking-[0.3em] text-text-muted/60">
            {scope === 'clients' ? 'client' : 'server'}
          </div>
          <div className="font-mono text-sm text-text-primary truncate" title={row.name}>
            {row.name}
          </div>
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
        {/* Pricing model editor */}
        <section className="space-y-1.5">
          <h3 className="text-[10px] uppercase tracking-[0.18em] text-text-muted/70">Pricing model</h3>
          <div title={MODEL_PRECEDENCE_HINT}>
            {scope === 'clients' ? (
              <ClientModelCell
                client={row.name}
                declaredModel={declaredModel}
                effective={effective}
                costAttribution={costAttribution}
                onSaved={onClientSaved}
                onOpenManager={onOpenManager}
                pickerAlign="left"
              />
            ) : (
              <ServerModelCell
                server={row.name}
                declaredModel={declaredModel}
                defaultModel={defaultModel}
                effective={effective}
                onSaved={onServerSaved}
                onOpenManager={onOpenManager}
                pickerAlign="left"
              />
            )}
          </div>
          {effective?.provenance === 'mixed' && (
            <p className="text-[10px] leading-snug text-text-muted/70">{MIXED_PROVENANCE_NOTE}</p>
          )}
          {effective?.provenance === 'none' && (
            <p className="text-[10px] leading-snug text-text-muted/70">{UNPRICED_NOTE}</p>
          )}
        </section>

        {/* KPI numbers */}
        <section className="grid grid-cols-2 gap-2">
          <InspectorStat label="Input" value={formatCompactNumber(row.input)} className="text-secondary" />
          <InspectorStat label="Output" value={formatCompactNumber(row.output)} className="text-primary" />
          <InspectorStat label="Total" value={formatCompactNumber(row.total)} className="text-text-primary" />
          <InspectorStat
            label="Cost · est."
            value={row.cost === undefined ? '—' : formatUSD(row.cost)}
            className={row.cost === undefined ? 'text-text-muted' : 'text-emerald-400'}
          />
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

        {/* Cost sparkline */}
        {costSeriesHasData && (
          <section className="space-y-1">
            <h3 className="text-[10px] uppercase tracking-[0.18em] text-text-muted/70 inline-flex items-center gap-1">
              <DollarSign size={10} className="text-emerald-400" /> Cost over time
            </h3>
            <div role="img" aria-label={`Estimated cost over time for ${row.name}`}>
              <AreaChart
                data={costSeries}
                index="time"
                categories={['Cost (USD)']}
                colors={['emerald']}
                type="default"
                fill="gradient"
                showLegend={false}
                showGridLines={false}
                showYAxis={false}
                valueFormatter={(v: number) => formatUSD(v)}
                className="h-20"
              />
            </div>
          </section>
        )}
      </div>
    </aside>
  );
}

function InspectorStat({ label, value, className }: { label: string; value: string; className?: string }) {
  return (
    <div className="rounded-lg bg-surface-elevated/60 border border-border/30 p-2.5">
      <span className="text-[9px] text-text-muted uppercase tracking-wider block mb-0.5">{label}</span>
      <span className={cn('text-sm font-bold tabular-nums', className)}>{value}</span>
    </div>
  );
}

// Shown when nothing is selected — a cost-provenance legend rather than an
// empty rail, carrying the same honesty copy the cards use.
function InspectorOverview({ onOpenManager }: { onOpenManager: () => void }) {
  return (
    <aside className="relative h-full flex flex-col bg-surface-elevated border-l border-border">
      <PaneAnchor />
      <div className="flex-shrink-0 px-4 py-3 border-b border-border-subtle">
        <div className="text-[10px] uppercase tracking-[0.3em] text-text-muted/60">about cost</div>
      </div>
      <div className="flex-1 min-h-0 overflow-y-auto scrollbar-dark p-4 space-y-3 text-[11px] leading-relaxed text-text-muted">
        <p>
          Select a client or server to inspect its tokens, estimated cost, and pricing model.
        </p>
        <p className="text-text-secondary">{ATTRIBUTION_HINT}.</p>
        <div className="space-y-1.5 pt-1">
          <p className="text-[10px] uppercase tracking-[0.18em] text-text-muted/70">provenance</p>
          <p><span className="font-mono text-text-secondary">declared</span> — one model priced all recorded cost.</p>
          <p><span className="font-mono text-text-secondary">mixed</span> — {MIXED_PROVENANCE_NOTE}</p>
          <p><span className="font-mono text-text-secondary">none</span> — {UNPRICED_NOTE}</p>
        </div>
        <button
          type="button"
          onClick={onOpenManager}
          className="mt-2 inline-flex items-center gap-1.5 rounded-md border border-primary/30 bg-primary/10 px-2.5 py-1 text-[10px] font-medium text-primary hover:bg-primary/20 transition-colors"
        >
          <DollarSign size={11} /> Edit pricing models
        </button>
      </div>
    </aside>
  );
}

export default MetricsInspector;
