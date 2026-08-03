import type { ItemState } from '../types';

// One source of truth for registry lifecycle-state color. This map previously
// existed in three divergent copies (StateBadge, the Stack canvas SkillNode, and
// the Library KPI dot legend), which drifted apart in border opacity and surface
// token. Presentation still differs per surface (badge typography, dot size), so
// only the color vocabulary is shared here.
//
// `draft` uses status-pending because that is what the token means: not yet
// promoted. Warnings share the hue but never the treatment, since they always
// carry an AlertTriangle and notice chrome that a lifecycle badge does not.
export const stateBadgeClasses: Record<ItemState, string> = {
  active: 'text-status-running bg-status-running/10 border-status-running/25',
  draft: 'text-status-pending bg-status-pending/10 border-status-pending/25',
  disabled: 'text-text-muted bg-surface border-border/40',
};

// Solid fills for the aria-hidden KPI legend dots, which carry no text of their
// own and so need the token at full opacity rather than a tint.
export const stateDotClasses: Record<ItemState, string> = {
  active: 'bg-status-running',
  draft: 'bg-status-pending',
  disabled: 'bg-text-muted',
};
