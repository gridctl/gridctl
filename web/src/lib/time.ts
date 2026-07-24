/**
 * Shared time formatting utilities
 */

export function formatRelativeTime(date: Date): string {
  const seconds = Math.floor((Date.now() - date.getTime()) / 1000);
  if (isNaN(seconds) || seconds < 10) return 'just now';
  if (seconds < 60) return `${seconds}s ago`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  return `${hours}h ago`;
}

// Finer-grained variant for log tails: sub-minute ages read as seconds
// ("3s ago") instead of collapsing to "just now", which is too coarse when
// entries arrive every few seconds. `now` is injectable for pure rendering
// against a fixed anchor (e.g. the last completed poll).
export function formatRelativeTimeFine(date: Date, now: number = Date.now()): string {
  const seconds = Math.floor((now - date.getTime()) / 1000);
  if (Number.isNaN(seconds)) return '';
  if (seconds < 1) return 'now';
  if (seconds < 60) return `${seconds}s ago`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  return `${hours}h ago`;
}
