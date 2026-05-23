import { useCallback } from 'react';
import { useVaultStore } from '../stores/useVaultStore';
import {
  fetchVariables,
  fetchVariableSets,
  fetchVariableUsage,
  createVariable,
  getVariable,
  updateVariable,
  deleteVariable,
  createVariableSet,
  deleteVariableSet,
  assignVariableToSet,
  fetchVariableStoreStatus,
  unlockVariableStore,
  lockVariableStore,
  importVariables,
} from '../lib/api';
import type {
  Consumer,
  CreateVariableInput,
  ImportVariableInput,
  UpdateVariableInput,
  Variable,
  VariableSet,
} from '../lib/api';

export interface UseVaultManagerOptions {
  // Invoked after refresh() pre-fetches plaintext (non-secret) variable
  // values, so the consumer can hydrate its local revealed-values map and
  // render plaintext rows with content on first paint.
  onPlaintextLoaded?: (map: Record<string, string>) => void;
}

export interface UseVaultManagerResult {
  // Store state — re-rendered when useVaultStore changes
  variables: Variable[] | null;
  sets: VariableSet[] | null;
  usage: Record<string, Consumer[]>;
  // recentlyEdited maps variable key → epoch-ms of its last session mutation;
  // consumers derive the per-set "recently edited" dot from it.
  recentlyEdited: Record<string, number>;
  loading: boolean;
  error: string | null;
  locked: boolean;
  encrypted: boolean;

  // Actions
  refresh: () => Promise<void>;
  unlock: (passphrase: string) => Promise<boolean>;
  lock: (passphrase: string) => Promise<void>;
  createVar: (input: CreateVariableInput) => Promise<void>;
  updateVar: (key: string, input: UpdateVariableInput) => Promise<void>;
  deleteVar: (key: string) => Promise<void>;
  markRecentlyEdited: (keys: string[]) => void;
  getVar: (key: string) => Promise<{ value: string }>;
  createSet: (name: string) => Promise<void>;
  deleteSet: (name: string) => Promise<void>;
  assignToSet: (key: string, set: string) => Promise<void>;
  importVars: (vars: ImportVariableInput[]) => Promise<{ imported: number }>;
}

// Single source of truth for vault data + IO actions, consumed by both the
// sidebar (VaultPanel) and the detached `/var` page. Reads from useVaultStore;
// every action that mutates server state calls refresh() to re-sync the
// store. Per-row reveal state and form state stay local to consumers — this
// hook deliberately owns no UI state.
export function useVaultManager(
  options?: UseVaultManagerOptions,
): UseVaultManagerResult {
  const variables = useVaultStore((s) => s.variables);
  const sets = useVaultStore((s) => s.sets);
  const usage = useVaultStore((s) => s.usage);
  const recentlyEdited = useVaultStore((s) => s.recentlyEdited);
  const markRecentlyEdited = useVaultStore((s) => s.markRecentlyEdited);
  const loading = useVaultStore((s) => s.loading);
  const error = useVaultStore((s) => s.error);
  const locked = useVaultStore((s) => s.locked);
  const encrypted = useVaultStore((s) => s.encrypted);

  const onPlaintextLoaded = options?.onPlaintextLoaded;

  const refresh = useCallback(async () => {
    useVaultStore.getState().setLoading(true);
    useVaultStore.getState().setError(null);
    try {
      const status = await fetchVariableStoreStatus();
      useVaultStore.getState().setLocked(status.locked);
      useVaultStore.getState().setEncrypted(status.encrypted);

      if (!status.locked) {
        // Usage is best-effort and parallel: a failure must not break the
        // variable list, so it resolves to {} rather than rejecting.
        const [variablesData, setsData, usageData] = await Promise.all([
          fetchVariables(),
          fetchVariableSets(),
          fetchVariableUsage().catch(() => ({}) as Record<string, Consumer[]>),
        ]);
        useVaultStore.getState().setVariables(variablesData);
        useVaultStore.getState().setSets(setsData);
        useVaultStore.getState().setUsage(usageData);

        // Plaintext variables display their value inline by default
        // (no Reveal click needed). Eager-fetch them so the rows render
        // with values on first paint.
        const plaintext = variablesData.filter((v) => !v.is_secret);
        if (plaintext.length > 0 && onPlaintextLoaded) {
          const fetched = await Promise.all(
            plaintext.map((v) =>
              getVariable(v.key).then(
                (detail) => [v.key, detail.value] as const,
                () => [v.key, ''] as const,
              ),
            ),
          );
          const map: Record<string, string> = {};
          for (const [k, val] of fetched) map[k] = val;
          onPlaintextLoaded(map);
        }
      }
    } catch (err) {
      useVaultStore
        .getState()
        .setError(err instanceof Error ? err.message : 'Failed to load vault');
    } finally {
      useVaultStore.getState().setLoading(false);
    }
  }, [onPlaintextLoaded]);

  const unlock = useCallback(
    async (passphrase: string): Promise<boolean> => {
      try {
        await unlockVariableStore(passphrase);
        useVaultStore.getState().setLocked(false);
        await refresh();
        return true;
      } catch {
        return false;
      }
    },
    [refresh],
  );

  const lock = useCallback(
    async (passphrase: string): Promise<void> => {
      await lockVariableStore(passphrase);
      await refresh();
    },
    [refresh],
  );

  const createVar = useCallback(
    async (input: CreateVariableInput) => {
      await createVariable(input);
      await refresh();
      useVaultStore.getState().markRecentlyEdited([input.key]);
    },
    [refresh],
  );

  const updateVar = useCallback(
    async (key: string, input: UpdateVariableInput) => {
      await updateVariable(key, input);
      await refresh();
      useVaultStore.getState().markRecentlyEdited([key]);
    },
    [refresh],
  );

  const deleteVar = useCallback(
    async (key: string) => {
      await deleteVariable(key);
      await refresh();
    },
    [refresh],
  );

  const getVar = useCallback(async (key: string) => {
    return getVariable(key);
  }, []);

  const createSet = useCallback(
    async (name: string) => {
      await createVariableSet(name);
      await refresh();
    },
    [refresh],
  );

  const deleteSet = useCallback(
    async (name: string) => {
      await deleteVariableSet(name);
      await refresh();
    },
    [refresh],
  );

  const assignToSet = useCallback(
    async (key: string, set: string) => {
      await assignVariableToSet(key, set);
      await refresh();
      // After refresh the variable's `.set` is the destination, so the dot
      // surfaces on the set it moved into (not the one it left).
      useVaultStore.getState().markRecentlyEdited([key]);
    },
    [refresh],
  );

  const importVars = useCallback(
    async (vars: ImportVariableInput[]) => {
      const result = await importVariables(vars);
      await refresh();
      useVaultStore.getState().markRecentlyEdited(vars.map((v) => v.key));
      return result;
    },
    [refresh],
  );

  return {
    variables,
    sets,
    usage,
    recentlyEdited,
    loading,
    error,
    locked,
    encrypted,
    refresh,
    unlock,
    lock,
    createVar,
    updateVar,
    deleteVar,
    markRecentlyEdited,
    getVar,
    createSet,
    deleteSet,
    assignToSet,
    importVars,
  };
}
