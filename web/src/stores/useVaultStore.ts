import { create } from 'zustand';
import type { VaultSecret, VaultSet } from '../lib/api';

interface VaultState {
  secrets: VaultSecret[] | null;
  sets: VaultSet[] | null;
  loading: boolean;
  error: string | null;
  locked: boolean;
  encrypted: boolean;

  setSecrets: (secrets: VaultSecret[]) => void;
  setSets: (sets: VaultSet[]) => void;
  setLoading: (loading: boolean) => void;
  setError: (error: string | null) => void;
  setLocked: (locked: boolean) => void;
  setEncrypted: (encrypted: boolean) => void;
}

export const useVaultStore = create<VaultState>()((set) => ({
  secrets: null,
  sets: null,
  loading: false,
  error: null,
  locked: false,
  encrypted: false,

  setSecrets: (secrets) => set({ secrets: secrets ?? [] }),
  setSets: (sets) => set({ sets: sets ?? [] }),
  setLoading: (loading) => set({ loading }),
  setError: (error) => set({ error }),
  setLocked: (locked) => set(locked ? { locked, secrets: null, sets: null } : { locked }),
  setEncrypted: (encrypted) => set({ encrypted }),
}));
