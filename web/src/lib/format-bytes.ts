// Compact human-readable byte formatter for telemetry-inventory UI
// callouts. Uses binary multiples (KiB / MiB / GiB) since lumberjack's
// MaxSize is documented in MiB and operators on the wipe modal are
// reasoning about disk footprint, not network throughput.

const UNITS = ['B', 'KiB', 'MiB', 'GiB', 'TiB'] as const;

export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B';
  let value = bytes;
  let unitIndex = 0;
  while (value >= 1024 && unitIndex < UNITS.length - 1) {
    value /= 1024;
    unitIndex++;
  }
  // < 10: one decimal; >= 10: integer. Keeps "1.4 MiB" but avoids
  // "1024 KiB" or noisy decimals on bigger numbers.
  const formatted = value < 10 ? value.toFixed(1) : Math.round(value).toString();
  return `${formatted} ${UNITS[unitIndex]}`;
}

// ISO date-only formatter used by the persisted-from divider and the
// wipe modal's "from / to" line. Keeps the UI stable across locales for
// the technical audience (operators) without pulling in Intl complexity.
export function formatDateOnly(date: Date | undefined): string {
  if (!date || Number.isNaN(date.getTime())) return '';
  return date.toISOString().slice(0, 10);
}
