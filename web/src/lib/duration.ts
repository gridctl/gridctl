/**
 * Millisecond-duration formatting shared by the trace surfaces. Two tiers:
 * `formatDuration` keeps sub-millisecond and one-decimal precision for span
 * rows and detail panes; `formatTotalDuration` rounds for list cells and
 * trace-level headlines.
 */

export function formatDuration(ms: number): string {
  if (!Number.isFinite(ms)) return '–';
  if (ms < 1) return `${(ms * 1000).toFixed(0)}µs`;
  if (ms < 1000) return `${ms.toFixed(1)}ms`;
  return `${(ms / 1000).toFixed(2)}s`;
}

export function formatTotalDuration(ms: number): string {
  if (!Number.isFinite(ms)) return '–';
  if (ms < 1000) return `${Math.round(ms)}ms`;
  return `${(ms / 1000).toFixed(2)}s`;
}
