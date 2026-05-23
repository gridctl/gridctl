import { create } from 'zustand';
import type { Consumer, Variable, VariableSet } from '../lib/api';

// useVaultStore is kept as the in-memory cache for the unified variable
// store (secrets + plaintext config). The hook name retains "Vault" for
// historic reasons; the API surface and on-disk schema use the unified
// "Variable" vocabulary.
interface VaultState {
  variables: Variable[] | null;
  sets: VariableSet[] | null;
  // usage maps each variable key to the consumers that reference it in the
  // active stack (the "used by" index). Best-effort: stays {} when no stack is
  // loaded or the usage fetch fails, so the variable list never depends on it.
  usage: Record<string, Consumer[]>;
  loading: boolean;
  error: string | null;
  locked: boolean;
  encrypted: boolean;

  setVariables: (variables: Variable[]) => void;
  setSets: (sets: VariableSet[]) => void;
  setUsage: (usage: Record<string, Consumer[]>) => void;
  setLoading: (loading: boolean) => void;
  setError: (error: string | null) => void;
  setLocked: (locked: boolean) => void;
  setEncrypted: (encrypted: boolean) => void;
}

export const useVaultStore = create<VaultState>()((set) => ({
  variables: null,
  sets: null,
  usage: {},
  loading: false,
  error: null,
  locked: false,
  encrypted: false,

  setVariables: (variables) => set({ variables: variables ?? [] }),
  setSets: (sets) => set({ sets: sets ?? [] }),
  setUsage: (usage) => set({ usage: usage ?? {} }),
  setLoading: (loading) => set({ loading }),
  setError: (error) => set({ error }),
  setLocked: (locked) =>
    set(locked ? { locked, variables: null, sets: null, usage: {} } : { locked }),
  setEncrypted: (encrypted) => set({ encrypted }),
}));
