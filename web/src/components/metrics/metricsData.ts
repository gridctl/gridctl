// Pure data helpers and types for the metrics surfaces. Kept JSX-free and
// separate from metricsShared.tsx so Fast Refresh stays happy (a file may not
// export both components and plain functions). The Metrics workspace and the
// detached window all derive their numbers here so token math is defined
// exactly once.
import { TOOL_NAME_DELIMITER } from '../../lib/constants';
import type {
  OptimizeFinding,
  TokenDataPoint,
  TokenMetricsResponse,
  TokenUsage,
  ToolUsageResponse,
} from '../../types';

export type SortDirection = 'asc' | 'desc';
// One sort vocabulary for both the client and server breakdown tables.
export type BreakdownSortColumn = 'name' | 'input' | 'output' | 'total';

// A single row in a breakdown table. Tool rows carry the server/tool split
// (names collide across servers, so `name` is the unique server-prefixed key
// while the table renders the two parts) plus the call count for the
// inspector.
export interface BreakdownRow {
  name: string;
  input: number;
  output: number;
  total: number;
  server?: string;
  tool?: string;
  calls?: number;
}

// serverNames unions in stack servers with no recorded traffic (as zero rows)
// — an unused server is a first-class finding, so it must be selectable in
// the breakdown, not absent from it.
export function derivePerServerRows(
  tokenUsage: TokenUsage | null,
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
  }));
}

export function derivePerClientRows(tokenUsage: TokenUsage | null): BreakdownRow[] {
  const tokenClients = tokenUsage?.per_client ?? {};
  return Object.keys(tokenClients).map((name) => ({
    name,
    input: tokenClients[name]?.input_tokens ?? 0,
    output: tokenClients[name]?.output_tokens ?? 0,
    total: tokenClients[name]?.total_tokens ?? 0,
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
    return dir * (a[column] - b[column]);
  });
}

// One chart-time label format for every metrics series, so a focused entity
// series and its fleet context always merge on identical keys.
function chartTime(timestamp: string): string {
  return new Date(timestamp).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

// Bare point mapper — the single place a raw data point becomes a chart row.
// The full-response builder below and the inspector sparklines all sit on
// this, so the mapping is defined exactly once.
export function toTokenPoints(dps: TokenDataPoint[]) {
  return dps.map((dp) => ({
    time: chartTime(dp.timestamp),
    'Input Tokens': dp.input_tokens,
    'Output Tokens': dp.output_tokens,
  }));
}

export function buildTokenChartData(metricsData: TokenMetricsResponse | null) {
  return toTokenPoints(metricsData?.data_points ?? []);
}

// Chart category label for the focused (entity-selected) center chart. The
// fleet series keeps a fixed label so the dashed-context styling can key on it.
export const FLEET_TOKEN_CATEGORY = 'Fleet Total';

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

// Window sums for a focused entity, feeding the focused-share line under the
// KPI row. Tokens are undefined (omitted, never rendered as 0) when the
// entity simply has no such series or no buckets in the window — a focused
// client has no token series at all, and "0 tokens" would contradict the
// honesty note beside the charts. Share is against the fleet window totals
// (WindowTotals).
export interface FocusedTotals {
  tokens: number | undefined;
  // 0-1 share of the fleet window; undefined when nothing is measurable or
  // the fleet denominator is zero.
  share: number | undefined;
}

export function deriveFocusedTotals(
  tokenDps: TokenDataPoint[] | undefined,
  windowTotals: WindowTotals,
): FocusedTotals {
  const tokens = tokenDps && tokenDps.length > 0
    ? tokenDps.reduce((sum, dp) => sum + dp.input_tokens + dp.output_tokens, 0)
    : undefined;
  let share: number | undefined;
  if (tokens !== undefined && windowTotals.total > 0) {
    share = tokens / windowTotals.total;
  }
  return { tokens, share };
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
  savingsPercent: number;
  savedTokens: number;
}

export function deriveSessionKpis(tokenUsage: TokenUsage | null): SessionKpis {
  return {
    input: tokenUsage?.session.input_tokens ?? 0,
    output: tokenUsage?.session.output_tokens ?? 0,
    total: tokenUsage?.session.total_tokens ?? 0,
    savingsPercent: tokenUsage?.format_savings.savings_percent ?? 0,
    savedTokens: tokenUsage?.format_savings.saved_tokens ?? 0,
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
  // True when the series has no bucket in the window — the "no activity in
  // this window" state, distinct from a stack with no traffic ever.
  isEmpty: boolean;
}

export function deriveWindowTotals(metricsData: TokenMetricsResponse | null): WindowTotals {
  // data_points is nullable in practice: the backend marshals an empty
  // downsampled range as null, which still means "loaded, nothing in it".
  const input = (metricsData?.data_points ?? []).reduce((sum, dp) => sum + dp.input_tokens, 0);
  const output = (metricsData?.data_points ?? []).reduce((sum, dp) => sum + dp.output_tokens, 0);
  return {
    input,
    output,
    total: input + output,
    isEmpty: (metricsData?.data_points?.length ?? 0) === 0,
  };
}

export function hasMetricsData(
  kpis: SessionKpis,
  metricsData: TokenMetricsResponse | null,
): boolean {
  return kpis.total > 0 || (metricsData?.data_points?.length ?? 0) > 0;
}
