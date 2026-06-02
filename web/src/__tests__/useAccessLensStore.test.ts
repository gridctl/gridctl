import { describe, it, expect, beforeEach } from 'vitest';
import {
  useAccessLensStore,
  canonical,
  isDirty,
  canSaveDraft,
  buildDraftScope,
  flattenTools,
  groupToolsByServer,
  seedToolState,
  hasEmptyCustomGrant,
} from '../stores/useAccessLensStore';
import type { MCPServerStatus } from '../types';

function server(name: string, tools: string[]): MCPServerStatus {
  return { name, transport: 'http', initialized: true, toolCount: tools.length, tools };
}

const SERVERS: MCPServerStatus[] = [
  server('github', ['search-repos', 'create-issue']),
  server('gitlab', ['list-issues', 'merge-request']),
];

// The live tool universe the store is seeded with, mirroring MCPServerStatus.tools.
const UNIVERSE: Record<string, string[]> = {
  github: ['search-repos', 'create-issue'],
  gitlab: ['list-issues', 'merge-request'],
};

describe('access-lens pure helpers', () => {
  it('canonical de-dupes and sorts', () => {
    expect(canonical(['gitlab', 'github', 'gitlab'])).toEqual(['github', 'gitlab']);
  });

  it('isDirty is order-independent', () => {
    expect(isDirty(['github', 'gitlab'], ['gitlab', 'github'])).toBe(false);
    expect(isDirty(['github'], ['gitlab', 'github'])).toBe(true);
  });

  it('canSaveDraft forbids an empty selection even when dirty', () => {
    // Empty means "all" in the backend model, so it can never express deny.
    expect(canSaveDraft([], ['github'])).toBe(false);
    expect(canSaveDraft(['github'], ['github', 'gitlab'])).toBe(true);
    expect(canSaveDraft(['github', 'gitlab'], ['github', 'gitlab'])).toBe(false);
  });
});

describe('groupToolsByServer', () => {
  it('splits prefixed names on the first delimiter and sorts', () => {
    expect(groupToolsByServer(['github__create-issue', 'github__search-repos', 'gitlab__x'])).toEqual({
      github: ['create-issue', 'search-repos'],
      gitlab: ['x'],
    });
  });

  it('keeps delimiters that appear inside a tool name', () => {
    expect(groupToolsByServer(['fs__read__file'])).toEqual({ fs: ['read__file'] });
  });
});

describe('flattenTools (global allow-list semantics)', () => {
  it('returns [] when every granted server is unrestricted (all)', () => {
    expect(flattenTools(['github', 'gitlab'], UNIVERSE, {}, {})).toEqual([]);
  });

  it('returns [] when a custom server still covers its whole tool set', () => {
    const customSel = { github: ['search-repos', 'create-issue'] };
    expect(flattenTools(['github'], UNIVERSE, { github: 'custom' }, customSel)).toEqual([]);
  });

  it('enumerates every granted server once ANY server narrows (the key invariant)', () => {
    // github is narrowed to one tool; gitlab is "all" but MUST be enumerated in
    // full, or the global list would silently hide it.
    const out = flattenTools(
      ['github', 'gitlab'],
      UNIVERSE,
      { github: 'custom', gitlab: 'all' },
      { github: ['search-repos'] },
    );
    expect(out).toEqual([
      'github__search-repos',
      'gitlab__list-issues',
      'gitlab__merge-request',
    ]);
  });

  it('does not enumerate revoked servers', () => {
    const out = flattenTools(
      ['github'],
      UNIVERSE,
      { github: 'custom', gitlab: 'all' },
      { github: ['search-repos'] },
    );
    expect(out).toEqual(['github__search-repos']);
  });
});

describe('seedToolState', () => {
  it('reconstructs "all" for a saved subset that covers the full tool set', () => {
    const seed = seedToolState(
      ['github__search-repos', 'github__create-issue'],
      UNIVERSE,
      ['github'],
    );
    expect(seed.toolMode.github).toBe('all');
    // A lone "all" server flattens to [] (unrestricted), so the dirty baseline
    // is [] even though the raw saved list was non-empty.
    expect(seed.baselineTools).toEqual([]);
  });

  it('reconstructs "custom" for a strict subset', () => {
    const seed = seedToolState(['github__search-repos'], UNIVERSE, ['github']);
    expect(seed.toolMode.github).toBe('custom');
    expect(seed.customSel.github).toEqual(['search-repos']);
    expect(seed.baselineTools).toEqual(['github__search-repos']);
  });
});

