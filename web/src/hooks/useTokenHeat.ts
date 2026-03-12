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
  const tokenUsage = useStackStore((s) => s.tokenUsage);

  return useMemo(() => {
    if (!showHeatMap || !tokenUsage?.per_server) return 0;

    const serverTokens = tokenUsage.per_server[serverName]?.total_tokens ?? 0;
    if (serverTokens === 0) return 0;

    // Find the max across all servers
    let max = 0;
    for (const counts of Object.values(tokenUsage.per_server)) {
      if (counts.total_tokens > max) max = counts.total_tokens;
    }

    if (max === 0) return 0;
    return serverTokens / max;
  }, [showHeatMap, tokenUsage, serverName]);
}
