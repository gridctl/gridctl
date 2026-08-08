import type { ModelPreference } from '../types';

/**
 * The effective chip content for a model preference: the policy-resolved
 * value wins (it is what projection applies), the author's declaration
 * otherwise. Null when the wire object is absent or carries neither, so
 * older backends and undeclared items render nothing.
 */
export function modelChipInfo(
  mp: ModelPreference | undefined | null,
): { value: string; viaPolicy: boolean; resolution?: 'default' | 'override'; declared?: string } | null {
  if (!mp) return null;
  if (mp.resolved?.value) {
    return {
      value: mp.resolved.value,
      viaPolicy: true,
      resolution: mp.resolved.resolution,
      declared: mp.declared?.value,
    };
  }
  if (mp.declared?.value) {
    return { value: mp.declared.value, viaPolicy: false };
  }
  return null;
}