describe('hasEmptyCustomGrant', () => {
  it('flags a granted server in custom mode with nothing selected', () => {
    expect(hasEmptyCustomGrant(['github'], { github: 'custom' }, { github: [] })).toBe(true);
    expect(hasEmptyCustomGrant(['github'], { github: 'custom' }, { github: ['x'] })).toBe(false);
    expect(hasEmptyCustomGrant(['github'], { github: 'all' }, {})).toBe(false);
    // An empty custom server that is NOT granted is irrelevant.
    expect(hasEmptyCustomGrant([], { github: 'custom' }, { github: [] })).toBe(false);
  });
});

describe('buildDraftScope', () => {
  it('surfaces every tool of granted servers when no tool allow-list', () => {
    const scope = buildDraftScope(['github'], SERVERS, []);
    expect(scope.configured).toBe(true);
    expect(scope.unscoped).toBe(false);
    expect(scope.servers).toEqual(['github']);
    expect(scope.tools).toEqual(['github__create-issue', 'github__search-repos']);
  });

  it('treats the tool allow-list as global, gating newly granted servers', () => {
    const scope = buildDraftScope(['github', 'gitlab'], SERVERS, ['github__search-repos']);
    expect(scope.tools).toEqual(['github__search-repos']);
    expect(scope.servers).toEqual(['github']);
  });

  it('drops a granted server that surfaces no visible tool', () => {
    const scope = buildDraftScope(['github', 'gitlab'], SERVERS, ['gitlab__list-issues']);
    expect(scope.servers).toEqual(['gitlab']);
    expect(scope.tools).toEqual(['gitlab__list-issues']);
  });
});

// Computes the tools axis a commit would send, exactly as AccessLensCommitGate
// does: omit it (undefined) unless the operator deliberately touched a tool
// group AND the flattened intent differs from the saved baseline.
function toolsAxisToSend(): string[] | undefined {
  const s = useAccessLensStore.getState();
  const flat = flattenTools(s.draft, s.serverTools, s.toolMode, s.customSel);
  const dirty = s.toolsTouched && isDirty(flat, s.baselineTools);
  return dirty ? flat : undefined;
}

describe('useAccessLensStore', () => {
  beforeEach(() => {
    useAccessLensStore.getState().clearDraft();
    useAccessLensStore.setState({ enabled: false, isSaving: false });
  });

  it('seeds the draft from the saved baseline', () => {
    useAccessLensStore.getState().seed({
      slug: 'cursor',
      name: 'Cursor',
      baseline: ['gitlab', 'github'],
      savedTools: [],
      createsBlock: false,
      serverTools: UNIVERSE,
    });
    const s = useAccessLensStore.getState();
    expect(s.clientSlug).toBe('cursor');
    expect(s.draft).toEqual(['github', 'gitlab']);
    expect(isDirty(s.draft, s.baseline)).toBe(false);
  });

  it('toggleServer mutates only the draft, never the baseline', () => {
    const st = useAccessLensStore.getState();
    st.seed({ slug: 'cursor', name: 'Cursor', baseline: ['github'], savedTools: [], createsBlock: false, serverTools: UNIVERSE });
    st.toggleServer('gitlab');
    let s = useAccessLensStore.getState();
    expect(s.draft.sort()).toEqual(['github', 'gitlab']);
    expect(s.baseline).toEqual(['github']);
    expect(isDirty(s.draft, s.baseline)).toBe(true);

    st.toggleServer('gitlab');
    s = useAccessLensStore.getState();
    expect(s.draft).toEqual(['github']);
    expect(isDirty(s.draft, s.baseline)).toBe(false);
  });

  it('discardDraft reverts to the baseline; clearDraft resets the target', () => {
    const st = useAccessLensStore.getState();
    st.seed({ slug: 'cursor', name: 'Cursor', baseline: ['github'], savedTools: [], createsBlock: false, serverTools: UNIVERSE });
    st.toggleServer('gitlab');
    st.discardDraft();
    expect(useAccessLensStore.getState().draft).toEqual(['github']);

    st.clearDraft();
    expect(useAccessLensStore.getState().clientSlug).toBeNull();
    expect(useAccessLensStore.getState().draft).toEqual([]);
  });
});

