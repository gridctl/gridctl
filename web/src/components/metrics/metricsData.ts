// Pure data helpers and types for the metrics surfaces. Kept JSX-free and
// separate from metricsShared.tsx so Fast Refresh stays happy (a file may not
// export both components and plain functions). The bottom glance tab, the
// Metrics workspace, and the detached window all derive their numbers here so
// cost/token math is defined exactly once.
import type {
  CostUsage,
  EffectiveModel,
  ModelShare,
  TokenMetricsResponse,
  CostMetricsResponse,
  TokenUsage,
} from '../../types';

export type SortDirection = 'asc' | 'desc';
// One sort vocabulary for both the client and server breakdown tables. Servers
// simply omit the `cost` column in the classic surfaces, so the wider type is
// harmless there.
export type BreakdownSortColumn = 'name' | 'input' | 'output' | 'total' | 'cost';

// A single row in a breakdown table. `cost` is optional: undefined means
// pricing-unknown (rendered as an em-dash, never $0).
export interface BreakdownRow {
  name: string;
  input: number;
  output: number;
  total: number;
  cost?: number;
}

export function derivePerServerRows(
  tokenUsage: TokenUsage | null,
  costUsage?: CostUsage | null,
): BreakdownRow[] {
  if (!tokenUsage?.per_server) return [];
  return Object.entries(tokenUsage.per_server).map(([name, counts]) => ({
    name,
    input: counts.input_tokens,
    output: counts.output_tokens,
    total: counts.total_tokens,
    cost: costUsage?.per_server?.[name]?.total_usd,
  }));
}

export function derivePerClientRows(
  tokenUsage: TokenUsage | null,
  costUsage: CostUsage | null,
): BreakdownRow[] {
  const tokenClients = tokenUsage?.per_client ?? {};
  const costClients = costUsage?.per_client ?? {};
  const names = new Set<string>([...Object.keys(tokenClients), ...Object.keys(costClients)]);
  return Array.from(names).map((name) => ({
    name,
    input: tokenClients[name]?.input_tokens ?? 0,
    output: tokenClients[name]?.output_tokens ?? 0,
    total: tokenClients[name]?.total_tokens ?? 0,
    cost: costClients[name]?.total_usd,
  }));
}

export function sortBreakdownRows(
  rows: BreakdownRow[],
  column: BreakdownSortColumn,
  direction: SortDirection,
): BreakdownRow[] {
  const dir = direction === 'asc' ? 1 : -1;
  return [...rows].sort((a, b) => {
    if (column === 'name') return dir * a.name.localeCompare(b.name);
    if (column === 'cost') {
      // Unknown cost sinks on descending, floats on ascending.
      const aCost = a.cost ?? -Infinity;
      const bCost = b.cost ?? -Infinity;
      return dir * (aCost - bCost);
    }
    return dir * (a[column] - b[column]);
  });
}

export function buildTokenChartData(metricsData: TokenMetricsResponse | null) {
  if (!metricsData?.data_points) return [];
  return metricsData.data_points.map((dp) => ({
    time: new Date(dp.timestamp).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
    'Input Tokens': dp.input_tokens,
    'Output Tokens': dp.output_tokens,
  }));
}

export function buildCostChartData(costData: CostMetricsResponse | null) {
  if (!costData?.data_points) return [];
  return costData.data_points.map((dp) => ({
    time: new Date(dp.timestamp).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
    'Cost (USD)': dp.usd,
  }));
}

// Aggregate a model→cost mix from the per-entity effective-model breakdowns.
// Each EffectiveModel.models[] partitions that entity's recorded cost, so
// summing across servers yields the global mix without double-counting (the
// client breakdowns partition the SAME total — we use servers as the single
// source, falling back to clients when no server traffic priced yet).
export function aggregateModelMix(
  effectiveServerModels: Record<string, EffectiveModel>,
  effectiveClientModels: Record<string, EffectiveModel>,
): ModelShare[] {
  const sum = (source: Record<string, EffectiveModel>): Map<string, number> => {
    const byModel = new Map<string, number>();
    for (const em of Object.values(source)) {
      for (const m of em.models ?? []) {
        byModel.set(m.model, (byModel.get(m.model) ?? 0) + m.cost_usd);
      }
    }
    return byModel;
  };
  let byModel = sum(effectiveServerModels);
  if (byModel.size === 0) byModel = sum(effectiveClientModels);
  const total = Array.from(byModel.values()).reduce((s, v) => s + v, 0);
  if (total <= 0) return [];
  return Array.from(byModel.entries())
    .map(([model, cost_usd]) => ({ model, cost_usd, share: cost_usd / total }))
    .sort((a, b) => b.cost_usd - a.cost_usd);
}

// Session-level KPI bundle, derived once and shared by every surface's KPI row.
export interface SessionKpis {
  input: number;
  output: number;
  total: number;
  costUSD: number | undefined;
  hasCost: boolean;
  savingsPercent: number;
  savedTokens: number;
  showAttributionHint: boolean;
  hasMixedProvenance: boolean;
}

export function deriveSessionKpis(
  tokenUsage: TokenUsage | null,
  costUsage: CostUsage | null,
  costAttribution: boolean,
  effectiveClientModels: Record<string, EffectiveModel>,
  effectiveServerModels: Record<string, EffectiveModel>,
): SessionKpis {
  const costUSD = costUsage?.session.total_usd;
  const hasCost = costUSD !== undefined;
  const anyMixed = (m: Record<string, EffectiveModel>) =>
    Object.values(m).some((e) => e.provenance === 'mixed');
  return {
    input: tokenUsage?.session.input_tokens ?? 0,
    output: tokenUsage?.session.output_tokens ?? 0,
    total: tokenUsage?.session.total_tokens ?? 0,
    costUSD,
    hasCost,
    savingsPercent: tokenUsage?.format_savings.savings_percent ?? 0,
    savedTokens: tokenUsage?.format_savings.saved_tokens ?? 0,
    // Without attribution the gateway cannot price calls, so explain the
    // config requirement instead of leaving a bare dash/$0.00.
    showAttributionHint: !costAttribution && !(costUSD && costUSD > 0),
    hasMixedProvenance: anyMixed(effectiveClientModels) || anyMixed(effectiveServerModels),
  };
}

export function hasMetricsData(
  kpis: SessionKpis,
  metricsData: TokenMetricsResponse | null,
  costData: CostMetricsResponse | null,
): boolean {
  return (
    kpis.total > 0 ||
    (metricsData?.data_points?.length ?? 0) > 0 ||
    (costData?.data_points?.length ?? 0) > 0
  );
}
