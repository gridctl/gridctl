import type { Consumer, Variable } from './api';

// Consumption vocabulary for the Variables workspace list. Pure (no React, no
// clock) so it is unit-testable and the workspace can memoize the derived list.

// How a variable reaches the workloads that use it:
//   explicit: written as ${var:KEY} at a named YAML site
//   set:      injected in bulk through a secrets.sets entry
//   none:     nothing in the active stack consumes it
//
// The classes overlap: a key can be referenced explicitly *and* carried by a
// set, so it answers true for both. That is why the chips are single-select
// rather than AND-combined toggles.
export type RefFilter = 'all' | 'explicit' | 'set' | 'none';

export const REF_FILTERS: { id: RefFilter; label: string }[] = [
  { id: 'all', label: 'All' },
  { id: 'explicit', label: 'Explicit refs' },
  { id: 'set', label: 'Set-injected' },
  // Deliberately not "Orphans": the sets rail already has an "Unassigned"
  // pill for "belongs to no set", a different axis that also reads as unused.
  { id: 'none', label: 'Not referenced' },
];

export function isRefFilter(v: unknown): v is RefFilter {
  return REF_FILTERS.some((f) => f.id === v);
}

// isSetInjected reports whether any consumer is a bulk set injection.
export function isSetInjected(consumers: Consumer[]): boolean {
  return consumers.some((c) => c.kind === 'secrets-set');
}

// isExplicitlyReferenced reports whether any consumer is a real YAML site.
// Everything that is not a synthetic set injection counts, including
// gateway/network/stack-level sites.
export function isExplicitlyReferenced(consumers: Consumer[]): boolean {
  return consumers.some((c) => c.kind !== 'secrets-set');
}

// matchesRefFilter tests one variable's consumers against a filter.
export function matchesRefFilter(
  consumers: Consumer[],
  filter: RefFilter,
): boolean {
  switch (filter) {
    case 'explicit':
      return isExplicitlyReferenced(consumers);
    case 'set':
      return isSetInjected(consumers);
    case 'none':
      return consumers.length === 0;
    case 'all':
      return true;
  }
}

// filterVariablesByRef narrows variables by how they are consumed.
//
// `usageLoaded` is the honesty gate: with an unknown usage index every
// variable would look unreferenced, so the filter goes inert instead of
// asserting something it cannot know. Callers should also disable the chips.
export function filterVariablesByRef<T extends Pick<Variable, 'key'>>(
  variables: T[],
  filter: RefFilter,
  usage: Record<string, Consumer[]>,
  usageLoaded: boolean,
): T[] {
  if (filter === 'all' || !usageLoaded) return variables;
  return variables.filter((v) => matchesRefFilter(usage[v.key] ?? [], filter));
}

// countByRefFilter returns the size of each class over the *unfiltered* list,
// so the chips read as a summary of the whole vault rather than of whatever
// the search box currently shows. Returns null counts when usage is unknown.
export function countByRefFilter<T extends Pick<Variable, 'key'>>(
  variables: T[],
  usage: Record<string, Consumer[]>,
  usageLoaded: boolean,
): Record<RefFilter, number> | null {
  if (!usageLoaded) return null;
  const counts: Record<RefFilter, number> = {
    all: variables.length,
    explicit: 0,
    set: 0,
    none: 0,
  };
  for (const v of variables) {
    const consumers = usage[v.key] ?? [];
    if (isExplicitlyReferenced(consumers)) counts.explicit++;
    if (isSetInjected(consumers)) counts.set++;
    if (consumers.length === 0) counts.none++;
  }
  return counts;
}
