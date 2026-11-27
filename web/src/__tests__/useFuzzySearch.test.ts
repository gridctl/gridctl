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

  it('finds a match late in the description', () => {
    const late: Item[] = [
      { name: 'notify', description: `${'x'.repeat(120)} webhook` },
      { name: 'other', description: 'Unrelated tool' },
    ];
    const { result } = renderHook(() => useFuzzySearch(late, 'webhook'));
    expect(result.current.map((i) => i.name)).toEqual(['notify']);
  });

  it('finds an exact tool name behind a long server prefix', () => {
    const prefix = 'a'.repeat(45);
    const prefixed: Item[] = [{ name: `${prefix}__create_issue` }, { name: `${prefix}__list_repos` }];
    const { result } = renderHook(() => useFuzzySearch(prefixed, 'create_issue'));
    expect(result.current.map((i) => i.name)).toEqual([`${prefix}__create_issue`]);
  });

  it('ranks a name hit above a description-only hit', () => {
    const ranked: Item[] = [
      { name: 'list_items', description: 'Lists every item on the board' },
      { name: 'get_board', description: 'Fetch one record' },
    ];
    const { result } = renderHook(() => useFuzzySearch(ranked, 'board'));
    expect(result.current.map((i) => i.name)).toEqual(['get_board', 'list_items']);
  });

  it('does not match "click" against an item that only contains "slack"', () => {
    const slack: Item[] = [{ name: 'post_message', description: 'Post to slack' }];
    const { result } = renderHook(() => useFuzzySearch(slack, 'click'));
    expect(result.current).toEqual([]);
  });

  it('searches custom weighted keys', () => {
    interface Row extends Item {
      server: string;
      tool: string;
    }
    const prefix = 'b'.repeat(45);
    const rows: Row[] = [
      { name: `${prefix}__create_issue`, server: prefix, tool: 'create_issue' },
      { name: `${prefix}__list_repos`, server: prefix, tool: 'list_repos' },
    ];
    const { result } = renderHook(() => useFuzzySearch(rows, 'create_issue', { keys: ROW_KEYS }));
    expect(result.current.map((r) => r.tool)).toEqual(['create_issue']);
  });
});

const ROW_KEYS = [
  { name: 'tool', weight: 3 },
  { name: 'server', weight: 1 },
];
