/**
 * Compact card state derivation for Stack canvas server and resource nodes.
 *
 * Pure functions and metadata shared by the compact card glyphs and the
 * canvas legend, so the two cannot drift apart.
 */

import type { MCPServerNodeData, ResourceNodeData } from '../../types';

export type CompactGlyphKind =
  | 'disabled'
  | 'needs-auth'
  | 'error'
  | 'restarting'
  | 'initializing'
  | 'stopped'
  | 'idle'
  | 'drift'
  | 'blocked';

type CardData = MCPServerNodeData | ResourceNodeData;

/** State glyph for a compact card, or null when the node is running quietly. */
export function compactStateGlyph(data: CardData): CompactGlyphKind | null {
  if (data.type === 'mcp-server' && data.enabled === false) return 'disabled';
  if (data.status === 'running') return null;
  return data.status;
}

/** Pin glyph for a compact server card. Blocked wins over drift. */
export function compactPinGlyph(data: CardData): CompactGlyphKind | null {
  if (data.type !== 'mcp-server') return null;
  if (data.pinStatus === 'blocked') return 'blocked';
  if (data.pinStatus === 'drift') return 'drift';
  return null;
}

export const GLYPH_META: Record<CompactGlyphKind, { label: string; description: string }> = {
  disabled: { label: 'Disabled', description: 'Turned off in the stack; not registered with the gateway' },
  'needs-auth': { label: 'Needs authorization', description: 'Waiting for an OAuth login; select it and use Authorize' },
  error: { label: 'Error', description: 'Health check or registration failed; hover for the reason' },
  restarting: { label: 'Restarting', description: 'Recovering after an exit; hover for the attempt and retry' },
  initializing: { label: 'Initializing', description: 'Starting up and not serving tools yet' },
  stopped: { label: 'Stopped', description: 'Not running' },
  idle: { label: 'Idle', description: 'Autoscaled to zero replicas' },
  drift: { label: 'Schema drift', description: 'Tool definitions changed since pinning; click to review in Pins' },
  blocked: { label: 'Blocked', description: 'Schema pin enforcement refuses its tool calls until the drift is reviewed' },
};

export function retryInSeconds(iso: string | undefined, now: number): string | undefined {
  if (!iso) return undefined;
  const at = Date.parse(iso);
  if (Number.isNaN(at)) return undefined;
  const seconds = Math.max(0, Math.round((at - now) / 1000));
  return `${seconds}s`;
}

export function mcpHealthSummary(data: MCPServerNodeData, now = Date.now()): string {
  const replicas = data.replicas ?? [];
  const exited = replicas.find((replica) => replica.exit);
  const restarting = replicas.find((replica) => replica.state === 'restarting');
  const parts: string[] = [];
  if (exited?.exit) {
    parts.push(exited.exit.oomKilled
      ? `exited ${exited.exit.code}, OOM killed`
      : `exited ${exited.exit.code}`);
  }
  if (restarting) {
    const attempt = restarting.restartAttempts ?? 0;
    const retry = retryInSeconds(restarting.nextRetryAt, now);
    parts.push(retry
      ? `restarting (attempt ${attempt}, retry in ${retry})`
      : `restarting (attempt ${attempt})`);
  } else {
    parts.push(data.healthError || 'Health check failed');
  }
  return parts.join('; ');
}
