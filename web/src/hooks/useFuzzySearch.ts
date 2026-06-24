import { useMemo } from 'react';
import Fuse from 'fuse.js';

// Anything searchable by name + optional description. AgentSkill, the tool rows
// in ToolsEditor/ToolsPicker, and similar name/description records all satisfy
// this — the hook is generic so each call site keeps its own item type.
export interface FuzzySearchable {
  name: string;
  description?: string;
}

// Fuzzy-filter a list of name/description records by query, returning the full
// list unchanged when the query is blank. Memoized on the source list and the
// query so re-renders that change neither are free.
//
// `items` is coalesced to an empty list defensively: callers can pass a store
// value that is momentarily null/undefined (e.g. a catalog the gateway returns
// as null in stackless mode), and `new Fuse(null)` would throw on `.length`.
export function useFuzzySearch<T extends FuzzySearchable>(items: T[], query: string): T[] {
  const list = useMemo(() => items ?? [], [items]);

  const fuse = useMemo(
    () => new Fuse(list, { keys: ['name', 'description'], threshold: 0.4 }),
    [list],
  );

  return useMemo(() => {
    if (!query.trim()) return list;
    return fuse.search(query).map((r) => r.item);
  }, [fuse, query, list]);
}
