import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import '@testing-library/jest-dom';
import { CommandRegistryProvider, useCommandRegistry } from '../hooks/useCommandRegistry';
import type { PaletteCommand } from '../types/palette';

// --- Helpers ---

const wrapper = ({ children }: { children: React.ReactNode }) => (
  <CommandRegistryProvider>{children}</CommandRegistryProvider>
);

function makeCommand(id: string, label: string, section: PaletteCommand['section'] = 'global'): PaletteCommand {
  return { id, label, section, onSelect: vi.fn() };
}

// --- Tests ---

describe('useCommandRegistry', () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
  });

  it('throws when used outside CommandRegistryProvider', () => {
    const spy = vi.spyOn(console, 'error').mockImplementation(() => {});
    expect(() => renderHook(() => useCommandRegistry())).toThrow(
      'useCommandRegistry must be used within a CommandRegistryProvider',
    );
    spy.mockRestore();
  });

  // ── Registration ──────────────────────────────────────────────────────────

  describe('registerCommands / unregisterCommands', () => {
    it('makes registered commands available', () => {
      const { result } = renderHook(() => useCommandRegistry(), { wrapper });
      const cmd = makeCommand('navigate:traces', 'Open Traces');

      act(() => result.current.registerCommands('nav', [cmd]));

      expect(result.current.commands).toHaveLength(1);
      expect(result.current.commands[0].id).toBe('navigate:traces');
    });

    it('merges commands from multiple sections', () => {
      const { result } = renderHook(() => useCommandRegistry(), { wrapper });
      const nav = makeCommand('navigate:traces', 'Open Traces');
      const canvas = makeCommand('canvas:zoom', 'Zoom to fit', 'canvas');

      act(() => {
        result.current.registerCommands('nav', [nav]);
        result.current.registerCommands('canvas', [canvas]);
      });

      expect(result.current.commands).toHaveLength(2);
    });

    it('replaces commands when re-registering the same section', () => {
      const { result } = renderHook(() => useCommandRegistry(), { wrapper });

      act(() => result.current.registerCommands('nav', [makeCommand('a', 'Command A')]));
      act(() => result.current.registerCommands('nav', [makeCommand('b', 'Command B')]));

      expect(result.current.commands).toHaveLength(1);
      expect(result.current.commands[0].id).toBe('b');
    });

    it('removes commands after unregistration', () => {
      const { result } = renderHook(() => useCommandRegistry(), { wrapper });
      const cmd = makeCommand('navigate:traces', 'Open Traces');

      act(() => result.current.registerCommands('nav', [cmd]));
      act(() => result.current.unregisterCommands('nav'));

      expect(result.current.commands).toHaveLength(0);
    });
  });

  // ── getSortedCommands ─────────────────────────────────────────────────────

  describe('getSortedCommands', () => {
    it('returns all commands when no query or scope provided', () => {
      const { result } = renderHook(() => useCommandRegistry(), { wrapper });
      const cmd1 = makeCommand('navigate:traces', 'Open Traces');
      const cmd2 = makeCommand('canvas:zoom', 'Zoom to fit', 'canvas');

      act(() => result.current.registerCommands('test', [cmd1, cmd2]));

      expect(result.current.getSortedCommands()).toHaveLength(2);
    });

    it('filters by section when scope is provided', () => {
      const { result } = renderHook(() => useCommandRegistry(), { wrapper });
      const navCmd = makeCommand('navigate:traces', 'Open Traces', 'global');
      const canvasCmd = makeCommand('canvas:zoom', 'Zoom to fit', 'canvas');

      act(() => result.current.registerCommands('test', [navCmd, canvasCmd]));

      const results = result.current.getSortedCommands(undefined, 'canvas');
      expect(results).toHaveLength(1);
      expect(results[0].id).toBe('canvas:zoom');
    });

    it('filters by fuzzy query on label', () => {
      const { result } = renderHook(() => useCommandRegistry(), { wrapper });
      const cmd1 = makeCommand('navigate:traces', 'Open Traces');
      const cmd2 = makeCommand('navigate:vault', 'Open Variables');

      act(() => result.current.registerCommands('test', [cmd1, cmd2]));

      const results = result.current.getSortedCommands('trac');
      expect(results).toHaveLength(1);
      expect(results[0].id).toBe('navigate:traces');
    });

    it('filters by fuzzy query on keywords', () => {
      const { result } = renderHook(() => useCommandRegistry(), { wrapper });
      const cmd = { ...makeCommand('canvas:zoom', 'Zoom to fit', 'canvas'), keywords: ['fitview', 'overview'] };

      act(() => result.current.registerCommands('test', [cmd]));

      expect(result.current.getSortedCommands('fitview')).toHaveLength(1);
    });

    it('returns empty when query has no matches', () => {
      const { result } = renderHook(() => useCommandRegistry(), { wrapper });
      const cmd = makeCommand('navigate:traces', 'Open Traces');

      act(() => result.current.registerCommands('test', [cmd]));

      expect(result.current.getSortedCommands('xyznotfound')).toHaveLength(0);
    });

    it('ranks frequently used commands higher', () => {
      const { result } = renderHook(() => useCommandRegistry(), { wrapper });
      const cmd1 = makeCommand('a', 'Alpha');
      const cmd2 = makeCommand('b', 'Beta');

      act(() => result.current.registerCommands('test', [cmd1, cmd2]));
      // Record 'b' as used more frequently
      act(() => {
        result.current.recordUsage('b');
        result.current.recordUsage('b');
        result.current.recordUsage('a');
      });

      const results = result.current.getSortedCommands();
      expect(results[0].id).toBe('b');
    });
  });

  // ── recordUsage ───────────────────────────────────────────────────────────

  describe('recordUsage', () => {
    it('persists frecency data to localStorage', () => {
      const { result } = renderHook(() => useCommandRegistry(), { wrapper });

      act(() => result.current.recordUsage('navigate:traces'));

      const stored = JSON.parse(localStorage.getItem('gridctl-palette-frecency') ?? '{}');
      expect(stored['navigate:traces']).toBeDefined();
      expect(stored['navigate:traces'].count).toBe(1);
    });

    it('increments count on repeated usage', () => {
      const { result } = renderHook(() => useCommandRegistry(), { wrapper });

      act(() => {
        result.current.recordUsage('navigate:traces');
        result.current.recordUsage('navigate:traces');
      });

      const stored = JSON.parse(localStorage.getItem('gridctl-palette-frecency') ?? '{}');
      expect(stored['navigate:traces'].count).toBe(2);
    });

    it('updates lastUsed timestamp on usage', () => {
      const { result } = renderHook(() => useCommandRegistry(), { wrapper });
      const before = Date.now();

      act(() => result.current.recordUsage('navigate:traces'));

      const stored = JSON.parse(localStorage.getItem('gridctl-palette-frecency') ?? '{}');
      expect(stored['navigate:traces'].lastUsed).toBeGreaterThanOrEqual(before);
    });
  });

  // ── getRecentCommands ─────────────────────────────────────────────────────

  describe('getRecentCommands', () => {
    it('returns commands that have frecency data', () => {
      const { result } = renderHook(() => useCommandRegistry(), { wrapper });
      const cmd = makeCommand('navigate:traces', 'Open Traces');

      act(() => result.current.registerCommands('test', [cmd]));
      act(() => result.current.recordUsage('navigate:traces'));

      expect(result.current.getRecentCommands()).toHaveLength(1);
      expect(result.current.getRecentCommands()[0].id).toBe('navigate:traces');
    });

    it('excludes commands that have never been used', () => {
      const { result } = renderHook(() => useCommandRegistry(), { wrapper });
      const cmd = makeCommand('navigate:traces', 'Open Traces');

      act(() => result.current.registerCommands('test', [cmd]));

      expect(result.current.getRecentCommands()).toHaveLength(0);
    });

    it('respects the limit parameter', () => {
      const { result } = renderHook(() => useCommandRegistry(), { wrapper });
      const cmds = ['a', 'b', 'c', 'd', 'e'].map((id) => makeCommand(id, `cmd-${id}`));

      act(() => result.current.registerCommands('test', cmds));
      act(() => { cmds.forEach((c) => result.current.recordUsage(c.id)); });

      expect(result.current.getRecentCommands(3)).toHaveLength(3);
    });

    it('sorts by most recently used first', () => {
      const { result } = renderHook(() => useCommandRegistry(), { wrapper });
      const cmd1 = makeCommand('a', 'Alpha');
      const cmd2 = makeCommand('b', 'Beta');

      act(() => result.current.registerCommands('test', [cmd1, cmd2]));

      // Ensure distinct timestamps so sort order is deterministic
      const nowSpy = vi.spyOn(Date, 'now');
      nowSpy.mockReturnValueOnce(1000);
      act(() => result.current.recordUsage('a'));
      nowSpy.mockReturnValueOnce(2000);
      act(() => result.current.recordUsage('b'));
      nowSpy.mockRestore();

      // 'b' used last → should be first
      const recent = result.current.getRecentCommands();
      expect(recent[0].id).toBe('b');
    });
  });

  // ── Frecency persistence ──────────────────────────────────────────────────

  describe('frecency persistence', () => {
    it('loads frecency data from localStorage on initialization', () => {
      // Pre-seed localStorage before the hook mounts
      localStorage.setItem(
        'gridctl-palette-frecency',
        JSON.stringify({ 'navigate:traces': { count: 5, lastUsed: Date.now() - 1000 } }),
      );

      const { result } = renderHook(() => useCommandRegistry(), { wrapper });
      const cmd = makeCommand('navigate:traces', 'Open Traces');

      act(() => result.current.registerCommands('test', [cmd]));

      // Command with pre-existing frecency data should show in recent list
      const recent = result.current.getRecentCommands();
      expect(recent).toHaveLength(1);
      expect(recent[0].id).toBe('navigate:traces');
    });

    it('handles corrupted localStorage gracefully', () => {
      localStorage.setItem('gridctl-palette-frecency', '{invalid json}');

      // Should not throw; falls back to empty frecency map
      expect(() => renderHook(() => useCommandRegistry(), { wrapper })).not.toThrow();
    });
  });
});
