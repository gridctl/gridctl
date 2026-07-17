import { describe, it, expect } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import type { ReactNode } from 'react';
import {
  CommandRegistryProvider,
  useCommandRegistry,
} from '../hooks/useCommandRegistry';
import type { PaletteCommand } from '../types/palette';

function wrap({ children }: { children: ReactNode }) {
  return <CommandRegistryProvider>{children}</CommandRegistryProvider>;
}

const noop = () => {};

const cmds: PaletteCommand[] = [
  {
    id: 'cross:nav',
    label: 'Cross-workspace nav',
    section: 'global',
    onSelect: noop,
  },
  {
    id: 'stack:thing',
    label: 'Stack-only thing',
    section: 'canvas',
    workspaces: ['stack'],
    onSelect: noop,
  },
  {
    id: 'library:thing',
    label: 'Library-only thing',
    section: 'global',
    workspaces: ['library'],
    onSelect: noop,
  },
];

describe('useCommandRegistry workspace scoping', () => {
  it('returns all commands when no workspace filter is provided', () => {
    const { result } = renderHook(() => useCommandRegistry(), { wrapper: wrap });
    act(() => result.current.registerCommands('test', cmds));
    const ids = result.current.getSortedCommands().map((c) => c.id);
    expect(ids).toEqual(expect.arrayContaining(['cross:nav', 'stack:thing', 'library:thing']));
  });

  it('hides commands tagged for other workspaces', () => {
    const { result } = renderHook(() => useCommandRegistry(), { wrapper: wrap });
    act(() => result.current.registerCommands('test', cmds));

    const onStack = result.current.getSortedCommands(undefined, undefined, 'stack').map((c) => c.id);
    expect(onStack).toContain('cross:nav');
    expect(onStack).toContain('stack:thing');
    expect(onStack).not.toContain('library:thing');
  });

  it('keeps untagged commands visible in every workspace', () => {
    const { result } = renderHook(() => useCommandRegistry(), { wrapper: wrap });
    act(() => result.current.registerCommands('test', cmds));

    for (const ws of ['stack', 'library'] as const) {
      const ids = result.current.getSortedCommands(undefined, undefined, ws).map((c) => c.id);
      expect(ids).toContain('cross:nav');
    }
  });
});
