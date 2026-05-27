// Derivations shared by the Library card grid and (later) the table view: the
// top-level category from a skill's `dir`, and a compact "weight" summary
// (files / criteria / license / compatibility) built only from fields the
// registry list payload already carries — no extra fetch.

import type { AgentSkill } from '../types';

/**
 * Top-level category for a skill, taken from the first segment of its `dir`
 * (e.g. "git-workflow/branch-fork" → "git-workflow"). Returns "" when `dir` is
 * absent or empty. Single source of truth for "category from dir", shared by
 * the card and by LibraryGrid's category grouping.
 */
export function skillCategory(dir?: string): string {
  if (!dir) return '';
  return dir.split('/')[0];
}

/**
 * Ordered, human-readable metadata segments for a skill, excluding the
 * category. Each present, non-zero field becomes one segment; absent or
 * zero-valued fields are omitted so a card never shows "0 files". Returns an
 * array (not a joined string) so callers pick their own separator and a future
 * table view can consume the segments individually.
 */
export function skillMetaSummary(skill: AgentSkill): string[] {
  const segments: string[] = [];

  if (skill.fileCount > 0) {
    segments.push(`${skill.fileCount} ${skill.fileCount === 1 ? 'file' : 'files'}`);
  }

  const criteria = skill.acceptanceCriteria?.length ?? 0;
  if (criteria > 0) {
    segments.push(`${criteria} ${criteria === 1 ? 'criterion' : 'criteria'}`);
  }

  if (skill.license) segments.push(skill.license);
  if (skill.compatibility) segments.push(skill.compatibility);

  return segments;
}
