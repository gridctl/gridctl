// Pure data helpers and types for the metrics surfaces. Kept JSX-free and
// separate from metricsShared.tsx so Fast Refresh stays happy (a file may not
// export both components and plain functions). The bottom glance tab, the
// Metrics workspace, and the detached window all derive their numbers here so
// cost/token math is defined exactly once.
import { TOOL_NAME_DELIMITER } from '../../lib/constants';
import type {
  CostDataPoint,
  CostUsage,
  EffectiveModel,
  ModelProvenance,
  ModelShare,
  OptimizeFinding,
  TokenDataPoint,
  TokenMetricsResponse,
  CostMetricsResponse,
  TokenUsage,
  ToolUsageResponse,
} from '../../types';

export type SortDirection = 'asc' | 'desc';
// One sort vocabulary for both the client and server breakdown tables. Servers
// simply omit the `cost` column in the classic surfaces, so the wider type is
// harmless there.
export type BreakdownSortColumn = 'name' | 'input' | 'output' | 'total' | 'cost';

// A single row in a breakdown table. `cost` is optional: undefined means
// pricing-unknown (rendered as an em-dash, never $0). Tool rows carry the
// server/tool split (names collide across servers, so `name` is the unique
// server-prefixed key while the table renders the two parts) plus the call
// count for the inspector.
export interface BreakdownRow {
  name: string;
  input: number;
  output: number;
  total: number;
  cost?: number;
  server?: string;
  tool?: string;
  calls?: number;
}

