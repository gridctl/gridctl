/**
 * Formatters shared by the runs grid, inspector, and detail page.
 * Keeping them in one module means a future "human readable" tweak
 * (e.g. shorter run-id slug, different duration unit) lands once.
 */

export function shortRunID(runID: string): string {
  return runID.length > 12 ? runID.slice(0, 12) : runID;
}

export function formatDurationMicros(micros: number | undefined): string {
  if (micros == null || !Number.isFinite(micros)) return '—';
  if (micros < 1000) return `${micros}µs`;
  const ms = micros / 1000;
  if (ms < 1000) return `${ms.toFixed(ms < 10 ? 1 : 0)}ms`;
  const s = ms / 1000;
  if (s < 60) return `${s.toFixed(s < 10 ? 2 : 1)}s`;
  const m = Math.floor(s / 60);
  const rem = Math.round(s - m * 60);
  return `${m}m${rem}s`;
}

export function formatDurationBetween(
  start: string | undefined,
  end: string | undefined,
): string {
  if (!start) return '—';
  const startMs = Date.parse(start);
  if (Number.isNaN(startMs)) return '—';
  const endMs = end ? Date.parse(end) : Date.now();
  if (Number.isNaN(endMs)) return '—';
  return formatDurationMicros((endMs - startMs) * 1000);
}

export function formatAbsoluteTime(iso: string | undefined): string {
  if (!iso) return '—';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '—';
  return d.toLocaleString([], {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  });
}
