import { useEffect, useMemo, useRef, useState } from 'react';
import { useStackStore } from '../stores/useStackStore';
import { showToast } from '../components/ui/Toast';
import {
  AuthError,
  ClientScopeError,
  fetchClients,
  fetchStatus,
  updateClientScope,
  type ClientScopeUpdate,
} from '../lib/api';
import {
  canonical,
  flattenTools,
  hasEmptyCustomGrant,
  isDirty as listDirty,
  seedToolState,
  type ToolMode,
} from '../stores/useAccessLensStore';
import type { ClientStatus } from '../types';

function arraysEqual(a: string[], b: string[]): boolean {
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) if (a[i] !== b[i]) return false;
  return true;
}

/**
 * Derive the server set a client currently reaches, used as the editor's saved
 * baseline. An unscoped client (no clients block, default-allow, or an empty
 * profile) reaches every server, so the baseline is all server names; a scoped
 * client's baseline is its effective servers (which is [] when it reaches
 * nothing under default-deny).
 */
export function baselineServers(
  client: ClientStatus | null,
  allServerNames: string[],
): string[] {
  const scope = client?.effectiveScope;
  if (!scope || !scope.configured || scope.unscoped) {
    return canonical(allServerNames);
  }
  return canonical(scope.servers);
}

export interface UseClientScopeEditor {
  selected: Set<string>;
  toggle: (server: string) => void;
  selectAll: () => void;
  clearAll: () => void;
  dirty: boolean;
  /** Save is allowed only with at least one server selected: an empty servers
   *  list means "all servers" in the config model, so it can't express "deny",
   *  and committing zero would silently grant everything. */
  canSave: boolean;
  isSaving: boolean;
  conflict: string | null;
  /** True when no clients: block exists yet, so saving creates one and flips
   *  unlisted clients to the default (deny) policy (a stack-wide consequence). */
  createsBlock: boolean;
  save: () => Promise<void>;
  reset: () => void;

  // ---- Tool axis (per-server All/Custom intent over the granted servers) ----
  // The state model is the Access Lens one (flattenTools/seedToolState), so the
  // modal editor and the Stack lens can never disagree about what a save means.
  toolMode: Record<string, ToolMode>;
  customSel: Record<string, string[]>;
  /** True once the operator edited any tool group this session. An untouched
   *  (or touched-but-reverted) axis is omitted from the save payload so an
   *  operator-authored allow-list in stack.yaml is preserved verbatim. */
  toolsTouched: boolean;
  /** A granted server sits in Custom mode with nothing selected, a state the
   *  backend cannot express (empty = all), so save is blocked. */
  emptyCustomGrant: boolean;
  /** A tool-axis edit is pending while a granted server has not reported its
   *  tool universe yet (still initializing). Flattening would enumerate zero
   *  tools for it and silently hide the whole server once it comes up, so
   *  save is blocked until the universe is known. */
  toolAxisBlocked: boolean;
  setServerToolMode: (server: string, mode: ToolMode) => void;
  toggleTool: (server: string, tool: string) => void;
  selectAllTools: (server: string, tools: string[]) => void;
  clearTools: (server: string, tools: string[]) => void;
}

/**
 * useClientScopeEditor owns the per-client server-access editing controller: it
 * tracks the draft set of servers a client may reach, dirty state against the
 * client's current effective scope, and the save-and-reload flow (mirroring
 * useToolsEditor's structured-error handling). Saving writes a server-level
 * profile (servers allow-list, tools unrestricted) to the stack `clients:`
 * block via updateClientScope, then refreshes the client list so the Stack view
 * reflects the new scope.
 */
