// Subtle inline divider rendered above the first entry of a Logs /
// Metrics / Traces tab when the active server has telemetry persistence
// enabled AND inventory shows files exist for the matching signal. The
// boundary is the inventory record's `oldestTime`, which marks the
// earliest entry that came from disk. Live entries appended above the
// marker have no special treatment — operators can tell what's "new"
// from timestamps.
import { useMemo } from 'react';
import { formatDateOnly } from '../../lib/format-bytes';
import { effectiveSignal } from '../../lib/telemetry-config';
import {
  inventoryByServer,
  useInventory,
  useTelemetryConfig,
} from '../../stores/useTelemetryStore';
import type { TelemetrySignal } from '../../types';

interface Props {
  // serverName scopes the boundary check to a single server's persistence
  // configuration. Pass null for the aggregated tab views (Metrics,
  // Traces) where the rendered data spans all servers — the marker then
  // matches against any server with the signal effectively enabled.
  serverName: string | null | undefined;
  signal: TelemetrySignal;
}

export function PersistedFromMarker({ serverName, signal }: Props) {
  const config = useTelemetryConfig();
  const inventory = useInventory();

  const date = useMemo(() => {
    let records = inventory.filter((r) => r.signal === signal);
    if (serverName) {
      if (!effectiveSignal(config, serverName, signal)) return null;
      records = inventoryByServer(records, serverName);
    } else {
      // Aggregated mode: keep records whose owning server has the signal
      // effectively enabled. Shielding the marker behind the resolved
      // config (rather than mere file existence) prevents stale files
      // from a previously-enabled server from advertising as live.
      records = records.filter((r) => effectiveSignal(config, r.server, signal));
    }
    if (records.length === 0) return null;
    // Pick the earliest oldestTime across rotated siblings — we want the
    // boundary at the very beginning of the on-disk window, not the last
    // rotated file.
    const earliest = records.reduce<string | null>((acc, r) => {
      if (!acc) return r.oldestTime;
      return r.oldestTime < acc ? r.oldestTime : acc;
    }, null);
    return earliest ? new Date(earliest) : null;
  }, [config, inventory, serverName, signal]);

  if (!date) return null;

  return (
    <div className="px-3 py-1.5 flex items-center gap-2 text-[10px] text-text-muted/80 font-mono select-none">
      <span aria-hidden className="flex-1 h-px bg-border-subtle" />
      <span>── persisted from {formatDateOnly(date)} ──</span>
      <span aria-hidden className="flex-1 h-px bg-border-subtle" />
    </div>
  );
}
