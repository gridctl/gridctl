import { create } from 'zustand';
import type { ClientScopeResult, MCPServerStatus } from '../types';
import { TOOL_NAME_DELIMITER } from '../lib/constants';

// canonical de-dupes and sorts a name list so draft/baseline comparisons and
// YAML-bound output are order-independent. Used for both server and (prefixed)
// tool lists.
export function canonical(names: string[]): string[] {
  return Array.from(new Set(names)).sort();
}

// isDirty reports whether a draft selection differs from its saved baseline.
// Shared by the server axis and the (prefixed) tool axis.
export function isDirty(draft: string[], baseline: string[]): boolean {
  const a = canonical(draft);
  const b = canonical(baseline);
  if (a.length !== b.length) return true;
  return a.some((v, i) => v !== b[i]);
}

// canSaveDraft mirrors useClientScopeEditor.canSave for the SERVER axis: a save
// needs at least one server selected (an empty list means "all" in the backend
// model, never "deny") AND a change from the baseline. The tool axis adds its
// own conditions on top of this — see canCommitDraft.
export function canSaveDraft(draft: string[], baseline: string[]): boolean {
  return draft.length > 0 && isDirty(draft, baseline);
}

// ToolMode is one granted server's per-client tool exposure: "all" tools of the
// server (the default), or a "custom" subset. It is purely a UI/intent concept —
// the backend has no per-server tool axis, only a single flat allow-list.
export type ToolMode = 'all' | 'custom';

// splitPrefixed splits "server__tool" on the FIRST delimiter (tool names may
// themselves contain the delimiter), matching the backend's ParsePrefixedTool.
function splitPrefixed(prefixed: string): { server: string; tool: string } | null {
  const idx = prefixed.indexOf(TOOL_NAME_DELIMITER);
  if (idx <= 0) return null;
  return {
    server: prefixed.slice(0, idx),
    tool: prefixed.slice(idx + TOOL_NAME_DELIMITER.length),
  };
}

// groupToolsByServer turns a flat list of prefixed tool names into a map of
// server -> sorted, de-duped unprefixed tool names.
export function groupToolsByServer(prefixed: string[]): Record<string, string[]> {
  const out: Record<string, string[]> = {};
  for (const name of prefixed) {
    const parsed = splitPrefixed(name);
    if (!parsed) continue;
    (out[parsed.server] ??= []).push(parsed.tool);
  }
  for (const s of Object.keys(out)) out[s] = canonical(out[s]);
  return out;
}

/**
 * flattenTools renders the per-server All/Custom INTENT into the single flat,
 * prefixed tool allow-list the backend stores (ClientProfile.Tools). This is the
 * faithful mapping, and the subtle part of the whole feature:
 *
 * The backend tool axis is GLOBAL, not per-server. An empty list means "all
 * tools of all granted servers"; a NON-empty list means "ONLY these tools" —
 * every tool not listed is hidden, even on a server the client otherwise reaches
 * (see buildDraftScope, which drops a granted server whose tools are absent from
 * a non-empty allow-list). So the moment ONE granted server is narrowed, every
 * other granted "All" server must be enumerated explicitly, or it would vanish.
 *
 * Therefore:
 *  - If no granted server restricts (all are "all", or "custom" but covering
 *    their whole tool set), return [] — the backend reads that as "all".
 *  - Otherwise enumerate: each granted server contributes its custom subset, or
 *    its full tool set when it is "all".
 *
 * serverTools maps server name -> its full (unprefixed) tool universe, sourced
 * from MCPServerStatus.tools by the caller (the store does not hold it).
 */
export function flattenTools(
  grantedServers: string[],
  serverTools: Record<string, string[]>,
  toolMode: Record<string, ToolMode>,
  customSel: Record<string, string[]>,
): string[] {
  const perServer: Record<string, string[]> = {};
  let anyRestriction = false;

  for (const s of grantedServers) {
    const full = serverTools[s] ?? [];
    const mode = toolMode[s] ?? 'all';
    if (mode === 'custom') {
      // Keep only selections that still exist in the live tool universe (a tool
      // can disappear between sessions); when the universe is unknown (tests),
      // keep the selection as-is.
      const sel = (customSel[s] ?? []).filter((t) => full.length === 0 || full.includes(t));
      perServer[s] = sel;
      const restricts = full.length === 0 ? sel.length > 0 : sel.length < full.length;
      if (restricts) anyRestriction = true;
    } else {
      perServer[s] = full;
    }
  }

  if (!anyRestriction) return [];

  const out: string[] = [];
  for (const s of grantedServers) {
    for (const tool of perServer[s] ?? []) {
      out.push(`${s}${TOOL_NAME_DELIMITER}${tool}`);
    }
  }
  return canonical(out);
}

