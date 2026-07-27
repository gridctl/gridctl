import { useEffect, useState } from 'react';
import { fetchRegistrySkill } from '../lib/api';

export interface SkillBodyState {
  /** The Markdown body, or null while it is unknown. */
  body: string | null;
  loading: boolean;
  error: string | null;
}

// One resolved fetch, tagged with the skill it belongs to so a result that
// lands after the selection moved on is ignored rather than shown against the
// wrong skill.
interface Resolved {
  name: string;
  body?: string;
  error?: string;
}

/**
 * Lazily loads one skill's Markdown body.
 *
 * The registry list omits bodies (they dominated a payload polled every few
 * seconds), so the surfaces that render instructions fetch the single skill on
 * demand. `seed` short-circuits the fetch for callers that already hold a
 * hydrated skill — a detail response, or a list read with ?full=1 — so this
 * hook never issues a request it does not need. An empty-string seed counts as
 * hydrated: "this skill has no instructions" is an answer, not a missing value.
 *
 * `enabled` gates the fetch on the surface actually being visible (the
 * Instructions tab being open, the editor being mounted), mirroring how
 * SkillFileTree defers its file listing until the Files tab is selected.
 *
 * State is written only inside the async loader after an await tick, never
 * synchronously in the effect body, so mounting cannot cascade an extra render.
 */
export function useSkillBody(
  skillName: string | null,
  enabled: boolean,
  seed?: string,
): SkillBodyState {
  const [resolved, setResolved] = useState<Resolved | null>(null);
  const needsFetch = enabled && !!skillName && seed === undefined;

  useEffect(() => {
    if (!needsFetch || !skillName) return;
    let active = true;
    fetchRegistrySkill(skillName)
      .then((skill) => {
        if (active) setResolved({ name: skillName, body: skill.body ?? '' });
      })
      .catch((err: unknown) => {
        if (!active) return;
        setResolved({
          name: skillName,
          error: err instanceof Error ? err.message : 'Failed to load instructions',
        });
      });
    return () => {
      active = false;
    };
  }, [skillName, needsFetch]);

  if (seed !== undefined) return { body: seed, loading: false, error: null };
  // A result for a different skill is stale by definition — the selection moved
  // while the request was in flight — so it reads as "still loading" here.
  const current = resolved?.name === skillName ? resolved : null;
  return {
    body: current?.body ?? null,
    loading: needsFetch && current === null,
    error: current?.error ?? null,
  };
}
