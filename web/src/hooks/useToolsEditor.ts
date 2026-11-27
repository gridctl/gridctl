import { useCallback, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { useStackStore } from '../stores/useStackStore';
import { TOOL_NAME_DELIMITER } from '../lib/constants';
import { showToast } from '../components/ui/Toast';
import { describeError, isCancellation } from '../lib/describeError';
import {
  AuthError,
  fetchStatus,
  fetchTools,
  fetchToolCatalog,
  setServerTools,
  SetServerToolsError,
} from '../lib/api';
import { useFuzzySearch } from './useFuzzySearch';
import type { ToolAnnotations } from '../types';

export interface ToolRow {
  name: string;
  description?: string;
  // Server-reported MCP annotations from the catalog (undefined when the
  // tool declares none or the catalog has no entry for it).
  annotations?: ToolAnnotations;
}

export function formatHiddenToolReference(
  issues: { field: string; message: string }[] | undefined,
  hint?: string,
): string {
  const parts = (issues ?? []).map((issue) => {
    const group = issue.field.match(/^groups\[([^\]]+)\]/)?.[1] ?? issue.field;
    const tool = issue.message.match(/tool '([^']+)'/)?.[1];
    return tool ? `groups ${group} (${tool})` : `groups ${group}`;
  });
  const lead = parts.length
    ? `Hidden by this change: ${parts.join(', ')}`
    : 'Hidden by this change';
  return hint ? `${lead}. ${hint}` : lead;
}

// canonicalWhitelist normalizes a selection into the wire form: a sorted,
// deduplicated array. Dirty comparison uses it on both sides so selection
// order never triggers a spurious "unsaved changes" state.
function canonicalWhitelist(names: string[]): string[] {
  return Array.from(new Set(names)).sort();
}

function arraysEqual(a: string[], b: string[]): boolean {
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) if (a[i] !== b[i]) return false;
  return true;
}

// toWireWhitelist converts a selection into the body for PUT tools. An empty
// whitelist means "expose all tools" in stack YAML, so the selection collapses
// to [] only when it contains every tool the server has; matching counts with
// different names is still a curated whitelist.
export function toWireWhitelist(selection: string[], allToolNames: string[]): string[] {
  if (allToolNames.length === 0) return selection;
  const chosen = new Set(selection);
  return allToolNames.every((name) => chosen.has(name)) ? [] : selection;
}

export interface UseToolsEditorOptions {
  // Called by handleKeepEditing to move the consumer's selection back to the
  // draft's owner. Defaults to selecting the owner's node in the Stack store,
  // which is only correct for the main-window Stack inspector.
  onRestoreServer?: (name: string) => void;
}

export interface UseToolsEditor {
  // Every tool the editor should render (server-advertised + saved + described).
  allTools: ToolRow[];
  // allTools narrowed by the current search query.
  visible: ToolRow[];
  query: string;
  setQuery: (q: string) => void;
  // Current selection as a Set for cheap membership/size lookups in the view.
  selected: Set<string>;
  toggle: (name: string) => void;
  selectAll: () => void;
  clearAll: () => void;
  // True when the selection differs from what's persisted in the stack YAML.
  // Meaningless while locked: it compares against the incoming server.
  dirty: boolean;
  // True when the draft selection is empty while the server advertises tools.
  // Saving that state is refused: an empty whitelist means "expose all", so
  // persisting it would invert "disable everything" into "expose everything".
  // Consumers should disable Save and switch the count line to danger copy.
  saveBlocked: boolean;
  // Count of added + removed tools relative to the saved whitelist.
  diffCount: number;
  isSaving: boolean;
  // Non-null when a 409 (stack file changed on disk) needs a reload affordance.
  conflict: string | null;
  // Non-null when unchecking a tool would hide a group reference.
  hiddenReference: string | null;
  // The server the draft selection belongs to. Differs from serverName only
  // while locked.
  owner: string;
  // True while the draft belongs to a server other than serverName. A locked
  // editor refuses every save and selection change until the consumer returns
  // to the owner or the draft is discarded.
  locked: boolean;
  // Non-null (the owner's name) while locked with unsaved edits; the user must
  // discard or return to the owner. Stays up until the editor unlocks.
  discardPrompt: string | null;
  handleSave: () => Promise<void>;
  // Remove the named tools from the saved whitelist and persist only that,
  // through the same save+reload path as handleSave. Used by Audit Mode's
  // "disable unused" remediation. The names also leave the draft; every other
  // draft edit stays unsaved. Refused (no change, no request) when it would
  // empty the saved whitelist; no request when none of the names are saved.
  // A save that does not persist restores the draft as it was before the
  // call. Respects expose-all semantics: persists the remaining tools as a
  // concrete whitelist (or [] if the remaining set is every tool).
  disableTools: (names: string[]) => Promise<void>;
  handleReloadFromDisk: () => Promise<void>;
  handleDiscard: () => void;
  // Asks the consumer to select the owner again. The prompt clears only when
  // serverName is the owner, so a restore that does not happen cannot unlock.
  handleKeepEditing: () => void;
}