// hasEmptyCustomGrant reports whether a granted server is in "custom" mode with
// nothing selected. That is the transient state the save path must block: an
// empty custom set would hide the server's tools entirely, and (because empty =
// all at the backend) cannot be expressed as a deliberate "deny" — so we forbid
// it rather than silently widen or silently hide.
export function hasEmptyCustomGrant(
  grantedServers: string[],
  toolMode: Record<string, ToolMode>,
  customSel: Record<string, string[]>,
): boolean {
  return grantedServers.some(
    (s) => (toolMode[s] ?? 'all') === 'custom' && (customSel[s] ?? []).length === 0,
  );
}

// ToolSeed is the resolved tool-axis state derived from a client's saved
// (prefixed) allow-list and the live tool universe. seedToolState reconstructs
// the per-server All/Custom intent: a server whose saved subset covers its whole
// tool set is "all"; a strict subset is "custom".
export interface ToolSeed {
  toolMode: Record<string, ToolMode>;
  customSel: Record<string, string[]>;
  baselineTools: string[];
}

export function seedToolState(
  savedTools: string[],
  serverTools: Record<string, string[]>,
  grantedServers: string[],
): ToolSeed {
  const byServer = groupToolsByServer(savedTools);
  const toolMode: Record<string, ToolMode> = {};
  const customSel: Record<string, string[]> = {};

  for (const [s, tools] of Object.entries(byServer)) {
    customSel[s] = tools; // already canonical from groupToolsByServer
    const full = serverTools[s] ?? [];
    const coversFull =
      full.length > 0 && tools.length === full.length && tools.every((t) => full.includes(t));
    toolMode[s] = coversFull ? 'all' : 'custom';
  }

  // baselineTools is the flatten of the seeded intent, NOT the raw savedTools:
  // it is the value the dirty check compares against, so an untouched client
  // never reads as dirty even when the saved list was an "all" server enumerated
  // (or, conversely, a 1-tool server that is ambiguously "all").
  const baselineTools = flattenTools(grantedServers, serverTools, toolMode, customSel);
  return { toolMode, customSel, baselineTools };
}

/**
 * buildDraftScope derives the ClientScopeResult a client WOULD have under a
 * draft server selection plus a flat tool allow-list, faithfully replicating the
 * backend's global tool allow-list semantics so the live canvas preview matches
 * what a commit writes:
 *
 *  - The tool axis is global, not per-server. With a non-empty allow-list,
 *    reachable tools are that list intersected with the drafted servers' tools
 *    (adding a server does NOT surface its tools unless they are in the list).
 *    With an empty allow-list, every tool of a drafted server is reachable.
 *  - Reachable servers are derived from the reachable tools (a server with no
 *    visible tool contributes nothing), matching pkg/mcp.scopeResult.
 *
 * Callers pass the live tool axis (see flattenTools) so the dim/light preview
 * updates instantly as the operator narrows a server.
 */
export function buildDraftScope(
  draftServers: string[],
  servers: MCPServerStatus[],
  toolAllowList: string[],
): ClientScopeResult {
  const granted = new Set(draftServers);
  const toolAllow = toolAllowList.length ? new Set(toolAllowList) : null;
  const tools: string[] = [];
  for (const s of servers) {
    if (!granted.has(s.name)) continue;
    for (const t of s.tools ?? []) {
      const prefixed = `${s.name}${TOOL_NAME_DELIMITER}${t}`;
      if (toolAllow && !toolAllow.has(prefixed)) continue;
      tools.push(prefixed);
    }
  }
  const reachableServers = new Set<string>();
  for (const t of tools) {
    const parsed = splitPrefixed(t);
    if (parsed) reachableServers.add(parsed.server);
  }
  return {
    configured: true,
    unscoped: false,
    servers: [...reachableServers].sort(),
    tools: tools.sort(),
  };
}

// SeedParams describes the saved state the draft is initialized from when a
// client becomes the Access Lens target. serverTools is the live tool universe
// (server name -> unprefixed tool names) used to reconstruct the per-server
// All/Custom intent from the flat saved allow-list; it is optional so existing
// server-only callers (and tests) keep working.
export interface SeedParams {
  slug: string;
  name: string;
  baseline: string[];
  savedTools: string[];
  createsBlock: boolean;
  serverTools?: Record<string, string[]>;
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
  // savedTools is the client's raw saved (prefixed) tool allow-list, kept for
  // reference (e.g. the "view raw allow-list" affordance). The editable tool
  // intent lives in toolMode/customSel; the dirty baseline is baselineTools.
  savedTools: string[];
  createsBlock: boolean;

  // The live draft selection (server names). The single source the canvas nodes,
  // the slide-over checkboxes, the action bar, and the highlight all read/write.
  draft: string[];

  // Per-server tool intent for the granted servers. toolMode defaults to "all"
  // for any server not present. customSel holds each server's chosen unprefixed
  // tools; it persists across All<->Custom toggles so flipping back to Custom
  // restores the prior subset. baselineTools is the flattened saved intent the
  // dirty check compares against. toolsTouched records whether the operator
  // deliberately edited any tool group this session — it drives the tri-state
  // preserve-vs-replace decision (omit the axis when untouched).
  toolMode: Record<string, ToolMode>;
  customSel: Record<string, string[]>;
  baselineTools: string[];
  toolsTouched: boolean;
  // The live tool universe (server name -> unprefixed tool names) captured at
  // seed time. Held so discard/markSaved can reconstruct the per-server intent
  // with the real universe (needed to tell "all" from a full-coverage "custom").
  serverTools: Record<string, string[]>;

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

