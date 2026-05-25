import { useEffect, useMemo, useRef, useState } from 'react';
import { useStackStore } from '../stores/useStackStore';
import { TOOL_NAME_DELIMITER } from '../lib/constants';
import { showToast } from '../components/ui/Toast';
import {
  AuthError,
  fetchStatus,
  fetchTools,
  fetchToolCatalog,
  setServerTools,
  SetServerToolsError,
} from '../lib/api';
import { useFuzzySearch } from './useFuzzySearch';

export interface ToolRow {
  name: string;
  description?: string;
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
  dirty: boolean;
  // Count of added + removed tools relative to the saved whitelist.
  diffCount: number;
  isSaving: boolean;
  // Non-null when a 409 (stack file changed on disk) needs a reload affordance.
  conflict: string | null;
  // Non-null (the previous server name) when the user switched servers with a
  // dirty editor and must confirm before the edit is discarded.
  discardPrompt: string | null;
  handleSave: () => Promise<void>;
  // Deselect the named tools and save the result through the same save+reload
  // path as handleSave. Used by Audit Mode's "disable unused" remediation.
  // Respects expose-all semantics: persists the remaining tools as a concrete
  // whitelist (or [] if the remaining set is every tool).
  disableTools: (names: string[]) => Promise<void>;
  handleReloadFromDisk: () => Promise<void>;
  handleDiscard: () => void;
  handleKeepEditing: () => void;
}