// serverNames unions in stack servers with no recorded traffic (as zero rows
// with unknown cost) — an unused server is a first-class finding, so it must
// be selectable in the breakdown, not absent from it.
export function derivePerServerRows(
  tokenUsage: TokenUsage | null,
  costUsage?: CostUsage | null,
  serverNames?: string[],
): BreakdownRow[] {
  const usage = tokenUsage?.per_server ?? {};
  const names = new Set<string>([...Object.keys(usage), ...(serverNames ?? [])]);
  if (names.size === 0) return [];
  return Array.from(names).map((name) => ({
    name,
    input: usage[name]?.input_tokens ?? 0,
    output: usage[name]?.output_tokens ?? 0,
    total: usage[name]?.total_tokens ?? 0,
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

// Rows for the Metrics "Tools" scope, one per (server, tool) with recorded
// calls. Fed by GET /api/tools/usage rather than the status snapshot — the
// tools pipeline is the single per-tool data source everywhere in the UI.
export function derivePerToolRows(usage: ToolUsageResponse | null): BreakdownRow[] {
  if (!usage?.servers) return [];
  const rows: BreakdownRow[] = [];
  for (const [server, tools] of Object.entries(usage.servers)) {
    for (const [tool, stat] of Object.entries(tools)) {
      rows.push({
        name: `${server}${TOOL_NAME_DELIMITER}${tool}`,
        server,
        tool,
        calls: stat.calls,
        input: stat.inputTokens ?? 0,
        output: stat.outputTokens ?? 0,
        total: (stat.inputTokens ?? 0) + (stat.outputTokens ?? 0),
        cost: stat.costUsd,
      });
    }
  }
  return rows;
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

// One chart-time label format for every metrics series, so a focused entity
// series and its fleet context always merge on identical keys.
function chartTime(timestamp: string): string {
  return new Date(timestamp).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

// Bare point mappers — the single place a raw data point becomes a chart row.
// The full-response builders below and the inspector sparklines all sit on
// these, so the mapping is defined exactly once.
export function toTokenPoints(dps: TokenDataPoint[]) {
  return dps.map((dp) => ({
    time: chartTime(dp.timestamp),
    'Input Tokens': dp.input_tokens,
    'Output Tokens': dp.output_tokens,
  }));
}

export function toCostPoints(dps: CostDataPoint[]) {
  return dps.map((dp) => ({
    time: chartTime(dp.timestamp),
    'Cost (USD)': dp.usd,
  }));
}

export function buildTokenChartData(metricsData: TokenMetricsResponse | null) {
  return toTokenPoints(metricsData?.data_points ?? []);
}

export function buildCostChartData(costData: CostMetricsResponse | null) {
  return toCostPoints(costData?.data_points ?? []);
}

// Chart category labels for the focused (entity-selected) center charts. The
// fleet series keeps a fixed label so the dashed-context styling can key on it.
export const FLEET_TOKEN_CATEGORY = 'Fleet Total';
export const FLEET_COST_CATEGORY = 'Fleet';

// Focused chart rows: the selected entity's series merged with the fleet
// series as context, on the fleet's time spine. The join key is the RAW
// bucket timestamp — both rings bucket identically (same minute keys, same
// hour truncation on downsampled ranges), while the HH:MM display label
// repeats across days on 24h/7d and would collide. Every write lands in both
// the global and the per-entity ring, so entity buckets are a subset of
// global buckets; a fleet bucket missing from the entity series means the
// entity was idle then — a true zero, not an unknown — so it renders as 0
// rather than a gap. (One edge: a wrapped global ring can start later than a
// sparse entity's oldest in-window bucket, dropping those entity points from
// the spine; the fleet ring covers ~7 busy days, so this only affects the far
// tail of 7d.) Callers must NOT use these when the entity series is entirely
// empty (that state gets an honest note instead of a flat zero line).
export function buildFocusedTokenChartData(
  metricsData: TokenMetricsResponse | null,
  entityDps: TokenDataPoint[],
) {
  const byStamp = new Map(entityDps.map((dp) => [dp.timestamp, dp]));
  return (metricsData?.data_points ?? []).map((dp) => {
    const entity = byStamp.get(dp.timestamp);
    return {
      time: chartTime(dp.timestamp),
      'Input Tokens': entity?.input_tokens ?? 0,
      'Output Tokens': entity?.output_tokens ?? 0,
      [FLEET_TOKEN_CATEGORY]: dp.total_tokens,
    };
  });
}

export function buildFocusedCostChartData(
  costData: CostMetricsResponse | null,
  entityDps: CostDataPoint[],
) {
  const byStamp = new Map(entityDps.map((dp) => [dp.timestamp, dp.usd]));
  return (costData?.data_points ?? []).map((dp) => ({
    time: chartTime(dp.timestamp),
    'Cost (USD)': byStamp.get(dp.timestamp) ?? 0,
    [FLEET_COST_CATEGORY]: dp.usd,
  }));
}

// Window sums for a focused entity, feeding the focused-share line under the
// KPI row. A term is undefined (omitted, never rendered as 0) when the entity
// simply has no such series or no buckets in the window — a focused client
// has no token series at all, and "0 tokens" would contradict the honesty
// note beside the charts. Shares are against the fleet window totals
// (WindowTotals), cost share when both sides carry cost, else token share.
export interface FocusedTotals {
  tokens: number | undefined;
  costUSD: number | undefined;
  // 0-1 share of the fleet window; undefined when nothing is measurable or
  // the fleet denominator is zero.
  share: number | undefined;
}

export function deriveFocusedTotals(
  tokenDps: TokenDataPoint[] | undefined,
  costDps: CostDataPoint[] | undefined,
  windowTotals: WindowTotals,
): FocusedTotals {
  const tokens = tokenDps && tokenDps.length > 0
    ? tokenDps.reduce((sum, dp) => sum + dp.input_tokens + dp.output_tokens, 0)
    : undefined;
  const costUSD = costDps && costDps.length > 0
    ? costDps.reduce((sum, dp) => sum + dp.usd, 0)
    : undefined;
  let share: number | undefined;
  if (costUSD !== undefined && windowTotals.costUSD !== undefined && windowTotals.costUSD > 0) {
    share = costUSD / windowTotals.costUSD;
  } else if (tokens !== undefined && windowTotals.total > 0) {
    share = tokens / windowTotals.total;
  }
  return { tokens, costUSD, share };
}

// One row of the Models breakdown table: a model's slice of recorded cost
// plus which entities it priced and their provenance mix.
export interface ModelRow extends ModelShare {
  entities: string[];
  provenance: Record<ModelProvenance, number>;
}

// Aggregate model rows from the per-entity effective-model breakdowns. Each
// EffectiveModel.models[] partitions that entity's recorded cost, so summing
// across servers yields the global mix without double-counting (the client
// breakdowns partition the SAME total — servers are the single source,
// falling back to clients only when no server traffic priced yet). The
// entities and provenance columns come from the same single-tier iteration,
// so they can never double-count either. Both the mix bars and the Models
// table consume these rows (ModelRow is a ModelShare superset), so the two
// surfaces can never disagree on tier choice or totals.
export function aggregateModelRows(
  effectiveServerModels: Record<string, EffectiveModel>,
  effectiveClientModels: Record<string, EffectiveModel>,
): ModelRow[] {
  type Acc = { cost: number; entities: string[]; provenance: Record<ModelProvenance, number> };
  const sum = (source: Record<string, EffectiveModel>): Map<string, Acc> => {
    const byModel = new Map<string, Acc>();
    for (const [entity, em] of Object.entries(source)) {
      for (const m of em.models ?? []) {
        const acc = byModel.get(m.model) ?? {
          cost: 0,
          entities: [],
          provenance: { declared: 0, mixed: 0, none: 0 },
        };
        acc.cost += m.cost_usd;
        acc.entities.push(entity);
        acc.provenance[em.provenance] += 1;
        byModel.set(m.model, acc);
      }
    }
    return byModel;
  };
  let byModel = sum(effectiveServerModels);
  if (byModel.size === 0) byModel = sum(effectiveClientModels);
  const total = Array.from(byModel.values()).reduce((s, v) => s + v.cost, 0);
  if (total <= 0) return [];
  return Array.from(byModel.entries())
    .map(([model, acc]) => ({
      model,
      cost_usd: acc.cost,
      share: acc.cost / total,
      entities: acc.entities.sort(),
      provenance: acc.provenance,
    }))
    .sort((a, b) => b.cost_usd - a.cost_usd);
}

// Where an optimize finding's click should land, or null when it names no
// entity. The single source for both "is this row a button?" (SavingsCard)
// and the workspace's URL write, so the two can never disagree (a
// linkable-looking row whose click does nothing is worse than plain text).
// Tools rows key on the server-qualified name; a finding with a tool but no
// server is unlinkable.
export function findingTarget(
  finding: OptimizeFinding,
): { scope: 'servers' | 'tools'; selected: string } | null {
  if (finding.tool && finding.server) {
    return { scope: 'tools', selected: `${finding.server}${TOOL_NAME_DELIMITER}${finding.tool}` };
  }
  if (finding.server) return { scope: 'servers', selected: finding.server };
  return null;
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

// Totals for the active time window, summed from the same ranged series the
// charts draw. The counterpart to SessionKpis (cumulative since gateway
// start/restore): the KPI cards bind here so the range control owns every
// headline number, while the session totals render on their own labeled line.
export interface WindowTotals {
  input: number;
  output: number;
  total: number;
  // Sum of the cost series. undefined means the cost series has not loaded,
  // so the window cost is unknown (rendered as an em-dash, never $0.00); a
  // loaded-but-empty window legitimately sums to 0.
  costUSD: number | undefined;
  // True when neither series has a bucket in the window — the "no activity
  // in this window" state, distinct from a stack with no traffic ever.
  isEmpty: boolean;
}

export function deriveWindowTotals(
  metricsData: TokenMetricsResponse | null,
  costData: CostMetricsResponse | null,
): WindowTotals {
  // data_points is nullable in practice: the backend marshals an empty
  // downsampled range as null, which still means "loaded, nothing in it".
  const input = (metricsData?.data_points ?? []).reduce((sum, dp) => sum + dp.input_tokens, 0);
  const output = (metricsData?.data_points ?? []).reduce((sum, dp) => sum + dp.output_tokens, 0);
  const costUSD = costData
    ? (costData.data_points ?? []).reduce((sum, dp) => sum + dp.usd, 0)
    : undefined;
  return {
    input,
    output,
    total: input + output,
    costUSD,
    isEmpty:
      (metricsData?.data_points?.length ?? 0) === 0 && (costData?.data_points?.length ?? 0) === 0,
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
