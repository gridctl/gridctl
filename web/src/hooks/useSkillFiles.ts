import { useEffect, useState } from 'react';
import { fetchSkillFiles } from '../lib/api';
import type { SkillFile } from '../types';

export interface SkillFilesState {
  /** The installed file list, or null while it is unknown. */
  files: SkillFile[] | null;
  loading: boolean;
}

interface Resolved {
  name: string;
  files: SkillFile[];
}

/**
 * Loads a skill's installed supporting files.
 *
 * `knownEmpty` is the cheap path: the registry list already carries
 * `fileCount`, so a skill reporting zero files needs no request to answer
 * "which supporting directories are installed" — the answer is none. That
 * matters because skills imported before supporting-file install shipped all
 * report zero, which is exactly the population the package check targets. Only
 * a skill that actually has files pays for a fetch.
 *
 * `files` stays null on failure rather than collapsing to `[]`: callers use it
 * to decide whether a package looks incomplete, and "we could not look" must
 * never read as "nothing is there".
 *
 * State is written only inside the async loader after an await tick, never
 * synchronously in the effect body.
 */
export function useSkillFiles(
  skillName: string | null,
  knownEmpty: boolean,
): SkillFilesState {
  const [resolved, setResolved] = useState<Resolved | null>(null);
  const needsFetch = !!skillName && !knownEmpty;

  useEffect(() => {
    if (!needsFetch || !skillName) return;
    let active = true;
    fetchSkillFiles(skillName)
      .then((files) => {
        if (active) setResolved({ name: skillName, files });
      })
      .catch(() => {
        // Leave the list unknown; the caller must not infer emptiness.
      });
    return () => {
      active = false;
    };
  }, [skillName, needsFetch]);

  if (!skillName) return { files: null, loading: false };
  if (knownEmpty) return { files: [], loading: false };

  // A result for a different skill is stale by definition.
  const current = resolved?.name === skillName ? resolved : null;
  return { files: current?.files ?? null, loading: current === null };
}
