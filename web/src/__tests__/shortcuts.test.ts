import { describe, expect, it } from 'vitest';
import { SHORTCUT_BINDINGS, SHORTCUT_SCOPE } from '../lib/shortcuts';
import { WORKSPACE_CONFIG } from '../types/workspace';

function pairKey(keys: string[], scope: string): string {
  return `${scope}|${keys.join('+')}`;
}

describe('SHORTCUT_BINDINGS', () => {
  it('lists each chord once per scope', () => {
    const seen = new Set<string>();
    for (const binding of SHORTCUT_BINDINGS) {
      const key = pairKey(binding.keys, binding.scope);
      expect(seen.has(key), key).toBe(false);
      seen.add(key);
    }
  });

  it('scopes global handler chords to Global, not Stack', () => {
    const globalChords = [
      ['f'],
      ['⌘', '0'],
      ['Escape'],
      ['⌘', '+'],
      ['⌘', '−'],
      ['⌘', '⇧', 'R'],
      ['⌘', 'J'],
      ['⌘', 'K'],
      ['⌘', '\\'],
      ['?'],
    ];
    for (const keys of globalChords) {
      const row = SHORTCUT_BINDINGS.find(
        (binding) => binding.keys.join('+') === keys.join('+') && binding.scope === SHORTCUT_SCOPE.global,
      );
      expect(row, keys.join('+')).toBeDefined();
      expect(row?.scope).toBe('Global');
    }
    expect(SHORTCUT_BINDINGS.some((binding) => binding.scope === 'Stack')).toBe(false);
  });

  it('generates one Workspaces row per workspace config label', () => {
    const rows = SHORTCUT_BINDINGS.filter((binding) => binding.scope === SHORTCUT_SCOPE.workspaces);
    expect(rows).toHaveLength(9);
    expect(rows.map((row) => row.label)).toEqual(WORKSPACE_CONFIG.map((workspace) => workspace.label));
    expect(rows.map((row) => row.keys)).toEqual(
      WORKSPACE_CONFIG.map((workspace) => ['⌘', workspace.shortcutKey]),
    );
  });
});