  // Tool-axis actions (all mark toolsTouched). server is a granted server name;
  // tool is an UNPREFIXED tool name.
  setServerToolMode: (server: string, mode: ToolMode) => void;
  toggleTool: (server: string, tool: string) => void;
  // setCustomTools replaces a server's selection outright (canonical). Used by
  // the canvas, where clicking one pill on an unrestricted server means "all
  // tools except this one" — a replace, not a per-tool toggle.
  setCustomTools: (server: string, tools: string[]) => void;
  selectAllTools: (server: string, tools: string[]) => void;
  // clearTools removes the given tools from a server's selection; with no list
  // it clears the whole server. Callers pass the currently-visible (filtered)
  // tools so a "clear all" under an active filter does not drop hidden picks.
  clearTools: (server: string, tools?: string[]) => void;

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
  toolMode: {} as Record<string, ToolMode>,
  customSel: {} as Record<string, string[]>,
  baselineTools: [] as string[],
  toolsTouched: false,
  serverTools: {} as Record<string, string[]>,
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

  seed: ({ slug, name, baseline, savedTools, createsBlock, serverTools }) => {
    const grantedServers = canonical(baseline);
    const universe = serverTools ?? {};
    const { toolMode, customSel, baselineTools } = seedToolState(
      savedTools,
      universe,
      grantedServers,
    );
    set({
      clientSlug: slug,
      clientName: name,
      baseline: grantedServers,
      savedTools: canonical(savedTools),
      createsBlock,
      draft: grantedServers,
      toolMode,
      customSel,
      baselineTools,
      toolsTouched: false,
      serverTools: universe,
      conflict: null,
    });
  },

  toggleServer: (name) => {
    const next = new Set(get().draft);
    if (next.has(name)) next.delete(name);
    else next.add(name);
    set({ draft: [...next] });
  },
  setDraft: (names) => set({ draft: [...names] }),
  selectAll: (allServerNames) => set({ draft: canonical(allServerNames) }),
  clearAll: () => set({ draft: [] }),

  setServerToolMode: (server, mode) =>
    set((s) => ({
      toolMode: { ...s.toolMode, [server]: mode },
      toolsTouched: true,
    })),

  toggleTool: (server, tool) =>
    set((s) => {
      const current = new Set(s.customSel[server] ?? []);
      if (current.has(tool)) current.delete(tool);
      else current.add(tool);
      return {
        customSel: { ...s.customSel, [server]: canonical([...current]) },
        toolsTouched: true,
      };
    }),

  setCustomTools: (server, tools) =>
    set((s) => ({
      customSel: { ...s.customSel, [server]: canonical(tools) },
      toolsTouched: true,
    })),

  selectAllTools: (server, tools) =>
    set((s) => {
      const merged = new Set(s.customSel[server] ?? []);
      for (const t of tools) merged.add(t);
      return {
        customSel: { ...s.customSel, [server]: canonical([...merged]) },
        toolsTouched: true,
      };
    }),

  clearTools: (server, tools) =>
    set((s) => {
      if (!tools) {
        return { customSel: { ...s.customSel, [server]: [] }, toolsTouched: true };
      }
      const remove = new Set(tools);
      return {
        customSel: {
          ...s.customSel,
          [server]: (s.customSel[server] ?? []).filter((t) => !remove.has(t)),
        },
        toolsTouched: true,
      };
    }),

  markSaved: () =>
    set((s) => ({
      baseline: canonical(s.draft),
      // Advance the tool baseline to the just-saved intent. A poll refresh then
      // reseeds from the persisted scope; until then this keeps the draft clean.
      baselineTools: flattenTools(s.draft, s.serverTools, s.toolMode, s.customSel),
      toolsTouched: false,
      conflict: null,
    })),
  discardDraft: () =>
    set((s) => {
      const { toolMode, customSel, baselineTools } = seedToolState(
        s.savedTools,
        s.serverTools,
        canonical(s.baseline),
      );
      return {
        draft: canonical(s.baseline),
        toolMode,
        customSel,
        baselineTools,
        toolsTouched: false,
        conflict: null,
      };
    }),
  clearDraft: () => set({ ...EMPTY_TARGET, slideOverOpen: false }),

  setConflict: (conflict) => set({ conflict }),
  setSaving: (isSaving) => set({ isSaving }),

  requestExitNav: (path) => set({ exitNavTarget: path }),
  clearExitNav: () => set({ exitNavTarget: null }),

  requestSwitch: (slug) => set({ pendingSwitchSlug: slug }),
  clearSwitch: () => set({ pendingSwitchSlug: null }),
}));
