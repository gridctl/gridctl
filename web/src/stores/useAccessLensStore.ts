import { create } from 'zustand';
import type { ClientScopeResult, MCPServerStatus } from '../types';

// canonical de-dupes and sorts a server-name list so draft/baseline comparisons
// and YAML-bound output are order-independent.
export function canonical(names: string[]): string[] {
  return Array.from(new Set(names)).sort();
}

// isDirty reports whether a draft selection differs from its saved baseline.
export function isDirty(draft: string[], baseline: string[]): boolean {
  const a = canonical(draft);
  const b = canonical(baseline);
  if (a.length !== b.length) return true;
  return a.some((v, i) => v !== b[i]);
}

// canSaveDraft mirrors useClientScopeEditor.canSave: a save needs at least one
// server selected (an empty list means "all" in the backend model, never
// "deny") AND a change from the baseline.
export function canSaveDraft(draft: string[], baseline: string[]): boolean {
  return draft.length > 0 && isDirty(draft, baseline);
}

/**
 * buildDraftScope derives the ClientScopeResult a client WOULD have under a
 * draft server selection, faithfully replicating the backend's global tool
 * allow-list semantics so the live canvas preview matches what a commit writes:
 *
 *  - The tool axis is global, not per-server. When the client has a saved tool
 *    allow-list, reachable tools are that list intersected with the drafted
 *    servers' tools (adding a server does NOT surface its tools if they aren't
 *    in the allow-list). With no saved allow-list, every tool of a drafted
 *    server is reachable.
 *  - Reachable servers are derived from the reachable tools (a server with no
 *    visible tool contributes nothing), matching pkg/mcp.scopeResult.
 *
 * The result feeds usePathHighlight in place of the saved effectiveScope, so the
 * dim/light updates instantly as the operator toggles.
 */
export function buildDraftScope(
  draftServers: string[],
  servers: MCPServerStatus[],
  savedTools: string[],
): ClientScopeResult {
  const granted = new Set(draftServers);
  const toolAllow = savedTools.length ? new Set(savedTools) : null;
  const tools: string[] = [];
  for (const s of servers) {
    if (!granted.has(s.name)) continue;
    for (const t of s.tools ?? []) {
      const prefixed = `${s.name}__${t}`;
      if (toolAllow && !toolAllow.has(prefixed)) continue;
      tools.push(prefixed);
    }
  }
  const reachableServers = new Set<string>();
  for (const t of tools) {
    const idx = t.indexOf('__');
    if (idx > 0) reachableServers.add(t.slice(0, idx));
  }
  return {
    configured: true,
    unscoped: false,
    servers: [...reachableServers].sort(),
    tools: tools.sort(),
  };
}

// SeedParams describes the saved state the draft is initialized from when a
// client becomes the Access Lens target.
export interface SeedParams {
  slug: string;
  name: string;
  baseline: string[];
  savedTools: string[];
  createsBlock: boolean;
}

interface AccessLensState {
  // Access Lens mode is on. Net-new to Topology (no prior mode toggle).
  enabled: boolean;
  // The slide-over editor (the keyboard-driven twin of canvas node toggling).
  slideOverOpen: boolean;

  // Draft target + saved baseline. clientSlug is the only client the draft
  // applies to; the canvas gates toggling to exactly this selected client.
  clientSlug: string | null;
  clientName: string | null;
  baseline: string[];
  savedTools: string[];
  createsBlock: boolean;

  // The live draft selection (server names). The single source the canvas nodes,
  // the slide-over checkboxes, the action bar, and the highlight all read/write.
  draft: string[];

  conflict: string | null;
  isSaving: boolean;

  setEnabled: (enabled: boolean) => void;
  openSlideOver: () => void;
  closeSlideOver: () => void;
  // seed sets the draft target and resets the draft to the saved baseline. The
  // controller calls it only when the target client (or its baseline) changes,
  // so an in-progress edit on an unchanged client is left alone.
  seed: (params: SeedParams) => void;
  toggleServer: (name: string) => void;
  setDraft: (names: string[]) => void;
  selectAll: (allServerNames: string[]) => void;
  clearAll: () => void;
  // markSaved advances the baseline to the current draft after a successful
  // commit, so the draft is no longer dirty and the action bar retracts (a poll
  // refresh then reseeds from the persisted effectiveScope).
  markSaved: () => void;
  // discardDraft reverts the selection to the saved baseline (keeps the target).
  discardDraft: () => void;
  // clearDraft fully resets the draft target — used after commit, discard, or
  // exiting the mode.
  clearDraft: () => void;
  setConflict: (conflict: string | null) => void;
  setSaving: (saving: boolean) => void;

  // exitNavTarget holds an in-app navigation the dirty-draft guard intercepted.
  // The app uses BrowserRouter (no useBlocker), so the WorkspaceSwitcher cancels
  // the NavLink and stashes the target here; AccessLens confirms, then routes.
  exitNavTarget: string | null;
  requestExitNav: (path: string) => void;
  clearExitNav: () => void;

  // pendingSwitchSlug holds the client the operator selected while a dirty draft
  // for another client was open. Set from the seeding effect (a store update, so
  // it stays out of React setState-in-effect); the confirm renders from it.
  pendingSwitchSlug: string | null;
  requestSwitch: (slug: string) => void;
  clearSwitch: () => void;
}

const EMPTY_TARGET = {
  clientSlug: null,
  clientName: null,
  baseline: [] as string[],
  savedTools: [] as string[],
  createsBlock: false,
  draft: [] as string[],
  conflict: null,
  exitNavTarget: null as string | null,
  pendingSwitchSlug: null as string | null,
};

export const useAccessLensStore = create<AccessLensState>((set, get) => ({
  enabled: false,
  slideOverOpen: false,
  ...EMPTY_TARGET,
  isSaving: false,

  setEnabled: (enabled) => set({ enabled }),
  openSlideOver: () => set({ slideOverOpen: true }),
  closeSlideOver: () => set({ slideOverOpen: false }),

  seed: ({ slug, name, baseline, savedTools, createsBlock }) =>
    set({
      clientSlug: slug,
      clientName: name,
      baseline: canonical(baseline),
      savedTools,
      createsBlock,
      draft: canonical(baseline),
      conflict: null,
    }),

  toggleServer: (name) => {
    const next = new Set(get().draft);
    if (next.has(name)) next.delete(name);
    else next.add(name);
    set({ draft: [...next] });
  },
  setDraft: (names) => set({ draft: [...names] }),
  selectAll: (allServerNames) => set({ draft: canonical(allServerNames) }),
  clearAll: () => set({ draft: [] }),

  markSaved: () => set((s) => ({ baseline: canonical(s.draft), conflict: null })),
  discardDraft: () => set((s) => ({ draft: canonical(s.baseline), conflict: null })),
  clearDraft: () => set({ ...EMPTY_TARGET, slideOverOpen: false }),

  setConflict: (conflict) => set({ conflict }),
  setSaving: (isSaving) => set({ isSaving }),

  requestExitNav: (path) => set({ exitNavTarget: path }),
  clearExitNav: () => set({ exitNavTarget: null }),

  requestSwitch: (slug) => set({ pendingSwitchSlug: slug }),
  clearSwitch: () => set({ pendingSwitchSlug: null }),
}));
