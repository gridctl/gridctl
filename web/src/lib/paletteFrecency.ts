export interface FrecencyEntry {
  count: number;
  lastUsed: number;
}

export type FrecencyMap = Map<string, FrecencyEntry>;

// Command IDs that were renamed. Usage recorded under the old ID is credited
// to the new one when the stored map loads.
export const FRECENCY_RENAMES: Readonly<Record<string, string>> = {
  'navigate:traces': 'navigate:workspace-traces',
  'navigate:logs': 'navigate:workspace-logs',
  'navigate:metrics': 'navigate:workspace-metrics',
  'navigate:vault': 'navigate:workspace-vault',
  'navigate:connections': 'navigate:workspace-connections',
  'canvas:refresh': 'system:refresh',
};

function num(value: unknown): number {
  return typeof value === 'number' && Number.isFinite(value) ? value : 0;
}

// Parses the stored frecency value and moves renamed keys onto their new IDs,
// summing counts and keeping the later lastUsed when both exist. Malformed
// input yields an empty map.
export function parseFrecency(raw: string | null): FrecencyMap {
  if (!raw) return new Map();
  try {
    const parsed: unknown = JSON.parse(raw);
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return new Map();
    const map: FrecencyMap = new Map(Object.entries(parsed as Record<string, FrecencyEntry>));
    for (const [oldId, newId] of Object.entries(FRECENCY_RENAMES)) {
      const old = map.get(oldId);
      if (!old) continue;
      map.delete(oldId);
      const current = map.get(newId);
      map.set(
        newId,
        current
          ? {
              count: num(current.count) + num(old.count),
              lastUsed: Math.max(num(current.lastUsed), num(old.lastUsed)),
            }
          : old,
      );
    }
    return map;
  } catch {
    return new Map();
  }
}