export function useClientScopeEditor(
  client: ClientStatus | null,
  allServerNames: string[],
  // Live tool universe (server name -> unprefixed tool names) for the tool
  // axis. Optional: server-only callers and existing tests omit it, which
  // leaves the tool axis inert (nothing to enumerate, never touched). Callers
  // must pass the whitelist-FILTERED set (effectiveEnabledTools semantics):
  // scope validation checks submitted names against the filtered catalog, so
  // enumerating whitelist-disabled tools would make every save 422.
  serverTools?: Record<string, string[]>,
  // Names of servers still initializing (no reported tool universe yet).
  // A tool-axis save is blocked while one of these is granted.
  pendingInitServers?: string[],
): UseClientScopeEditor {
  const saved = useMemo(
    () => baselineServers(client, allServerNames),
    [client, allServerNames],
  );
  const createsBlock = !client?.effectiveScope?.configured;

  const [selection, setSelection] = useState<string[]>(saved);
  const [isSaving, setIsSaving] = useState(false);
  const [conflict, setConflict] = useState<string | null>(null);

  // Tool axis: per-server All/Custom intent seeded from the client's saved
  // (prefixed) allow-list, exactly like the Access Lens store.
  const universe = useMemo(() => serverTools ?? {}, [serverTools]);
  const savedTools = useMemo(
    () => canonical(client?.effectiveScope?.tools ?? []),
    [client],
  );
  const [toolMode, setToolMode] = useState<Record<string, ToolMode>>({});
  const [customSel, setCustomSel] = useState<Record<string, string[]>>({});
  const [baselineTools, setBaselineTools] = useState<string[]>([]);
  const [toolsTouched, setToolsTouched] = useState(false);

  // True once the operator has edited anything this session (servers or
  // tools). Distinct from `dirty`: an external baseline change makes a clean
  // editor read as dirty, and only operator edits must survive a reseed.
  const touchedRef = useRef(false);

  // Re-seed the draft when the selected client (or its saved baseline)
  // changes, keyed on the client slug + the canonical baseline signatures so
  // a polling refresh that doesn't change membership leaves state alone. An
  // in-progress edit is never clobbered: while touched, a signature change
  // (a server finishing init, a whitelist edit elsewhere) skips the reseed;
  // the operator's save then resolves against the fresh state server-side
  // (409/422), and discard re-adopts it.
  const slug = client?.slug ?? '';
  const savedSignature = `${saved.join(' ')}|${savedTools.join(' ')}`;
  const seededRef = useRef('');
  useEffect(() => {
    const key = `${slug} ${savedSignature}`;
    if (seededRef.current !== key && !touchedRef.current) {
      seededRef.current = key;
      setSelection(saved);
      setConflict(null);
      const seeded = seedToolState(savedTools, universe, saved);
      setToolMode(seeded.toolMode);
      setCustomSel(seeded.customSel);
      setBaselineTools(seeded.baselineTools);
      setToolsTouched(false);
    }
  }, [slug, savedSignature, saved, savedTools, universe]);

  const selected = useMemo(() => new Set(selection), [selection]);
  const grantedServers = useMemo(() => canonical(selection), [selection]);
  const flatTools = useMemo(
    () => flattenTools(grantedServers, universe, toolMode, customSel),
    [grantedServers, universe, toolMode, customSel],
  );
  const emptyCustomGrant = hasEmptyCustomGrant(grantedServers, toolMode, customSel);
  const toolsDirty = toolsTouched && listDirty(flatTools, baselineTools);
  // Flattening enumerates every granted server's tools; a server with no
  // reported universe would contribute zero entries and vanish for this
  // client the moment it initializes. Block the axis until every granted
  // server's universe is known.
  const pendingInit = useMemo(() => new Set(pendingInitServers ?? []), [pendingInitServers]);
  const toolAxisBlocked =
    toolsDirty &&
    grantedServers.some((s) => pendingInit.has(s) || (universe[s] ?? []).length === 0);
  const dirty = !arraysEqual(canonical(selection), saved) || toolsDirty;
  const canSave = dirty && selection.length > 0 && !emptyCustomGrant && !toolAxisBlocked;

  const toggle = (server: string) => {
    touchedRef.current = true;
    const next = new Set(selected);
    if (next.has(server)) next.delete(server);
    else next.add(server);
    setSelection([...next]);
  };
  const selectAll = () => {
    touchedRef.current = true;
    setSelection(canonical(allServerNames));
  };
  const clearAll = () => {
    touchedRef.current = true;
    setSelection([]);
  };
  const reset = () => {
    // Adopt the current on-disk state (which may have moved while the edit
    // was in flight) and re-arm the reseed effect for future refreshes.
    touchedRef.current = false;
    seededRef.current = `${slug} ${savedSignature}`;
    setSelection(saved);
    setConflict(null);
    const seeded = seedToolState(savedTools, universe, saved);
    setToolMode(seeded.toolMode);
    setCustomSel(seeded.customSel);
    setBaselineTools(seeded.baselineTools);
    setToolsTouched(false);
  };

  const setServerToolMode = (server: string, mode: ToolMode) => {
    touchedRef.current = true;
    setToolMode((prev) => ({ ...prev, [server]: mode }));
    setToolsTouched(true);
  };
  const toggleTool = (server: string, tool: string) => {
    touchedRef.current = true;
    setCustomSel((prev) => {
      const current = new Set(prev[server] ?? []);
      if (current.has(tool)) current.delete(tool);
      else current.add(tool);
      return { ...prev, [server]: canonical([...current]) };
    });
    setToolsTouched(true);
  };
  const selectAllTools = (server: string, tools: string[]) => {
    touchedRef.current = true;
    setCustomSel((prev) => {
      const merged = new Set(prev[server] ?? []);
      for (const t of tools) merged.add(t);
      return { ...prev, [server]: canonical([...merged]) };
    });
    setToolsTouched(true);
  };
  const clearTools = (server: string, tools: string[]) => {
    touchedRef.current = true;
    setCustomSel((prev) => {
      const remove = new Set(tools);
      return { ...prev, [server]: (prev[server] ?? []).filter((t) => !remove.has(t)) };
    });
    setToolsTouched(true);
  };

  const save = async () => {
    if (!client || selection.length === 0 || emptyCustomGrant || toolAxisBlocked) return;
    setIsSaving(true);
    setConflict(null);
    try {
      // Server axis always writes the selection. The tool axis is tri-state:
      // clean (untouched or reverted) -> omitted, so an operator-authored
      // stack.yaml allow-list is preserved verbatim; dirty -> the flattened
      // intent replaces it ([] = all tools of the granted servers).
      const update: ClientScopeUpdate = { servers: canonical(selection) };
      if (toolsDirty) update.tools = flatTools;
      const resp = await updateClientScope(client.slug, update);
      // The refresh below carries the new baseline; let it reseed cleanly.
      touchedRef.current = false;
      showToast('success', `Access saved for ${client.name}`);
      if (resp.reloaded === false) {
        showToast('warning', 'Stack updated. Run "gridctl reload" or restart with --watch to apply.');
      }
      // Refresh clients (carries the recomputed effectiveScope) and gateway
      // status so the Stack view and editor reflect the new scope.
      try {
        const [clients, status] = await Promise.all([fetchClients(), fetchStatus()]);
        useStackStore.getState().setClients(clients);
        useStackStore.getState().setGatewayStatus(status);
      } catch {
        /* ignore refresh failures; polling will catch up */
      }
    } catch (err) {
      if (err instanceof AuthError) throw err;
      if (err instanceof ClientScopeError) {
        if (err.code === 'stack_modified') {
          setConflict(err.hint || err.message);
          return;
        }
        if (err.code === 'reload_failed') {
          showToast('error', `Access saved for ${client.name}, but reload failed: ${err.message}.`);
          return;
        }
        showToast('error', err.message);
        return;
      }
      showToast('error', err instanceof Error ? err.message : 'Save failed');
    } finally {
      setIsSaving(false);
    }
  };

  return {
    selected,
    toggle,
    selectAll,
    clearAll,
    dirty,
    canSave,
    isSaving,
    conflict,
    createsBlock,
    save,
    reset,
    toolMode,
    customSel,
    toolsTouched,
    emptyCustomGrant,
    toolAxisBlocked,
    setServerToolMode,
    toggleTool,
    selectAllTools,
    clearTools,
  };
}
