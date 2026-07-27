import { create } from 'zustand';
import type { Consumer, DriftEntry, Variable, VariableSet } from '../lib/api';

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
  // usageLoaded distinguishes "usage fetched (possibly empty)" from "usage
  // unknown" (fetch failed or not attempted). An unknown index must not be
  // presented as "nothing is referenced".
  usageLoaded: boolean;
  // drift lists stack references to variables that do not exist. Like usage
  // it is best-effort and stays empty when unknown, so a failed fetch shows
  // no warning rather than a false one.
  drift: DriftEntry[];
  // recentlyEdited maps a variable key to the epoch-ms it was last mutated in
  // this session (create/update/import/reassign). It powers the per-set
  // "recently edited" dot and is in-memory only — never persisted and cleared
  // on lock, so it's a "since you last looked" hint, not durable metadata.
  recentlyEdited: Record<string, number>;
  loading: boolean;
  error: string | null;
  locked: boolean;
  encrypted: boolean;

  setVariables: (variables: Variable[]) => void;
  setSets: (sets: VariableSet[]) => void;
  setUsage: (usage: Record<string, Consumer[]>, loaded?: boolean) => void;
  setDrift: (drift: DriftEntry[]) => void;
  markRecentlyEdited: (keys: string[]) => void;
  clearRecentlyEdited: () => void;
  setLoading: (loading: boolean) => void;
  setError: (error: string | null) => void;
  setLocked: (locked: boolean) => void;
  setEncrypted: (encrypted: boolean) => void;
}

export const useVaultStore = create<VaultState>()((set) => ({
  variables: null,
  sets: null,
  usage: {},
  usageLoaded: false,
  drift: [],
  recentlyEdited: {},
  loading: false,
  error: null,
  locked: false,
  encrypted: false,

  setVariables: (variables) => set({ variables: variables ?? [] }),
  setSets: (sets) => set({ sets: sets ?? [] }),
  setUsage: (usage, loaded = true) =>
    set({ usage: usage ?? {}, usageLoaded: loaded }),
  setDrift: (drift) => set({ drift: drift ?? [] }),
  markRecentlyEdited: (keys) =>
    set((state) => {
      if (keys.length === 0) return state;
      const now = Date.now();
      const next = { ...state.recentlyEdited };
      for (const key of keys) next[key] = now;
      return { recentlyEdited: next };
    }),
  clearRecentlyEdited: () => set({ recentlyEdited: {} }),
  setLoading: (loading) => set({ loading }),
  setError: (error) => set({ error }),
  setLocked: (locked) =>
    set(
      locked
        ? {
            locked,
            variables: null,
            sets: null,
            usage: {},
            usageLoaded: false,
            drift: [],
            recentlyEdited: {},
          }
        : { locked },
    ),
  setEncrypted: (encrypted) => set({ encrypted }),
}));
