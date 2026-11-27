import { useMemo } from 'react';
import Fuse, { type FuseOptionKey } from 'fuse.js';

// Anything searchable by name + optional description. AgentSkill, the tool rows
// in ToolsEditor/ToolsPicker, and similar name/description records all satisfy
// this — the hook is generic so each call site keeps its own item type.
export interface FuzzySearchable {
  name: string;
  description?: string;
}

export interface FuzzySearchOptions<T> {
  // Weighted keys replacing the name/description default. Must be
  // referentially stable (a module-level constant), or the Fuse index is
  // rebuilt on every render.
  keys?: FuseOptionKey<T>[];
}

// Name hits outrank description-only hits.
const DEFAULT_KEYS: FuseOptionKey<FuzzySearchable>[] = [
  { name: 'name', weight: 2 },
  { name: 'description', weight: 1 },
];

// Fuzzy-filter a list of name/description records by query, returning the full
// list unchanged when the query is blank. Memoized on the source list and the
// query so re-renders that change neither are free.
//
// Location is ignored because Fuse otherwise scores a match by its distance
// from the start of the field, dropping exact hits more than about 40
// characters into a description or a long prefixed name. Without that
// penalty a 0.4 threshold floods results with two-edit matches ("click"
// matching "slack"), so the threshold is 0.3: exact substrings and one-typo
// queries still match.
//
// `items` is coalesced to an empty list defensively: callers can pass a store
// value that is momentarily null/undefined (e.g. a catalog the gateway returns
// as null in stackless mode), and `new Fuse(null)` would throw on `.length`.
export function useFuzzySearch<T extends FuzzySearchable>(
  items: T[],
  query: string,
  options?: FuzzySearchOptions<T>,
): T[] {
  const list = useMemo(() => items ?? [], [items]);
  const keys = options?.keys ?? (DEFAULT_KEYS as FuseOptionKey<T>[]);

  const fuse = useMemo(
    () => new Fuse(list, { keys, threshold: 0.3, ignoreLocation: true }),
    [list, keys],
  );

  return useMemo(() => {
    if (!query.trim()) return list;
    return fuse.search(query).map((r) => r.item);
  }, [fuse, query, list]);
}
