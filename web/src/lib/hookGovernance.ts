// hookPinsUrl owns the deep-link grammar into the Pins workspace's hook
// review. Param names match PinsWorkspace's URL parsing, so every producer
// of a hook pins link routes through here.
export function hookPinsUrl(name?: string | null, view?: 'drift' | 'findings'): string {
  if (!name) return '/pins?kind=hook';
  const suffix = view ? `&view=${view}` : '';
  return `/pins?kind=hook&hook=${encodeURIComponent(name)}${suffix}`;
}
