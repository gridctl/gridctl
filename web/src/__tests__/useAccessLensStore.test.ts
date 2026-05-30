import { describe, it, expect, beforeEach } from 'vitest';
import {
  useAccessLensStore,
  canonical,
  isDirty,
  canSaveDraft,
  buildDraftScope,
} from '../stores/useAccessLensStore';
import type { MCPServerStatus } from '../types';

function server(name: string, tools: string[]): MCPServerStatus {
  return { name, transport: 'http', initialized: true, toolCount: tools.length, tools };
}

const SERVERS: MCPServerStatus[] = [
  server('github', ['search-repos', 'create-issue']),
  server('gitlab', ['list-issues', 'merge-request']),
];

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

describe('buildDraftScope', () => {
  it('surfaces every tool of granted servers when no tool allow-list', () => {
    const scope = buildDraftScope(['github'], SERVERS, []);
    expect(scope.configured).toBe(true);
    expect(scope.unscoped).toBe(false);
    expect(scope.servers).toEqual(['github']);
    expect(scope.tools).toEqual(['github__create-issue', 'github__search-repos']);
  });

  it('treats the tool allow-list as global, gating newly granted servers', () => {
    // Adding gitlab at the server level does NOT surface its tools while the
    // global allow-list pins visibility to github__search-repos (faithful to
    // the backend so the live canvas matches the commit).
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
    });
    const s = useAccessLensStore.getState();
    expect(s.clientSlug).toBe('cursor');
    expect(s.draft).toEqual(['github', 'gitlab']);
    expect(isDirty(s.draft, s.baseline)).toBe(false);
  });

  it('toggleServer mutates only the draft, never the baseline', () => {
    const st = useAccessLensStore.getState();
    st.seed({ slug: 'cursor', name: 'Cursor', baseline: ['github'], savedTools: [], createsBlock: false });
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
    st.seed({ slug: 'cursor', name: 'Cursor', baseline: ['github'], savedTools: [], createsBlock: false });
    st.toggleServer('gitlab');
    st.discardDraft();
    expect(useAccessLensStore.getState().draft).toEqual(['github']);

    st.clearDraft();
    expect(useAccessLensStore.getState().clientSlug).toBeNull();
    expect(useAccessLensStore.getState().draft).toEqual([]);
  });
});
