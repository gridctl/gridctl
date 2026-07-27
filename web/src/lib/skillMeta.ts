// Derivations shared by the Library card grid and the table view: a skill's
// category (frontmatter first, then a nested `dir`), and a compact "weight" summary
// (files / criteria / license / compatibility) built only from fields the
// registry list payload already carries — no extra fetch.

import type { AgentSkill } from '../types';

/**
 * Top-level category for a skill. An explicit `category` in the skill's
 * frontmatter metadata wins; otherwise the category is the first segment of a
 * *nested* `dir` (e.g. "git-workflow/branch-fork" → "git-workflow").
 *
 * A flat dir carries no category. "docx" is the skill's own directory name, not
 * a group it belongs to, so returning it would make every skill in a flat
 * registry its own one-member category and turn Group-by-Category into a list.
 * Returns "" for that case, and for an absent or empty dir. Single source of
 * truth for "category", shared by the card grid, the table, and the inspector.
 */
export function skillCategory(dir?: string, metadata?: Record<string, string>): string {
  const declared = metadata?.category?.trim();
  if (declared) return declared;
  if (!dir) return '';
  const slash = dir.indexOf('/');
  return slash === -1 ? '' : dir.slice(0, slash);
}

/** Whether any skill in the set carries a category, gating category surfaces. */
export function hasAnyCategory(skills: Pick<AgentSkill, 'dir' | 'metadata'>[]): boolean {
  return skills.some((s) => skillCategory(s.dir, s.metadata) !== '');
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
