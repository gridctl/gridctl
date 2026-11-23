import type { Variable, VariableType } from '../../lib/api';

export interface ReplaceDraft {
  type: VariableType;
  isSecret: boolean;
  // Omit when the form sends no set, so the stored set is kept.
  set?: string;
}

const visibility = (isSecret: boolean) => (isSecret ? 'secret' : 'plaintext');

// describeReplace lists what replacing `existing` with the form state will
// change. It reads only key metadata from the list entry, which carries no
// value, so no value can reach the summary.
export function describeReplace(existing: Variable, draft: ReplaceDraft): string[] {
  const lines = ['The current value is replaced and cannot be recovered.'];
  if (existing.type !== draft.type) {
    lines.push(`Type: ${existing.type} → ${draft.type}`);
  }
  if (existing.is_secret !== draft.isSecret) {
    lines.push(`Visibility: ${visibility(existing.is_secret)} → ${visibility(draft.isSecret)}`);
  }
  if (draft.set && draft.set !== (existing.set ?? '')) {
    lines.push(`Set: ${existing.set || 'no set'} → ${draft.set}`);
  }
  lines.push('The description and other metadata are kept.');
  return lines;
}
