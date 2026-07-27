import type { SkillUsageResponse } from '../types';

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
  return `${hours} ${hours === 1 ? 'hour' : 'hours'} ago`;
}
