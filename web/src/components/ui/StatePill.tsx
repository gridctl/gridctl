import { cn } from '../../lib/cn';

/**
 * The shared projection-state vocabulary from the pkg/project engine. The
 * CLI, the context API, the agent projection API, and the wiring API all
 * speak these exact strings; UI surfaces must render them verbatim rather
 * than inventing synonyms. Agent rows use the first four; context rows
 * add never-synced and unsupported; wiring rows add foreign and missing; pack rows add unresolved.
 */
export type ProjectionState =
  | 'in-sync'
  | 'stale'
  | 'drifted'
  | 'target-missing'
  | 'never-synced'
  | 'unsupported'
  | 'foreign'
  | 'missing'
  | 'unresolved';

const PROJECTION_STATE_STYLE: Record<ProjectionState, string> = {
  'in-sync': 'text-emerald-400 border-emerald-400/25 bg-emerald-400/10',
  stale: 'text-status-pending border-status-pending/30 bg-status-pending/10',
  drifted: 'text-red-400 border-red-400/25 bg-red-400/10',
  'target-missing': 'text-red-400 border-red-400/25 bg-red-400/10',
  'never-synced': 'text-text-muted border-border/40 bg-background/40',
  unsupported: 'text-text-muted/60 border-border/30 bg-background/30',
  // Wiring extensions: foreign is an ownership conflict (red family);
  // missing is a quiet opportunity, not a fault.
  foreign: 'text-red-400 border-red-400/25 bg-red-400/10',
  missing: 'text-text-muted border-border/40 bg-background/40',
  // Pack extension: a manifest selection the repository does not ship.
  // Selection-time, not a projection outcome: amber, never red, and it
  // must not be reused for loading or not-yet-applied states.
  unresolved: 'text-status-pending border-status-pending/30 bg-status-pending/10',
};

/**
 * One projection-state chip. Extracted from GlobalContextDialog's local
 * STATE_STYLE so context rows and agent projection rows stay one color
 * vocabulary. The state text itself is the accessible content — color is
 * never the only carrier.
 */
export function StatePill({ state, className }: { state: ProjectionState; className?: string }) {
  return (
    <span
      className={cn(
        'text-[10px] px-2 py-0.5 rounded-full border font-medium whitespace-nowrap',
        PROJECTION_STATE_STYLE[state],
        className,
      )}
    >
      {state}
    </span>
  );
}
