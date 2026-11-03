// contextPinsUrl owns the deep-link grammar into the Pins workspace's
// context review. Param names match PinsWorkspace and the security-report
// action path /pins?kind=context&fragment=<name>.
export function contextPinsUrl(name?: string | null, view?: 'drift' | 'findings'): string {
  if (!name) return '/pins?kind=context';
  const suffix = view ? `&view=${view}` : '';
  return `/pins?kind=context&fragment=${encodeURIComponent(name)}${suffix}`;
}

export type ContextPinChipStatus = 'pinned' | 'drift' | 'unpinned';

/** Status of one fragment from the additive pins object on GET /api/context. */
export function contextPinStatus(
  name: string,
  pins?: { drifted?: string[]; unpinned?: string[]; missing?: string[] } | null,
): ContextPinChipStatus {
  if (!pins) return 'unpinned';
  if (pins.drifted?.includes(name)) return 'drift';
  if (pins.unpinned?.includes(name) || pins.missing?.includes(name)) return 'unpinned';
  return 'pinned';
}
