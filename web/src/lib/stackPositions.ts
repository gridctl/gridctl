// Per-stack persistence of user-dragged node positions. Pure helpers: no React
// and no store imports. Loading never throws and never writes.
import { readPref, removePref, writePref } from './preferences';

export type SavedPosition = { x: number; y: number };

const PAYLOAD_VERSION = 1;
// Bump when the automatic layout changes so positions saved against the old
// layout are discarded instead of landing in the wrong place.
export const STACK_LAYOUT_VERSION = 1;
export const MAX_SAVED_POSITIONS = 500;
const MAX_NODE_ID_LENGTH = 512;

export function stackPositionsPrefName(stackName: string): string {
  return `stack-positions:${stackName}`;
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return false;
  const proto = Object.getPrototypeOf(value);
  return proto === Object.prototype || proto === null;
}

export function loadStackPositions(
  stackName: string,
  compact: boolean,
): Map<string, SavedPosition> {
  const positions = new Map<string, SavedPosition>();
  const raw = readPref(stackPositionsPrefName(stackName));
  if (raw == null) return positions;
  let payload: unknown;
  try {
    payload = JSON.parse(raw);
  } catch {
    return positions;
  }
  if (
    !isPlainObject(payload) ||
    payload.v !== PAYLOAD_VERSION ||
    payload.layout !== STACK_LAYOUT_VERSION ||
    payload.compact !== compact ||
    !isPlainObject(payload.positions)
  ) {
    return positions;
  }
  const entries = Object.entries(payload.positions);
  // The newest entries are written last, so an oversized payload keeps its tail.
  for (const [id, value] of entries.slice(Math.max(0, entries.length - MAX_SAVED_POSITIONS))) {
    if (id.length === 0 || id.length > MAX_NODE_ID_LENGTH || !isPlainObject(value)) continue;
    const { x, y } = value;
    if (typeof x !== 'number' || typeof y !== 'number') continue;
    if (!Number.isFinite(x) || !Number.isFinite(y)) continue;
    positions.set(id, { x, y });
  }
  return positions;
}

// Map order is recency order: the oldest entries are dropped past the cap.
export function saveStackPositions(
  stackName: string,
  compact: boolean,
  positions: Map<string, SavedPosition>,
): void {
  if (positions.size === 0) {
    clearStackPositions(stackName);
    return;
  }
  const kept = [...positions].slice(Math.max(0, positions.size - MAX_SAVED_POSITIONS));
  const out: Record<string, SavedPosition> = Object.create(null);
  for (const [id, { x, y }] of kept) {
    out[id] = { x, y };
  }
  try {
    writePref(
      stackPositionsPrefName(stackName),
      JSON.stringify({ v: PAYLOAD_VERSION, layout: STACK_LAYOUT_VERSION, compact, positions: out }),
    );
  } catch {
    // writePref already falls back to a session copy; a listener throwing is not ours to surface.
  }
}

export function clearStackPositions(stackName: string): void {
  try {
    removePref(stackPositionsPrefName(stackName));
  } catch {
    // Storage failures are silent.
  }
}
