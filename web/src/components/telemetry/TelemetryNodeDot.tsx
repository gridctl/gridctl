// 12px dot anchored to the bottom-right of an MCP server graph node,
// indicating telemetry persistence state. Three visual states map onto
// the (config, inventory) cross-product:
//
//   off       — gray solid: no signal effectively persisted for this server.
//   pending   — outlined emerald: at least one signal is on but no files
//               yet exist on disk. Useful for spotting silent failures
//               (e.g., persistence enabled but the writer can't write).
//   active    — filled emerald with glow: at least one signal is on AND
//               inventory has files.
//
// Tooltip enumerates the per-signal status and total disk footprint.
import { useMemo } from 'react';
import { cn } from '../../lib/cn';
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

  if (view.state === 'off') {
    return (
      <span
        aria-label={view.tooltip}
        title={view.tooltip}
        className="absolute bottom-1.5 right-1.5 w-3 h-3 rounded-full border border-text-muted/50 bg-text-muted/20"
      />
    );
  }

  return (
    <span
      aria-label={view.tooltip}
      title={view.tooltip}
      className={cn(
        'absolute bottom-1.5 right-1.5 w-3 h-3 rounded-full border transition-all duration-200',
        view.state === 'active'
          ? 'bg-status-running border-status-running shadow-[0_0_8px_rgba(16,185,129,0.55)]'
          : 'border-status-running/70 bg-transparent',
      )}
    />
  );
}
