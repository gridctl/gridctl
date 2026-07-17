import { useEffect, useMemo, useRef, useState } from 'react';
import { useStackStore } from '../stores/useStackStore';
import { showToast } from '../components/ui/Toast';
import {
  AuthError,
  ClientScopeError,
  fetchClients,
  fetchStatus,
  updateClientScope,
} from '../lib/api';
import type { ClientStatus } from '../types';

function canonical(names: string[]): string[] {
  return Array.from(new Set(names)).sort();
}

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
): UseClientScopeEditor {
  const saved = useMemo(
    () => baselineServers(client, allServerNames),
    [client, allServerNames],
  );
  const createsBlock = !client?.effectiveScope?.configured;

  const [selection, setSelection] = useState<string[]>(saved);
  const [isSaving, setIsSaving] = useState(false);
  const [conflict, setConflict] = useState<string | null>(null);

  // Re-seed the draft when the selected client (or its saved baseline) changes,
  // keyed on the client slug + the canonical baseline signature so a polling
  // refresh that doesn't change membership leaves an in-progress edit alone.
  const slug = client?.slug ?? '';
  const savedSignature = saved.join(' ');
  const seededRef = useRef('');
  useEffect(() => {
    const key = `${slug} ${savedSignature}`;
    if (seededRef.current !== key) {
      seededRef.current = key;
      setSelection(saved);
      setConflict(null);
    }
  }, [slug, savedSignature, saved]);

  const selected = useMemo(() => new Set(selection), [selection]);
  const dirty = !arraysEqual(canonical(selection), saved);
  const canSave = dirty && selection.length > 0;

  const toggle = (server: string) => {
    const next = new Set(selected);
    if (next.has(server)) next.delete(server);
    else next.add(server);
    setSelection([...next]);
  };
  const selectAll = () => setSelection(canonical(allServerNames));
  const clearAll = () => setSelection([]);
  const reset = () => {
    setSelection(saved);
    setConflict(null);
  };

  const save = async () => {
    if (!client || selection.length === 0) return;
    setIsSaving(true);
    setConflict(null);
    try {
      // Server-level scope: write the selected servers; omit tools so an
      // operator-authored tool allow-list on this profile is preserved.
      const resp = await updateClientScope(client.slug, {
        servers: canonical(selection),
      });
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
  };
}
