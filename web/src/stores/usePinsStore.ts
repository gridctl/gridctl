import { useMemo } from 'react';
import { create } from 'zustand';
import { subscribeWithSelector } from 'zustand/middleware';
import type { ServerPins } from '../lib/api';

interface PinsState {
  pins: Record<string, ServerPins> | null;
  setPins: (pins: Record<string, ServerPins>) => void;
}

export const usePinsStore = create<PinsState>()(
  subscribeWithSelector((set) => ({
    pins: null,
    setPins: (pins) => set({ pins }),
  }))
);

// Stable empty reference — avoids new array allocation when pins is null,
// satisfying useSyncExternalStore's requirement for referential stability.
const EMPTY_DRIFTED: Array<{ name: string } & ServerPins> = [];

export const useDriftedServers = () => {
  const pins = usePinsStore((s) => s.pins);
  return useMemo(() => {
    if (!pins) return EMPTY_DRIFTED;
    return Object.entries(pins)
      .filter(([, sp]) => sp.status === 'drift')
      .map(([name, sp]) => ({ name, ...sp }));
  }, [pins]);
};

// serverHasAlertFindings reports whether a server's pinned tools carry at
// least one warn-or-critical poisoning-scan finding. Info findings are
// deliberately excluded everywhere this is used: chips, toasts, rail marks,
// and deep-link targets are attention signals, not inventories.
export const serverHasAlertFindings = (sp: ServerPins): boolean =>
  Object.values(sp.tools ?? {}).some((rec) =>
    (rec.findings ?? []).some((f) => f.severity === 'warn' || f.severity === 'critical'),
  );

// countServerAlertFindings counts a single server's warn-or-critical
// findings, for the rail's compact per-server indicator.
export const countServerAlertFindings = (sp: ServerPins): number =>
  Object.values(sp.tools ?? {}).reduce(
    (acc, rec) =>
      acc + (rec.findings ?? []).filter((f) => f.severity === 'warn' || f.severity === 'critical').length,
    0,
  );

// countFindingServers counts servers with at least one warn-or-critical
// finding; the status bar chip counts servers, not findings.
export const countFindingServers = (pins: Record<string, ServerPins> | null): number => {
  if (!pins) return 0;
  return Object.values(pins).filter(serverHasAlertFindings).length;
};

export const useFindingServerCount = () => {
  const pins = usePinsStore((s) => s.pins);
  return useMemo(() => countFindingServers(pins), [pins]);
};

// firstDriftedServer / firstFindingsServer pick the deep-link target for the
// drift and findings chips and toasts: the alphabetically first affected
// server, matching the rail's drifted-first-then-alphabetical order.
export const firstDriftedServer = (pins: Record<string, ServerPins> | null): string | null => {
  if (!pins) return null;
  const names = Object.keys(pins)
    .filter((name) => pins[name].status === 'drift')
    .sort((a, b) => a.localeCompare(b));
  return names[0] ?? null;
};

export const firstFindingsServer = (pins: Record<string, ServerPins> | null): string | null => {
  if (!pins) return null;
  const names = Object.keys(pins)
    .filter((name) => serverHasAlertFindings(pins[name]))
    .sort((a, b) => a.localeCompare(b));
  return names[0] ?? null;
};

export const useFirstDriftedServer = () => {
  const pins = usePinsStore((s) => s.pins);
  return useMemo(() => firstDriftedServer(pins), [pins]);
};

export const useFirstFindingsServer = () => {
  const pins = usePinsStore((s) => s.pins);
  return useMemo(() => firstFindingsServer(pins), [pins]);
};