// The draft and the server it belongs to live in one state object so a late
// update (an in-flight save's revert) can check ownership in a functional
// update instead of trusting values captured before the await.
interface Draft {
  owner: string;
  // The owner's canonical saved selection. The incoming savedSelection
  // belongs to serverName, which differs from the owner while locked.
  ownerSaved: string[];
  selection: string[];
}

// useToolsEditor owns the per-server tool whitelist editing controller: the
// tool list derivation, selection + dirty tracking, the polling/server-switch
// guard, and the save-and-reload flow with its structured error handling. The
// sidebar ToolsEditor and the fleet Tools workspace both drive their UI from
// this hook so the two never diverge.
//
// `savedTools` is the whitelist applied to the server in the live stack. An
// empty array means "no whitelist" (every tool the gateway knows is exposed).
// `serverTools` is the unprefixed list of tools this specific server advertises
// (from /api/status); it is authoritative because code mode hides downstream
// tools behind meta-tools in the global aggregated list.
export function useToolsEditor(
  serverName: string,
  savedTools: string[],
  serverTools?: string[],
  options?: UseToolsEditorOptions,
): UseToolsEditor {
  const catalog = useStackStore((s) => s.toolCatalog);
  const onRestoreServer = options?.onRestoreServer;

  // Every tool the editor should render. The server's own advertised list
  // (from /api/status) is the authoritative source; it holds every tool
  // regardless of code mode. The tool catalog (/api/tools/catalog) provides
  // descriptions when available, including in code mode. A saved whitelist
  // entry missing from both still renders so the user sees what's actually
  // persisted in the YAML.
  const allTools: ToolRow[] = useMemo(() => {
    const prefix = `${serverName}${TOOL_NAME_DELIMITER}`;
    const catalogInfo = new Map<
      string,
      { description?: string; annotations?: ToolAnnotations }
    >();
    for (const t of catalog ?? []) {
      if (t.name.startsWith(prefix)) {
        catalogInfo.set(t.name.slice(prefix.length), {
          description: t.description,
          annotations: t.annotations,
        });
      }
    }
    const rows = new Map<string, ToolRow>();
    for (const name of serverTools ?? []) {
      const info = catalogInfo.get(name);
      rows.set(name, { name, description: info?.description, annotations: info?.annotations });
    }
    for (const [name, info] of catalogInfo) {
      if (!rows.has(name)) {
        rows.set(name, { name, description: info.description, annotations: info.annotations });
      }
    }
    for (const name of savedTools) {
      if (!rows.has(name)) rows.set(name, { name });
    }
    return [...rows.values()];
  }, [catalog, serverName, savedTools, serverTools]);

  // Saved selection in canonical form. When savedTools is empty, the YAML
  // has no whitelist, so the server is exposing every tool; we reflect that
  // by checking every row.
  const savedSelection = useMemo(() => {
    if (savedTools.length === 0) {
      return canonicalWhitelist(allTools.map((t) => t.name));
    }
    return canonicalWhitelist(savedTools);
  }, [savedTools, allTools]);

  // Tool names are constrained to [a-zA-Z0-9_-] (Claude Desktop tool-name
  // rules), so a NUL byte can never appear inside one; it is a collision-free
  // join delimiter for the membership signature the sync below keys on.
  const savedSignature = savedSelection.join(String.fromCharCode(0));

  const [draft, setDraft] = useState<Draft>({
    owner: serverName,
    ownerSaved: savedSelection,
    selection: savedSelection,
  });
  const selection = draft.selection;
  const setSelection = useCallback(
    (next: string[] | ((prev: string[]) => string[])) =>
      setDraft((cur) => ({
        ...cur,
        selection: typeof next === 'function' ? next(cur.selection) : next,
      })),
    [],
  );
  const [synced, setSynced] = useState({ server: serverName, signature: savedSignature });
  const [query, setQuery] = useState('');
  const [isSaving, setIsSaving] = useState(false);
  const [conflict, setConflict] = useState<string | null>(null);
  const [hiddenReference, setHiddenReference] = useState<string | null>(null);

  // Reconcile the draft when the server or its saved membership changes. Done
  // during render (React's "adjust state when a prop changes" pattern) so a
  // clean switch adopts the new server before anything commits. Keyed on
  // (serverName, savedSignature): polling that reshuffles the tools array
  // without changing membership must not reset state.
  if (synced.server !== serverName || synced.signature !== savedSignature) {
    setSynced({ server: serverName, signature: savedSignature });
    if (synced.server !== serverName) setHiddenReference(null);
    const draftDirty = !arraysEqual(canonicalWhitelist(selection), draft.ownerSaved);
    if (draft.owner === serverName) {
      // Same owner: a refreshed baseline (poll, save, or a return from a
      // locked switch). Pending edits survive; a clean draft follows disk.
      setDraft({
        owner: serverName,
        ownerSaved: savedSelection,
        selection: draftDirty ? selection : savedSelection,
      });
    } else if (!draftDirty) {
      setDraft({ owner: serverName, ownerSaved: savedSelection, selection: savedSelection });
    }
    // Otherwise the draft stays with its owner and the editor stays locked.
  }

  const owner = draft.owner;
  const locked = owner !== serverName;

  // The server the consumer shows as of the last commit. An in-flight save
  // reads it after the await to tell whether the editor has moved on.
  const currentServerRef = useRef(serverName);
  useLayoutEffect(() => {
    currentServerRef.current = serverName;
  }, [serverName]);

  const visible = useFuzzySearch(allTools, query);

  const selected = useMemo(() => new Set(selection), [selection]);
  const canonicalSelection = useMemo(() => canonicalWhitelist(selection), [selection]);
  const dirty = !arraysEqual(canonicalSelection, savedSelection);
  const saveBlocked = canonicalSelection.length === 0 && allTools.length > 0;
  const diffCount = useMemo(() => {
    const saved = new Set(savedSelection);
    let count = 0;
    for (const name of canonicalSelection) if (!saved.has(name)) count++;
    for (const name of savedSelection) if (!selected.has(name)) count++;
    return count;
  }, [canonicalSelection, savedSelection, selected]);
  const discardPrompt =
    locked && !arraysEqual(canonicalSelection, draft.ownerSaved) ? owner : null;

  // Functional update so this identity does not follow the selection. A
  // dependency on selected would change every toggle and defeat row memo.
  const toggle = useCallback(
    (name: string) => {
      if (locked) return;
      setSelection((prev) => {
        const next = new Set(prev);
        if (next.has(name)) next.delete(name);
        else next.add(name);
        return [...next];
      });
    },
    [locked, setSelection],
  );

  const selectAll = () => {
    if (locked) return;
    setSelection(allTools.map((t) => t.name));
  };
  const clearAll = () => {
    if (locked) return;
    setSelection([]);
  };

  // saveSelection persists a canonical selection through the atomic
  // save+reload path and refreshes the global caches. Shared by handleSave
  // (current selection) and disableTools (selection minus the unused tools)
  // so both honor the same expose-all semantics and structured-error handling.
  // Resolves true when the write persisted (including reload_failed, where the
  // file was written but the hot reload was not).
  const saveSelection = async (targetSelection: string[]): Promise<boolean> => {
    // A locked draft belongs to another server. Refusing here, at the shared
    // choke point, makes a write to the wrong server impossible from any
    // caller, whatever the view renders.
    if (locked) return false;
    // Refuse an empty selection outright: the wire form can't express "expose
    // nothing" (an empty whitelist means "expose all"), so saving it would
    // silently re-expose every tool, the inverse of the user's intent. A
    // server with no discovered tools is exempt; its only valid save is [].
    // disableTools keeps its own earlier check: it must refuse before
    // setSelection blanks the draft, and its copy names the remediation flow.
    if (targetSelection.length === 0 && allTools.length > 0) {
      showToast(
        'error',
        `Can't save an empty selection for ${serverName} — an empty whitelist would re-expose every tool. Keep at least one tool enabled or discard.`,
      );
      return false;
    }
    setIsSaving(true);
    setConflict(null);
    setHiddenReference(null);
    const wire = toWireWhitelist(
      targetSelection,
      allTools.map((t) => t.name),
    );
    const savedFor = serverName;
    try {
      const resp = await setServerTools(serverName, wire);
      showToast('success', `Tools saved for ${serverName}`);
      if (resp.reloaded === false) {
        showToast(
          'warning',
          'Stack updated. Run "gridctl reload" or restart with --watch to apply.',
        );
      }
      // Refresh the global caches so the sidebar reflects the now-filtered
      // tool set. Best-effort; we've already persisted the write.
      try {
        const [status, toolsList, catalogList] = await Promise.all([
          fetchStatus(),
          fetchTools(),
          fetchToolCatalog(),
        ]);
        useStackStore.getState().setGatewayStatus(status);
        // Coalesce to []: stackless responses carry null tool lists (nil Go
        // slice) that would crash list/search consumers downstream.
        useStackStore.getState().setTools(toolsList.tools ?? []);
        useStackStore.getState().setToolCatalog(catalogList.tools ?? []);
      } catch {
        /* ignore refresh failures — the page will re-poll shortly */
      }
      return true;
    } catch (err) {
      if (err instanceof AuthError) {
        throw err;
      }
      if (err instanceof SetServerToolsError) {
        switch (err.code) {
          case 'stack_modified':
            setConflict(err.hint || err.message);
            return false;
          case 'reload_failed':
            showToast(
              'error',
              `Tools saved for ${serverName}, but reload failed: ${err.message}. Check gridctl logs.`,
            );
            // The save persisted. Refetch so the editor shows the new
            // on-disk state as the clean baseline; the hot reload can be
            // re-attempted via the Reload button elsewhere in the UI.
            try {
              const [status, toolsList, catalogList] = await Promise.all([
                fetchStatus(),
                fetchTools(),
                fetchToolCatalog(),
              ]);
              useStackStore.getState().setGatewayStatus(status);
              useStackStore.getState().setTools(toolsList.tools);
              useStackStore.getState().setToolCatalog(catalogList.tools);
            } catch {
              /* ignore refresh failures */
            }
            return true;
          case 'unknown_tool':
            if (!isCancellation(err)) {
              const described = describeError(err, 'Save');
              showToast('error', described.text, { detail: described.fullDetail });
            }
            return false;
          case 'hidden_tool_reference': {
            const notice = formatHiddenToolReference(err.issues, err.hint);
            showToast('error', notice);
            // The editor may have moved to another server while the request
            // was in flight. Revert and show the notice only when it still
            // shows the server this save was for, and revert to that owner's
            // current baseline, never a value captured before the await.
            if (currentServerRef.current !== savedFor) return false;
            setDraft((cur) =>
              cur.owner === savedFor ? { ...cur, selection: cur.ownerSaved } : cur,
            );
            setHiddenReference(notice);
            return false;
          }
          default:
            if (!isCancellation(err)) {
              const described = describeError(err, 'Save');
              showToast('error', described.text, { detail: described.fullDetail });
            }
            return false;
        }
      }
      if (!isCancellation(err)) {
        const described = describeError(err, 'Save');
        showToast('error', described.text, { detail: described.fullDetail });
      }
      return false;
    } finally {
      setIsSaving(false);
    }
  };

  const handleSave = async () => {
    await saveSelection(canonicalSelection);
  };

  const disableTools = async (names: string[]) => {
    if (locked) return;
    const drop = new Set(names);
    const savedRemaining = savedSelection.filter((name) => !drop.has(name));
    // The whitelist model can't express "expose nothing": an empty whitelist
    // means "expose all". Disabling every exposed tool would therefore
    // paradoxically re-expose them (and any previously-disabled tools). Refuse
    // that case rather than silently inverting the user's intent.
    if (savedRemaining.length === 0) {
      showToast(
        'error',
        `Can't disable every exposed tool on ${serverName} — at least one must stay enabled.`,
      );
      return;
    }
    // Captured before the optimistic update: a failed save restores exactly
    // this, never React state read after the await.
    const before = draft.selection;
    const savedFor = serverName;
    setSelection((prev) => prev.filter((name) => !drop.has(name)));
    // Names enabled only in the draft are not on disk; nothing to write.
    if (savedRemaining.length === savedSelection.length) return;
    const restore = () =>
      setDraft((cur) => (cur.owner === savedFor ? { ...cur, selection: before } : cur));
    let persisted = false;
    try {
      persisted = await saveSelection(savedRemaining);
    } catch (err) {
      restore();
      throw err;
    }
    if (!persisted) restore();
  };

  // handleReloadFromDisk refreshes the saved state after a 409. We refetch
  // gateway status (which carries the running whitelist) and let the consumer
  // re-render with the new savedTools; the user's in-flight selection is kept
  // on top of the refreshed state.
  const handleReloadFromDisk = async () => {
    setConflict(null);
    setHiddenReference(null);
    try {
      const [status, toolsList, catalogList] = await Promise.all([
        fetchStatus(),
        fetchTools(),
        fetchToolCatalog(),
      ]);
      useStackStore.getState().setGatewayStatus(status);
      useStackStore.getState().setTools(toolsList.tools);
      useStackStore.getState().setToolCatalog(catalogList.tools);
    } catch (err) {
      if (!isCancellation(err)) {
        const described = describeError(err, 'Refresh');
        showToast('error', described.text, { detail: described.fullDetail });
      }
    }
  };

  const handleDiscard = () => {
    // Drop the in-flight edit and adopt serverName's saved state. Serves the
    // locked prompt, the in-place Discard button, and the leave confirms.
    setDraft({ owner: serverName, ownerSaved: savedSelection, selection: savedSelection });
  };

  const handleKeepEditing = () => {
    if (!locked) return;
    // Request the restore and nothing else: the prompt stays up until the
    // consumer re-renders with the owner as serverName.
    if (onRestoreServer) onRestoreServer(owner);
    else useStackStore.getState().selectNode(`mcp-${owner}`);
  };

  return {
    allTools,
    visible,
    query,
    setQuery,
    selected,
    toggle,
    selectAll,
    clearAll,
    dirty,
    saveBlocked,
    diffCount,
    isSaving,
    conflict,
    hiddenReference,
    owner,
    locked,
    discardPrompt,
    handleSave,
    disableTools,
    handleReloadFromDisk,
    handleDiscard,
    handleKeepEditing,
  };
}
