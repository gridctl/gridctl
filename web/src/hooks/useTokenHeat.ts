import { useMemo } from 'react';
import { useStackStore } from '../stores/useStackStore';
import { useUIStore } from '../stores/useUIStore';

/**
 * Returns a heat intensity (0-1) for a given server name based on its
 * token usage relative to the highest-usage server. Returns 0 when
 * the heat map is disabled or the server has no token data.
 */
export function useTokenHeat(serverName: string): number {
  const showHeatMap = useUIStore((s) => s.showHeatMap);
  // While the heat map is off, ignore tokenUsage so a poll does not re-render
  // every server node. undefined is a stable selector result.
  const perServer = useStackStore((s) => (showHeatMap ? s.tokenUsage?.per_server : undefined));

  return useMemo(() => {
    if (!showHeatMap || !perServer) return 0;

    const serverTokens = perServer[serverName]?.total_tokens ?? 0;
    if (serverTokens === 0) return 0;

    // Find the max across all servers
    let max = 0;
    for (const counts of Object.values(perServer)) {
      if (counts.total_tokens > max) max = counts.total_tokens;
    }

    if (max === 0) return 0;
    return serverTokens / max;
  }, [showHeatMap, perServer, serverName]);
}
