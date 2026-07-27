import type { SkillFile } from '../types';

// Ordering for a skill's supporting files. Pure (no React) so it is
// unit-testable and the tree can sort without re-rendering logic.
//
// Fetch order is whatever the backend directory walk produced, which is neither
// stable nor useful for finding a file, so the tree picks an explicit axis.
// Name is the default; size answers "what is big in here"; path breaks ties by
// full path when basenames repeat across nested directories in one bucket.
export type FileSort = 'name' | 'size' | 'path';

export const FILE_SORTS: { key: FileSort; label: string }[] = [
  { key: 'name', label: 'Name' },
  { key: 'size', label: 'Size' },
  { key: 'path', label: 'Path' },
];

const baseName = (path: string) => path.split('/').pop() ?? path;

/** Sort a copy of the file list; never mutates the fetched array. */
export function sortSkillFiles(files: SkillFile[], sort: FileSort): SkillFile[] {
  const byPath = (a: SkillFile, b: SkillFile) => a.path.localeCompare(b.path);
  const copy = [...files];
  if (sort === 'size') {
    // Largest first, then by path so equal sizes keep a stable, readable order.
    return copy.sort((a, b) => b.size - a.size || byPath(a, b));
  }
  if (sort === 'path') return copy.sort(byPath);
  return copy.sort((a, b) => baseName(a.path).localeCompare(baseName(b.path)) || byPath(a, b));
}

/**
 * Group files by their top-level directory, ordering within each bucket by the
 * chosen axis. Buckets keep first-seen order; the sort is an intra-directory
 * concern, not a reshuffle of the tree. Files directly in the skill root are
 * bucketed under `_root`.
 */
export function groupFilesByDir(files: SkillFile[], sort: FileSort): Record<string, SkillFile[]> {
  const grouped: Record<string, SkillFile[]> = {};
  for (const file of files ?? []) {
    if (file.isDir) continue;
    const parts = file.path.split('/');
    const dir = parts.length > 1 ? parts[0] : '_root';
    if (!grouped[dir]) grouped[dir] = [];
    grouped[dir].push(file);
  }
  for (const dir of Object.keys(grouped)) {
    grouped[dir] = sortSkillFiles(grouped[dir], sort);
  }
  return grouped;
}
