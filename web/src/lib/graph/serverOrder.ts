/**
 * Attention order for the Stack canvas server zone.
 *
 * Servers that need an operator come first so they sit at the top of the
 * column nearest the gateway. The sort is stable, so servers keep their API
 * (name) order inside a bucket.
 */

import type { MCPServerStatus } from '../../types';
import { getMCPServerStatus } from './nodes';
import type { ServerPinState } from './nodes';

type PinState = Pick<ServerPinState, 'status'>;

/**
 * Bucket for one server, lower first:
 * 0 unhealthy, restarting, or pin-blocked; 1 needs authorization; 2 pin
 * drift; 3 everything else. A disabled server is always 3.
 */
export function attentionBucket(
  server: MCPServerStatus,
  pins: Record<string, PinState> | null,
): number {
  const status = getMCPServerStatus(server);
  if (status === 'disabled') return 3;
  const pinStatus = pins?.[server.name]?.status;
  if (status === 'error' || status === 'restarting' || pinStatus === 'blocked') return 0;
  if (status === 'needs-auth') return 1;
  if (pinStatus === 'drift') return 2;
  return 3;
}

/**
 * Return the servers stable-sorted by attention bucket. The input is not
 * mutated; when no server is promoted the input array itself is returned.
 */
export function orderServersByAttention(
  servers: MCPServerStatus[],
  pins: Record<string, PinState> | null,
): MCPServerStatus[] {
  const buckets = servers.map((server) => attentionBucket(server, pins));
  if (buckets.every((bucket) => bucket === 3)) return servers;
  return servers
    .map((server, index) => ({ server, index, bucket: buckets[index] }))
    .sort((a, b) => a.bucket - b.bucket || a.index - b.index)
    .map((entry) => entry.server);
}
