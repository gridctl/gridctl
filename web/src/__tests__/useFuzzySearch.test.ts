import { describe, it, expect } from 'vitest';
import { renderHook } from '@testing-library/react';
import { useFuzzySearch } from '../hooks/useFuzzySearch';

interface Item {
  name: string;
  description?: string;
}

const items: Item[] = [
  { name: 'create_issue', description: 'Create a GitHub issue' },
  { name: 'list_repos', description: 'List repositories' },
  { name: 'get_page', description: 'Get a Confluence page' },
];

describe('useFuzzySearch', () => {
  it('returns the full list unchanged when the query is blank', () => {
    const { result } = renderHook(() => useFuzzySearch(items, ''));
    expect(result.current).toEqual(items);
  });

  it('fuzzy-filters by name and description', () => {
    const { result } = renderHook(() => useFuzzySearch(items, 'repos'));
    expect(result.current.map((i) => i.name)).toEqual(['list_repos']);
  });

  // Regression: a null/undefined list (e.g. the stackless tool catalog the
  // gateway returns as null) must not reach new Fuse(null), which throws on
  // `.length` and would unmount the whole app.
  it('treats a null list as empty without throwing', () => {
    const { result } = renderHook(() =>
      useFuzzySearch(null as unknown as Item[], ''),
    );
    expect(result.current).toEqual([]);
  });

  it('treats an undefined list as empty without throwing, even with a query', () => {
    const { result } = renderHook(() =>
      useFuzzySearch(undefined as unknown as Item[], 'anything'),
    );
    expect(result.current).toEqual([]);
  });
});