// The four "touched vs. untouched tools axis" cases the feature hinges on
// (v1 acceptance criterion 4). Each asserts exactly what a commit would send.
describe('tools axis: touched vs untouched (preserve-vs-replace)', () => {
  beforeEach(() => {
    useAccessLensStore.getState().clearDraft();
  });

  it('case a: no saved tools, server-only edit -> tools omitted', () => {
    const st = useAccessLensStore.getState();
    st.seed({ slug: 'c', name: 'C', baseline: ['github'], savedTools: [], createsBlock: false, serverTools: UNIVERSE });
    st.toggleServer('gitlab');
    expect(toolsAxisToSend()).toBeUndefined();
  });

  it('case b: existing tool list, server-only edit -> tools omitted (preserved)', () => {
    const st = useAccessLensStore.getState();
    st.seed({
      slug: 'c',
      name: 'C',
      baseline: ['github'],
      savedTools: ['github__search-repos'],
      createsBlock: false,
      serverTools: UNIVERSE,
    });
    // Grant another server but never open a tool group.
    st.toggleServer('gitlab');
    expect(useAccessLensStore.getState().toolsTouched).toBe(false);
    expect(toolsAxisToSend()).toBeUndefined();
  });

  it('case c: open a tool group and narrow -> tools sent, enumerating "all" servers', () => {
    const st = useAccessLensStore.getState();
    st.seed({ slug: 'c', name: 'C', baseline: ['github', 'gitlab'], savedTools: [], createsBlock: false, serverTools: UNIVERSE });
    st.setServerToolMode('github', 'custom');
    st.toggleTool('github', 'search-repos');
    // github narrowed to one tool; gitlab stays "all" and is enumerated in full.
    expect(toolsAxisToSend()).toEqual([
      'github__search-repos',
      'gitlab__list-issues',
      'gitlab__merge-request',
    ]);
  });

  it('case d: All -> Custom -> All -> Custom restores the remembered subset', () => {
    const st = useAccessLensStore.getState();
    st.seed({ slug: 'c', name: 'C', baseline: ['github'], savedTools: [], createsBlock: false, serverTools: UNIVERSE });
    st.setServerToolMode('github', 'custom');
    st.toggleTool('github', 'search-repos');
    // Back to All — the selection persists in customSel, intent flattens to [].
    st.setServerToolMode('github', 'all');
    expect(flattenTools(['github'], UNIVERSE, useAccessLensStore.getState().toolMode, useAccessLensStore.getState().customSel)).toEqual([]);
    // Forward to Custom again — the prior subset is restored, not wiped.
    st.setServerToolMode('github', 'custom');
    expect(useAccessLensStore.getState().customSel.github).toEqual(['search-repos']);
  });

  it('blocks save when a granted server is custom with nothing selected', () => {
    const st = useAccessLensStore.getState();
    st.seed({ slug: 'c', name: 'C', baseline: ['github'], savedTools: [], createsBlock: false, serverTools: UNIVERSE });
    st.setServerToolMode('github', 'custom');
    const s = useAccessLensStore.getState();
    expect(hasEmptyCustomGrant(s.draft, s.toolMode, s.customSel)).toBe(true);
  });

  it('setCustomTools replaces the selection outright (canvas "all minus one")', () => {
    const st = useAccessLensStore.getState();
    st.seed({ slug: 'c', name: 'C', baseline: ['github'], savedTools: [], createsBlock: false, serverTools: UNIVERSE });
    // Clicking one pill on an unrestricted server: custom with every OTHER tool.
    st.setServerToolMode('github', 'custom');
    st.setCustomTools('github', ['create-issue']);
    expect(useAccessLensStore.getState().customSel.github).toEqual(['create-issue']);
    // The flattened axis enumerates only the surviving tool (search-repos hidden).
    expect(toolsAxisToSend()).toEqual(['github__create-issue']);
  });

  it('clearTools with a list removes only those tools (filtered clear)', () => {
    const st = useAccessLensStore.getState();
    st.seed({ slug: 'c', name: 'C', baseline: ['github'], savedTools: [], createsBlock: false, serverTools: UNIVERSE });
    st.setServerToolMode('github', 'custom');
    st.selectAllTools('github', ['search-repos', 'create-issue']);
    // A filtered "clear all" passes only the visible subset; the hidden pick stays.
    st.clearTools('github', ['create-issue']);
    expect(useAccessLensStore.getState().customSel.github).toEqual(['search-repos']);
    // No list clears the whole server.
    st.clearTools('github');
    expect(useAccessLensStore.getState().customSel.github).toEqual([]);
  });

  it('clearing all restrictions sends [] (deliberate widen), not omit', () => {
    const st = useAccessLensStore.getState();
    st.seed({
      slug: 'c',
      name: 'C',
      baseline: ['github'],
      savedTools: ['github__search-repos'],
      createsBlock: false,
      serverTools: UNIVERSE,
    });
    // Operator flips the restricted server back to All — a deliberate widen.
    st.setServerToolMode('github', 'all');
    expect(toolsAxisToSend()).toEqual([]);
  });
});