// useToolsEditor owns the per-server tool whitelist editing controller: the
// tool list derivation, selection + dirty tracking, the polling/server-switch
// guard, and the save-and-reload flow with its structured error handling. The
// sidebar ToolsEditor and the fleet Tools workspace both drive their UI from
// this hook so the two never diverge.
//
// `savedTools` is the whitelist applied to the server in the live stack — an
// empty array means "no whitelist" (every tool the gateway knows is exposed).
// `serverTools` is the unprefixed list of tools this specific server advertises
// (from /api/status); it is authoritative because code mode hides downstream
// tools behind meta-tools in the global aggregated list.
export function useToolsEditor(
  serverName: string,
  savedTools: string[],
  serverTools?: string[],
): UseToolsEditor {
  const catalog = useStackStore((s) => s.toolCatalog);

  // Every tool the editor should render. The server's own advertised list
  // (from /api/status) is the authoritative source — it holds every tool
  // regardless of code mode. The tool catalog (/api/tools/catalog) provides
  // descriptions when available, including in code mode. A saved whitelist
  // entry missing from both still renders so the user sees what's actually
  // persisted in the YAML.
  const allTools: ToolRow[] = useMemo(() => {
    const prefix = `${serverName}${TOOL_NAME_DELIMITER}`;
    const descriptions = new Map<string, string | undefined>();
    for (const t of catalog ?? []) {
      if (t.name.startsWith(prefix)) {
        descriptions.set(t.name.slice(prefix.length), t.description);
      }
    }
    const rows = new Map<string, ToolRow>();
    for (const name of serverTools ?? []) {
      rows.set(name, { name, description: descriptions.get(name) });
    }
    for (const [name, description] of descriptions) {
      if (!rows.has(name)) rows.set(name, { name, description });
    }
    for (const name of savedTools) {
      if (!rows.has(name)) rows.set(name, { name });
    }
    return [...rows.values()];
  }, [catalog, serverName, savedTools, serverTools]);

  // Saved selection in canonical form. When savedTools is empty, the YAML
  // has no whitelist, so the server is exposing every tool — we reflect that
  // by checking every row.
  const savedSelection = useMemo(() => {
    if (savedTools.length === 0) {
      return canonicalWhitelist(allTools.map((t) => t.name));
    }
    return canonicalWhitelist(savedTools);
  }, [savedTools, allTools]);

  const [selection, setSelection] = useState<string[]>(savedSelection);
  const [query, setQuery] = useState('');
  const [isSaving, setIsSaving] = useState(false);
  const [conflict, setConflict] = useState<string | null>(null);
  // When the user tries to switch servers with unsaved edits, we stash the name
  // of the server we were editing so the "Keep editing" affordance can re-
  // select it in the graph store.
  const [discardPrompt, setDiscardPrompt] = useState<string | null>(null);

  // Reset local selection when the server switches or when the saved state
  // changes and the user has no pending edits. The ref captures the
  // most-recent committed serverName so polling-driven re-renders don't
  // clobber the user's in-progress edits.
  const committedServer = useRef(serverName);
  const savedRef = useRef(savedSelection);
  const selectionRef = useRef(selection);
  // Mirror the latest selection into a ref so the server-switch guard can read
  // it without depending on `selection` (which would re-run the guard on every
  // toggle). Done in an effect to keep ref writes out of render.
  useEffect(() => {
    selectionRef.current = selection;
  }, [selection]);

  // Tool names are constrained to [a-zA-Z0-9_-] (Claude Desktop tool-name
  // rules), so a NUL byte can never appear inside one — it is a collision-free
  // join delimiter for the membership signature this effect depends on.
  const savedSignature = savedSelection.join(String.fromCharCode(0));

  useEffect(() => {
    const prevServer = committedServer.current;
    const prevSaved = savedRef.current;
    const currentCanonical = canonicalWhitelist(selectionRef.current);
    const isDirty = !arraysEqual(currentCanonical, prevSaved);

    if (prevServer !== serverName && isDirty) {
      // Server switched out from under a dirty editor. Freeze the incoming
      // render and surface a confirm dialog — until the user decides we keep
      // the previous selection in view.
      setDiscardPrompt(prevServer);
      return;
    }

    committedServer.current = serverName;
    savedRef.current = savedSelection;
    if (!isDirty || prevServer !== serverName) {
      setSelection(savedSelection);
    }
    // Intentionally depend on the tuple of (serverName, canonicalized
    // savedSelection signature) — polling that reshuffles the underlying tools
    // array without changing membership must not reset state.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [serverName, savedSignature]);

  const visible = useFuzzySearch(allTools, query);

  const selected = useMemo(() => new Set(selection), [selection]);
  const canonicalSelection = useMemo(() => canonicalWhitelist(selection), [selection]);
  const dirty = !arraysEqual(canonicalSelection, savedSelection);
  const diffCount = useMemo(() => {
    const saved = new Set(savedSelection);
    let count = 0;
    for (const name of canonicalSelection) if (!saved.has(name)) count++;
    for (const name of savedSelection) if (!selected.has(name)) count++;
    return count;
  }, [canonicalSelection, savedSelection, selected]);

  const toggle = (name: string) => {
    const next = new Set(selected);
    if (next.has(name)) next.delete(name);
    else next.add(name);
    setSelection([...next]);
  };

  const selectAll = () => setSelection(allTools.map((t) => t.name));
  const clearAll = () => setSelection([]);

  // saveSelection persists a canonical selection through the atomic
  // save+reload path and refreshes the global caches. Shared by handleSave
  // (current selection) and disableTools (selection minus the unused tools)
  // so both honor the same expose-all semantics and structured-error handling.
  const saveSelection = async (targetSelection: string[]) => {
    setIsSaving(true);
    setConflict(null);
    // Empty whitelist means "expose all tools" in stack YAML semantics. We
    // send an empty array when the selection covers every known tool so
    // the stack file stays clean of a redundant full-list whitelist.
    const wire =
      targetSelection.length === allTools.length && allTools.length > 0 ? [] : targetSelection;
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
        useStackStore.getState().setTools(toolsList.tools);
        useStackStore.getState().setToolCatalog(catalogList.tools);
      } catch {
        /* ignore refresh failures — the page will re-poll shortly */
      }
    } catch (err) {
      if (err instanceof AuthError) {
        throw err;
      }
      if (err instanceof SetServerToolsError) {
        switch (err.code) {
          case 'stack_modified':
            setConflict(err.hint || err.message);
            return;
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
            return;
          case 'unknown_tool':
            showToast('error', err.message);
            return;
          default:
            showToast('error', err.message);
            return;
        }
      }
      const msg = err instanceof Error ? err.message : 'Save failed';
      showToast('error', msg);
    } finally {
      setIsSaving(false);
    }
  };

  const handleSave = () => saveSelection(canonicalSelection);

  const disableTools = (names: string[]) => {
    const drop = new Set(names);
    const remaining = canonicalSelection.filter((name) => !drop.has(name));
    // The whitelist model can't express "expose nothing" — an empty whitelist
    // means "expose all". Disabling every exposed tool would therefore
    // paradoxically re-expose them (and any previously-disabled tools). Refuse
    // that case rather than silently inverting the user's intent.
    if (remaining.length === 0) {
      showToast(
        'error',
        `Can't disable every exposed tool on ${serverName} — at least one must stay enabled.`,
      );
      return Promise.resolve();
    }
    // Reflect the change in the view, then persist it. saveSelection reads the
    // computed `remaining` directly (not React state) so the save is correct
    // even though setSelection has not flushed yet.
    setSelection(remaining);
    return saveSelection(remaining);
  };

  // handleReloadFromDisk refreshes the saved state after a 409. We refetch
  // gateway status (which carries the running whitelist) and let the consumer
  // re-render with the new savedTools; the user's in-flight selection is kept
  // on top of the refreshed state.
  const handleReloadFromDisk = async () => {
    setConflict(null);
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
      const msg = err instanceof Error ? err.message : 'Refresh failed';
      showToast('error', msg);
    }
  };

  const handleDiscard = () => {
    // User accepted the server switch. Adopt the incoming shape by
    // re-initialising selection to the new savedSelection.
    committedServer.current = serverName;
    savedRef.current = savedSelection;
    setSelection(savedSelection);
    setDiscardPrompt(null);
  };

  const handleKeepEditing = () => {
    if (!discardPrompt) return;
    // Revert the graph's node selection back to the server we were editing.
    // The consumer re-renders with the previous server's data and our local
    // selection is still the in-flight edit.
    useStackStore.getState().selectNode(`mcp-${discardPrompt}`);
    setDiscardPrompt(null);
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
    diffCount,
    isSaving,
    conflict,
    discardPrompt,
    handleSave,
    disableTools,
    handleReloadFromDisk,
    handleDiscard,
    handleKeepEditing,
  };
}
