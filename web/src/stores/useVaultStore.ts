import { create } from 'zustand';
import type { Variable, VariableSet } from '../lib/api';

// useVaultStore is kept as the in-memory cache for the unified variable
// store (secrets + plaintext config). The hook name retains "Vault" for
// historic reasons; the API surface and on-disk schema use the unified
// "Variable" vocabulary.
interface VaultState {
  variables: Variable[] | null;
  sets: VariableSet[] | null;
  loading: boolean;
  error: string | null;
  locked: boolean;
  encrypted: boolean;

  setVariables: (variables: Variable[]) => void;
  setSets: (sets: VariableSet[]) => void;
  setLoading: (loading: boolean) => void;
  setError: (error: string | null) => void;
  setLocked: (locked: boolean) => void;
  setEncrypted: (encrypted: boolean) => void;
}

export const useVaultStore = create<VaultState>()((set) => ({
  variables: null,
  sets: null,
  loading: false,
  error: null,
  locked: false,
  encrypted: false,

  setVariables: (variables) => set({ variables: variables ?? [] }),
  setSets: (sets) => set({ sets: sets ?? [] }),
  setLoading: (loading) => set({ loading }),
  setError: (error) => set({ error }),
  setLocked: (locked) => set(locked ? { locked, variables: null, sets: null } : { locked }),
  setEncrypted: (encrypted) => set({ encrypted }),
}));
