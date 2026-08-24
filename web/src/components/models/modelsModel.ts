import type { ModelsSyncResult, ModelsTargetStatus } from '../../types';

/** LiteLLM's fixed tier vocabulary, in render order. */
export const TIER_ORDER = ['SIMPLE', 'MEDIUM', 'COMPLEX', 'REASONING'] as const;

/** Human labels for the three projection targets. */
const TARGET_LABELS: Record<string, string> = {
  'litellm-fragment': 'LiteLLM router fragment',
  'litellm-include': 'LiteLLM include line',
  opencode: 'OpenCode provider',
};

export function modelsTargetLabel(target: string): string {
  return TARGET_LABELS[target] ?? target;
}

/**
 * States that need a sync or a decision. Mirrors the engine's
 * NeedsAttention exactly: restart-pending is an annotation, never
 * attention, and never-synced is an invitation, not a fault.
 */
export const MODELS_ATTENTION = new Set(['stale', 'drifted', 'target-missing']);

/**
 * Adopt records on-disk bytes for the fragment and the OpenCode
 * provider only; a removed include line is not adoptable (nothing
 * exists to record) and resolves via force sync instead.
 */
export const ADOPTABLE_TARGETS = new Set(['litellm-fragment', 'opencode']);

/** Rows a Review has to resolve: recorded drift on any target. */
export function driftedTargets(targets: ModelsTargetStatus[]): ModelsTargetStatus[] {
  return targets.filter((t) => t.state === 'drifted');
}

/** True when at least one drifted row is one Adopt can record. */
export function hasAdoptableDrift(targets: ModelsTargetStatus[]): boolean {
  return driftedTargets(targets).some((t) => ADOPTABLE_TARGETS.has(t.target));
}

/**
 * Toast classification for a sync pass. Skips carry no error field, so
 * an error-keyed toast would announce success while nothing was written;
 * mirror the engine's HasFailures vocabulary instead.
 */
export function describeModelsSyncResults(
  results: ModelsSyncResult[],
  dryRun: boolean,
): { kind: 'success' | 'warning' | 'error'; message: string } {
  const errors = results.filter((r) => r.action === 'error');
  const skipped = results.filter(
    (r) => r.action === 'skipped-drift' || r.action === 'skipped-foreign',
  );
  const updated = results.filter((r) => r.action === 'updated');
  const wouldUpdate = results.filter((r) => r.action === 'would-update');

  if (errors.length > 0) {
    return {
      kind: 'error',
      message: `${errors.length} target${errors.length === 1 ? '' : 's'} failed: ${errors[0].error ?? 'sync error'}`,
    };
  }
  if (skipped.length > 0) {
    return {
      kind: 'warning',
      message: `${skipped.length} target${skipped.length === 1 ? '' : 's'} skipped (${skipped
        .map((r) => modelsTargetLabel(r.target))
        .join(', ')}); review the drift before overwriting`,
    };
  }
  if (dryRun) {
    return {
      kind: 'success',
      message:
        wouldUpdate.length === 0
          ? 'Nothing to change: every target matches the policy'
          : `${wouldUpdate.length} target${wouldUpdate.length === 1 ? '' : 's'} would change`,
    };
  }
  if (updated.length === 0) {
    return { kind: 'success', message: 'Everything already in sync' };
  }
  const restartPending = updated.some((r) => r.target === 'litellm-fragment');
  return {
    kind: 'success',
    message: `${updated.length} target${updated.length === 1 ? '' : 's'} updated${restartPending ? '; restart LiteLLM to make the policy live' : ''}`,
  };
}
