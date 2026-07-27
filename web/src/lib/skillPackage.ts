import type { SkillFile } from '../types';

// Detects a skill whose instructions expect supporting files that are not
// installed. Pure (no React, no fetch) so the matcher is unit-testable on its
// own, which matters more here than usual: the entire value of the warning
// depends on it not crying wolf.
//
// Background: skills imported before supporting-file install shipped carry only
// their SKILL.md. An agent following instructions that invoke `scripts/foo.sh`
// then fails at use time with a missing-file error, producing degraded output
// rather than a visible break. This is the safety net that says so; the fix is
// re-importing or syncing the skill.

/** Directories the registry installs alongside a skill's SKILL.md. */
export const MANAGED_DIRS = ['scripts', 'references', 'assets'] as const;

export type ManagedDir = (typeof MANAGED_DIRS)[number];

/**
 * Matches a reference that reads as a *path* into a managed directory, not a
 * prose mention of the word.
 *
 * The leading boundary rejects a longer path segment ending in the same word,
 * so `vendor/scripts/x.sh` and `myscripts/x.sh` do not match while `scripts/x`,
 * `./scripts/x`, and `` `scripts/x` `` do. The trailing part requires at least
 * one filename character after the slash, so a bare "scripts/" with nothing
 * after it, or the prose "the scripts / assets split", does not match.
 *
 * Deliberately not anchored to a file extension: `references/api` (a directory
 * reference) is a real dependency, and requiring an extension would miss it.
 */
function pathReferencePattern(dir: ManagedDir): RegExp {
  return new RegExp(String.raw`(?:^|[\s"'`+'`'+String.raw`([<])(?:\.{1,2}\/)?${dir}\/[\w.-]`, 'm');
}

/**
 * Strips fenced code blocks. A fence often contains a shell transcript from a
 * *different* project, so treating its contents as this skill's dependencies is
 * a common false positive. Inline code spans are deliberately kept: `` `scripts/
 * setup.sh` `` is the most natural way to name a bundled file in prose.
 */
function stripFencedCode(body: string): string {
  return body.replace(/^[ \t]*(```|~~~)[\s\S]*?^[ \t]*\1[ \t]*$/gm, '\n');
}

/** The managed directories a body references as paths. */
export function referencedManagedDirs(body: string): ManagedDir[] {
  if (!body) return [];
  const prose = stripFencedCode(body);
  return MANAGED_DIRS.filter((dir) => pathReferencePattern(dir).test(prose));
}

/** The managed directories that actually have an installed file under them. */
export function installedManagedDirs(files: SkillFile[]): Set<ManagedDir> {
  const present = new Set<ManagedDir>();
  for (const file of files) {
    if (file.isDir) continue;
    const top = file.path.split('/')[0];
    if ((MANAGED_DIRS as readonly string[]).includes(top)) present.add(top as ManagedDir);
  }
  return present;
}

/**
 * Managed directories the instructions reference but that ship no files.
 *
 * `files` must be the *loaded* file list. Pass null while it is unknown and
 * this returns nothing: a skill whose files have not loaded is not a skill with
 * no files, and warning on absent data is exactly the dishonesty this module
 * exists to avoid.
 */
export function missingManagedDirs(body: string | null, files: SkillFile[] | null): ManagedDir[] {
  if (!body || files === null) return [];
  const installed = installedManagedDirs(files);
  return referencedManagedDirs(body).filter((dir) => !installed.has(dir));
}

/** Human-readable list: "scripts/", "scripts/ and references/", "a/, b/, and c/". */
export function formatManagedDirs(dirs: ManagedDir[]): string {
  const withSlash = dirs.map((d) => `${d}/`);
  if (withSlash.length <= 1) return withSlash[0] ?? '';
  if (withSlash.length === 2) return `${withSlash[0]} and ${withSlash[1]}`;
  return `${withSlash.slice(0, -1).join(', ')}, and ${withSlash[withSlash.length - 1]}`;
}
