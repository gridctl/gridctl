/**
 * Shared time formatting utilities
 */

// Ages of 48 hours or more read as days, and two weeks or more as an
// absolute date: "853h ago" says nothing, and past a fortnight "36d ago"
// is harder to place than "Jun 12, 2026".
const coarseAge = (hours: number, date: Date): string => {
  const days = Math.floor(hours / 24);
  if (days < 14) return `${days}d ago`;
  return date.toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' });
};

export function formatRelativeTime(date: Date): string {
  const seconds = Math.floor((Date.now() - date.getTime()) / 1000);
  if (isNaN(seconds) || seconds < 10) return 'just now';
  if (seconds < 60) return `${seconds}s ago`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 48) return `${hours}h ago`;
  return coarseAge(hours, date);
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
  if (hours < 48) return `${hours}h ago`;
  return coarseAge(hours, date);
}
