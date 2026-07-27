import type { MCPServerStatus, ToolUsageStat } from '../types';
import { effectiveEnabledTools, unusedEnabledTools } from './toolAudit';

// Fleet bulk-action planning. Pure functions (no React, no clock) so they're
// unit-testable and memoizable in the workspace. A plan resolves an action +
// pattern over a set of servers into the concrete batch payload, the resolved
// match count to echo before acting, and the servers that can't be changed.

export type BulkAction = 'expose-all' | 'hide-pattern' | 'disable-unused';

// Usage snapshot context for the disable-unused action: the per-server maps
// from GET /api/tools/usage plus the audit window and the snapshot's fetch
// time as "now" (an injected clock, matching the toolAudit conventions).
export interface BulkUsageContext {
  usage: Record<string, Record<string, ToolUsageStat>> | undefined;
  windowMs: number;
  now: number;
}

// globToRegExp converts a shell-style glob (`*` = any run, `?` = one char) into
// an anchored RegExp. All other regex metacharacters are escaped so a pattern
// like "delete_*" matches literally except for the wildcard.
export function globToRegExp(pattern: string): RegExp {
  let out = '';
  for (const ch of pattern) {
    if (ch === '*') out += '.*';
    else if (ch === '?') out += '.';
    else out += ch.replace(/[.+^${}()|[\]\\]/g, '\\$&');
  }
  return new RegExp(`^${out}$`);
}

export interface BulkPlanEntry {
  name: string;
  // The new whitelist to persist for this server. [] = expose all (the kept
  // set covers every advertised tool).
  tools: string[];
  // Tool names this change removes from exposure (for the summary).
  hidden: string[];
}

export interface BulkPlan {
  // Servers that actually change — the source of the batch payload. Servers
  // already in the target state are omitted so the batch stays minimal.
  entries: BulkPlanEntry[];
  // Total tools the pattern would hide across all entries (the echoed count).
  matchedTools: number;
  // Servers skipped because the action would hide every exposed tool. The
  // whitelist model can't express "expose nothing" ([] = expose all), so we
  // refuse rather than silently re-expose everything.
  blocked: string[];
}

function sortedUnique(names: Iterable<string>): string[] {
  return [...new Set(names)].sort();
}

// planBulkAction resolves the action over `servers` (already scoped by the
// caller to all servers or a single one).
//
// expose-all: clears the whitelist (persist []) on servers that currently
// restrict tools; servers already exposing everything are unchanged.
//
// hide-pattern: removes currently-exposed tools matching `pattern` from each
// server, persisting the kept set as an explicit whitelist. A server whose
// every exposed tool matches is blocked (see BulkPlan.blocked).
//
// disable-unused: removes exposed tools with no recorded calls inside
// `usageCtx`'s window. Without a usage snapshot the plan is empty (never
// guess unused from missing data). A server whose every exposed tool is
// unused is blocked: the same [] = expose-all refusal as hide-pattern.
export function planBulkAction(
  servers: MCPServerStatus[],
  action: BulkAction,
  pattern: string,
  usageCtx?: BulkUsageContext,
): BulkPlan {
  const entries: BulkPlanEntry[] = [];
  const blocked: string[] = [];
  let matchedTools = 0;

  if (action === 'expose-all') {
    for (const server of servers) {
      const restricts = (server.toolWhitelist?.length ?? 0) > 0;
      if (!restricts) continue; // already exposing all
      entries.push({ name: server.name, tools: [], hidden: [] });
    }
    return { entries, matchedTools, blocked };
  }

  if (action === 'disable-unused') {
    if (!usageCtx) return { entries, matchedTools, blocked };
    for (const server of servers) {
      const unused = unusedEnabledTools(
        server,
        usageCtx.usage?.[server.name],
        usageCtx.windowMs,
        usageCtx.now,
      );
      if (unused.length === 0) continue;
      const drop = new Set(unused);
      const kept = [...effectiveEnabledTools(server)].filter((t) => !drop.has(t));
      if (kept.length === 0) {
        blocked.push(server.name);
        continue;
      }
      // Counted only for servers that actually change: the copy pairs this
      // number with entries.length ("Disables N tools across M servers"), so
      // including blocked servers would overstate what the apply will do.
      matchedTools += unused.length;
      entries.push({ name: server.name, tools: sortedUnique(kept), hidden: sortedUnique(unused) });
    }
    return { entries, matchedTools, blocked };
  }

  // hide-pattern
  const trimmed = pattern.trim();
  if (!trimmed) return { entries, matchedTools, blocked };
  const re = globToRegExp(trimmed);

  for (const server of servers) {
    const exposed = effectiveEnabledTools(server);
    const hidden = [...exposed].filter((t) => re.test(t));
    if (hidden.length === 0) continue; // nothing matches on this server
    matchedTools += hidden.length;

    const kept = [...exposed].filter((t) => !re.test(t));
    if (kept.length === 0) {
      // Hiding these would leave zero exposed tools, which [] can't express.
      blocked.push(server.name);
      continue;
    }
    // kept is necessarily a strict subset of advertised (we removed ≥1 exposed
    // tool), so it's always a concrete whitelist — never the expose-all [].
    entries.push({ name: server.name, tools: sortedUnique(kept), hidden: sortedUnique(hidden) });
  }

  return { entries, matchedTools, blocked };
}
