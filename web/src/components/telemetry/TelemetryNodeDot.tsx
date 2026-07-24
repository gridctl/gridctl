// 12px dot anchored to the bottom-right of an MCP server graph node,
// indicating telemetry persistence state. Surfaces only when something
// needs attention; every steady state renders nothing so it does not
// compete visually with the running-status indicator.
//
//   off       — hidden: persistence disabled is the default for most
//               stacks, and marking it put a permanent gray circle on
//               every server card, which read as noise rather than
//               signal. Details remain reachable via the telemetry
//               sidebar.
//   pending   — outlined emerald: at least one signal is on but no files
//               yet exist on disk. Useful for spotting silent failures
//               (e.g., persistence enabled but the writer can't write).
//   active    — hidden: signals on AND inventory has files. Healthy steady
//               state needs no marker.
//
// Tooltip (when rendered) enumerates the per-signal status and total disk footprint.
import { useMemo } from 'react';
import { formatBytes } from '../../lib/format-bytes';
import { effectiveSignal } from '../../lib/telemetry-config';
import {
  inventoryByServer,
  useInventory,
  useTelemetryConfig,
} from '../../stores/useTelemetryStore';
import type { TelemetrySignal } from '../../types';

const SIGNALS: TelemetrySignal[] = ['logs', 'metrics', 'traces'];

interface Props {
  serverName: string;
}

export function TelemetryNodeDot({ serverName }: Props) {
  const config = useTelemetryConfig();
  const inventory = useInventory();

  const view = useMemo(() => {
    const records = inventoryByServer(inventory, serverName);
    const sizeBytes = records.reduce((sum, r) => sum + r.sizeBytes, 0);
    const enabled: Record<TelemetrySignal, boolean> = {
      logs: effectiveSignal(config, serverName, 'logs'),
      metrics: effectiveSignal(config, serverName, 'metrics'),
      traces: effectiveSignal(config, serverName, 'traces'),
    };
    const anyOn = SIGNALS.some((s) => enabled[s]);
    const hasFiles = records.length > 0;
    let state: 'off' | 'pending' | 'active';
    if (!anyOn) state = 'off';
    else if (!hasFiles) state = 'pending';
    else state = 'active';
    const tooltip = SIGNALS.map((s) => {
      const label = s[0].toUpperCase() + s.slice(1);
      return `${label}: ${enabled[s] ? 'persistent' : 'off'}`;
    }).join(' · ') + ` · ${formatBytes(sizeBytes)} on disk`;
    return { state, tooltip };
  }, [config, inventory, serverName]);

  // Only the pending state renders: active is healthy and off is the
  // default, and neither warrants a permanent marker on every card.
  if (view.state !== 'pending') {
    return null;
  }

  return (
    <span
      aria-label={view.tooltip}
      title={view.tooltip}
      className="absolute bottom-1.5 right-1.5 w-3 h-3 rounded-full border border-status-running/70 bg-transparent transition-all duration-200"
    />
  );
}
