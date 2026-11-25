import { cn } from '../../lib/cn';

/** Container chrome for a segment control, shared with non-tab segment groups. */
export const SEGMENT_GROUP_CLASS =
  'inline-flex items-center gap-0.5 p-0.5 rounded-lg border border-border-subtle';

/** Segment chrome for one option; the selected segment is filled. */
export function segmentClass(selected: boolean): string {
  return cn(
    'px-2.5 py-1 rounded-md text-xs font-medium transition-colors',
    'focus-visible:-outline-offset-2',
    selected
      ? 'bg-primary/10 text-primary'
      : 'text-text-muted hover:text-text-primary hover:bg-surface-highlight',
  );
}
