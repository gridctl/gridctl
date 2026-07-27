import type { SkillUsageResponse, SkillUsageStat } from '../types';

// Honesty gate for the Library's usage surfaces. Pure (no React, no ambient
// clock — `now` is always passed in) so it is unit-testable and the workspace
// can memoize the derived values.
//
// The problem it solves: a gateway that started recording minutes ago reports
// `{observedSince: <just now>, skills: {}}`. Read literally that says "no skill
// has ever been called", so a "Never used" count equal to the whole active
// catalog looks like a finding rather than an artifact of an empty window. Act
// on it (Select all, Disable) and the entire library goes dark.
//
// The discriminator is time, not emptiness. A registry where every skill really
// is unused after a month of tracking is a genuine signal and must still show
// its count, so the map being empty is corroborating evidence only.

/**
 * Minimum tracking window before a zero-call reading means anything. Matches
 * `defaultMinObservationWindow` in pkg/optimize/optimize.go, which gates the
 * backend's own unused-tool heuristic, so the two surfaces agree on how young
 * is too young.
 */
export const MIN_OBSERVATION_MS = 24 * 60 * 60 * 1000;

/**
 * Whether the tracking window is too young for a zero-call reading to mean
 * anything. A missing or unparseable `observedSince` is cold, not epoch zero:
 * "we do not know when tracking started" is not "tracking started in 1970".
 *
 * Pass `usage` as null only for "the endpoint is unavailable"; that is a
 * separate state and is reported as cold here so no caller can mistake an
 * absent snapshot for an observed zero.
 */
export function isUsageWindowCold(usage: SkillUsageResponse | null, now: number): boolean {
  if (!usage) return true;
  const since = usage.observedSince;
  if (!since) return true;
  const startedAt = Date.parse(since);
  if (Number.isNaN(startedAt)) return true;
  return now - startedAt < MIN_OBSERVATION_MS;
}

/**
 * The sentence explaining why a usage count is being withheld, for the KPI
 * tooltip and the bulk-action caveat. Returns null when the window is warm and
 * no caveat is warranted.
 */
export function coldWindowCaveat(usage: SkillUsageResponse | null, now: number): string | null {
  if (!isUsageWindowCold(usage, now)) return null;
  const observed = observedSinceLabel(usage, now);
  return observed
    ? `Usage tracking started ${observed}, less than a day ago. Skills with no recorded calls have not been idle, they have not been observed yet.`
    : 'Usage tracking has no recorded start, so a skill with no calls may simply predate tracking.';
}

/**
 * Whether the tracking window is too short to support a claim about `windowMs`.
 *
 * This is a *second*, independent unknowability condition, and the easy mistake
 * is to implement only `isUsageWindowCold`. A gateway tracking for three days
 * cannot answer "which skills have gone unused for 30 days" — every skill would
 * qualify by construction, which is exactly the artifact the cold-window gate
 * was added to stop. Returns true when the window has not been observed long
 * enough, including when `observedSince` is missing or unparseable.
 */
export function isWindowLongerThanTracking(
  usage: SkillUsageResponse | null,
  windowMs: number,
  now: number,
): boolean {
  if (!usage) return true;
  const since = usage.observedSince;
  if (!since) return true;
  const startedAt = Date.parse(since);
  if (Number.isNaN(startedAt)) return true;
  return now - startedAt < windowMs;
}

/**
 * Whether a skill counts as stale: it has been called at least once, but not
 * inside the window. A skill with no recorded calls is "never used", a separate
 * axis, so it is deliberately excluded here rather than folded in.
 *
 * A recorded call with no timestamp is *not* stale. `calls > 0` with a missing
 * `lastCalledAt` means "used, when is unknown", and treating unknown as old is
 * the same guess this module exists to refuse.
 */
export function isStaleUsage(
  stat: SkillUsageStat | undefined,
  windowMs: number,
  now: number,
): boolean {
  if (!stat || stat.calls <= 0) return false;
  if (!stat.lastCalledAt) return false;
  const lastMs = Date.parse(stat.lastCalledAt);
  if (Number.isNaN(lastMs)) return false;
  return now - lastMs > windowMs;
}

/**
 * The sentence explaining why a stale count is being withheld, or null when the
 * question is answerable. Distinguishes the two reasons, since "tracking just
 * started" and "tracking is younger than the window you picked" call for
 * different user actions (wait, versus pick a shorter window).
 */
export function staleUnknownReason(
  usage: SkillUsageResponse | null,
  windowMs: number,
  windowLabel: string,
  now: number,
): string | null {
  if (isUsageWindowCold(usage, now)) return coldWindowCaveat(usage, now);
  if (!isWindowLongerThanTracking(usage, windowMs, now)) return null;
  const observed = observedSinceLabel(usage, now);
  return `Usage has only been tracked${observed ? ` since ${observed}` : ''}, which is less than ${windowLabel}. Pick a shorter window to see which skills have gone idle.`;
}

// observedSinceLabel renders the tracking start as a short relative phrase
// ("2 hours ago"), or null when there is no usable timestamp.
function observedSinceLabel(usage: SkillUsageResponse | null, now: number): string | null {
  const since = usage?.observedSince;
  if (!since) return null;
  const startedAt = Date.parse(since);
  if (Number.isNaN(startedAt)) return null;
  const elapsedMin = Math.max(0, Math.round((now - startedAt) / 60000));
  if (elapsedMin < 1) return 'just now';
  if (elapsedMin < 60) return `${elapsedMin} ${elapsedMin === 1 ? 'minute' : 'minutes'} ago`;
  const hours = Math.round(elapsedMin / 60);
  if (hours < 24) return `${hours} ${hours === 1 ? 'hour' : 'hours'} ago`;
  // Days matter once the stale facet is in play: its windows run to 30 days,
  // so "720 hours ago" would be a useless way to say "a month".
  const days = Math.round(hours / 24);
  return `${days} ${days === 1 ? 'day' : 'days'} ago`;
}
