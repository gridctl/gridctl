// Small string-formatting helpers shared across the UI.

/**
 * Turn a kebab/lower key into a Title-Cased label, e.g.
 * `git-workflow` → `Git Workflow`.
 */
export function toTitleCase(key: string): string {
  return key.replace(/-/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase());
}
