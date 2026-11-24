/**
 * Render phase for a polled or refreshable surface.
 *
 * - `loading`: no load has completed yet and nothing has failed.
 * - `failed`: a load failed and nothing is loaded (render `FailedState`).
 * - `stale`: data is on screen but the latest refresh failed (render the
 *   data under a `FailureBanner`).
 * - `ready`: data is on screen and current.
 *
 * `hasLoaded` means a load completed successfully for what is currently
 * selected. Callers that can be idle before a first fetch starts keep their
 * own guard for that case.
 */
export type RefreshPhase = 'loading' | 'failed' | 'stale' | 'ready';

export function refreshPhase({
  error,
  hasLoaded,
}: {
  error: string | null | undefined;
  hasLoaded: boolean;
}): RefreshPhase {
  if (hasLoaded) return error ? 'stale' : 'ready';
  return error ? 'failed' : 'loading';
}
