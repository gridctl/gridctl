import type { ClientScopeResult } from '../types';

// summarizeClientReach condenses a client's backend-computed access scope into
// the shape the inspector renders. A client is "scoped" only when a `clients:`
// block is configured AND it does not reach the full surface; otherwise it is
// unscoped and reaches every server (the legacy, no-policy default).
export interface ClientReach {
  scoped: boolean; // configured && !unscoped
  reachableCount: number; // servers this client can reach
  totalCount: number; // total MCP servers in the stack
  servers: string[]; // reachable server names (sorted)
}

export function summarizeClientReach(
  scope: ClientScopeResult | undefined,
  allServerNames: string[],
): ClientReach {
  const total = allServerNames.length;
  const sortedAll = [...allServerNames].sort((a, b) => a.localeCompare(b));

  const scoped = Boolean(scope?.configured) && !scope?.unscoped;
  if (!scoped) {
    // Unscoped (or no policy configured): reaches every server.
    return { scoped: false, reachableCount: total, totalCount: total, servers: sortedAll };
  }

  const servers = [...(scope?.servers ?? [])].sort((a, b) => a.localeCompare(b));
  return { scoped: true, reachableCount: servers.length, totalCount: total, servers };
}
