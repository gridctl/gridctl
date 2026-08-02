import { useMemo } from 'react';
import { create } from 'zustand';
import { subscribeWithSelector } from 'zustand/middleware';
import type { ServerPins, SkillPin } from '../lib/api';

interface PinsState {
  pins: Record<string, ServerPins> | null;
  setPins: (pins: Record<string, ServerPins>) => void;
  // Skill-document pins, the same polling contract as pins: null until the
  // first /api/skill-pins poll lands, {} when skill pinning is quiet.
  skillPins: Record<string, SkillPin> | null;
  setSkillPins: (skillPins: Record<string, SkillPin>) => void;
}

export const usePinsStore = create<PinsState>()(
  subscribeWithSelector((set) => ({
    pins: null,
    setPins: (pins) => set({ pins }),
    skillPins: null,
    setSkillPins: (skillPins) => set({ skillPins }),
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

// ---------------------------------------------------------------------------
// Skill pin selectors — same shapes and stability discipline as the server
// selectors above (stable empty refs, pure fns over the map, useMemo hooks).
// ---------------------------------------------------------------------------

// skillHasAlertFindings mirrors serverHasAlertFindings: warn-or-critical
// only, info deliberately excluded from every attention signal.
export const skillHasAlertFindings = (pin: SkillPin): boolean =>
  (pin.findings ?? []).some((f) => f.severity === 'warn' || f.severity === 'critical');

export const countSkillAlertFindings = (pin: SkillPin): number =>
  (pin.findings ?? []).filter((f) => f.severity === 'warn' || f.severity === 'critical').length;

export const countDriftedSkills = (skillPins: Record<string, SkillPin> | null): number => {
  if (!skillPins) return 0;
  return Object.values(skillPins).filter((p) => p.status === 'drift').length;
};

export const countFindingSkills = (skillPins: Record<string, SkillPin> | null): number => {
  if (!skillPins) return 0;
  return Object.values(skillPins).filter(skillHasAlertFindings).length;
};

export const firstDriftedSkill = (skillPins: Record<string, SkillPin> | null): string | null => {
  if (!skillPins) return null;
  const names = Object.keys(skillPins)
    .filter((name) => skillPins[name].status === 'drift')
    .sort((a, b) => a.localeCompare(b));
  return names[0] ?? null;
};

export const firstFindingsSkill = (skillPins: Record<string, SkillPin> | null): string | null => {
  if (!skillPins) return null;
  const names = Object.keys(skillPins)
    .filter((name) => skillHasAlertFindings(skillPins[name]))
    .sort((a, b) => a.localeCompare(b));
  return names[0] ?? null;
};

export const useDriftedSkillCount = () => {
  const skillPins = usePinsStore((s) => s.skillPins);
  return useMemo(() => countDriftedSkills(skillPins), [skillPins]);
};

export const useFindingSkillCount = () => {
  const skillPins = usePinsStore((s) => s.skillPins);
  return useMemo(() => countFindingSkills(skillPins), [skillPins]);
};

export const useFirstDriftedSkill = () => {
  const skillPins = usePinsStore((s) => s.skillPins);
  return useMemo(() => firstDriftedSkill(skillPins), [skillPins]);
};

export const useFirstFindingsSkill = () => {
  const skillPins = usePinsStore((s) => s.skillPins);
  return useMemo(() => firstFindingsSkill(skillPins), [skillPins]);
};
