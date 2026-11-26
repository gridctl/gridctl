import { describe, expect, it } from 'vitest';
import { SHORTCUT_BINDINGS, SHORTCUT_SCOPE, SHORTCUT_SCOPE_ORDER } from '../lib/shortcuts';
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

  it('scopes shell chords to Global and fit and zoom to Stack', () => {
    const globalChords = [
      ['Escape'],
      ['Mod', 'Shift', 'R'],
      ['Mod', 'J'],
      ['Mod', 'K'],
      ['Mod', '\\'],
      ['?'],
    ];
    const stackChords = [['f'], ['Mod', '0'], ['Mod', '+'], ['Mod', '−']];
    const scopesOf = (keys: string[]) =>
      SHORTCUT_BINDINGS.filter((binding) => binding.keys.join('+') === keys.join('+')).map((b) => b.scope);
    for (const keys of globalChords) {
      expect(scopesOf(keys), keys.join('+')).toContain('Global');
    }
    for (const keys of stackChords) {
      const scopes = scopesOf(keys).filter((scope) => scope === 'Global' || scope === 'Stack');
      expect(scopes, keys.join('+')).toEqual(['Stack']);
    }
    expect(SHORTCUT_SCOPE_ORDER.indexOf(SHORTCUT_SCOPE.stack)).toBe(
      SHORTCUT_SCOPE_ORDER.indexOf(SHORTCUT_SCOPE.workspaces) + 1,
    );
  });

  it('lists the / filter once under the Stack scope', () => {
    const rows = SHORTCUT_BINDINGS.filter(
      (binding) => binding.scope === SHORTCUT_SCOPE.stack && binding.keys.join('+') === '/',
    );
    expect(rows).toEqual([{ keys: ['/'], label: 'Filter', scope: 'Stack' }]);
    expect(SHORTCUT_SCOPE_ORDER.filter((scope) => scope === SHORTCUT_SCOPE.stack)).toHaveLength(1);
  });

  it('stores Mod and Shift tokens instead of Mac symbols', () => {
    for (const binding of SHORTCUT_BINDINGS) {
      for (const key of binding.keys) {
        expect(key, binding.label).not.toMatch(/[⌘⇧]/);
      }
    }
  });

  it('generates one Workspaces row per workspace config label', () => {
    const rows = SHORTCUT_BINDINGS.filter((binding) => binding.scope === SHORTCUT_SCOPE.workspaces);
    expect(rows).toHaveLength(9);
    expect(rows.map((row) => row.label)).toEqual(WORKSPACE_CONFIG.map((workspace) => workspace.label));
    expect(rows.map((row) => row.keys)).toEqual(
      WORKSPACE_CONFIG.map((workspace) => ['Mod', workspace.shortcutKey]),
    );
  });

  it('lists the Pins approve chord under its own scope', () => {
    expect(SHORTCUT_SCOPE_ORDER).toContain(SHORTCUT_SCOPE.pins);
    const rows = SHORTCUT_BINDINGS.filter((binding) => binding.scope === SHORTCUT_SCOPE.pins);
    expect(rows).toEqual([{ keys: ['Mod', 'Enter'], label: 'Approve drift', scope: 'Pins' }]);
  });
});
