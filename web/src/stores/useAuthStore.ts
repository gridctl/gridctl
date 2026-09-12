import { create } from 'zustand';
import { persistCredential, readCredential, type GatewayCredential } from '../lib/credentials';

interface AuthState {
  credential: GatewayCredential | null;
  generation: symbol;
  attempt: symbol | null;
  notice: string | null;
  beginAttempt: () => symbol;
  commitAttempt: (attempt: symbol, credential: GatewayCredential) => boolean;
  rejectGeneration: (generation: symbol) => void;
  storageChanged: () => void;
  authRequired: boolean;
  isAuthenticated: boolean;

  setAuthRequired: (required: boolean) => void;
  setAuthenticated: (authenticated: boolean) => void;
}

const saved = readCredential();
export const useAuthStore = create<AuthState>()((set, get) => ({
  credential: saved.credential,
  notice: saved.notice,
  generation: Symbol(),
  attempt: null,
  beginAttempt: () => {
    const attempt = Symbol();
    set({ attempt });
    return attempt;
  },
  commitAttempt: (attempt, credential) => {
    if (get().attempt !== attempt) return false;
    const notice = persistCredential(credential);
    set({ credential: { ...credential }, generation: Symbol(), attempt: null, notice, authRequired: false, isAuthenticated: true });
    return true;
  },
  rejectGeneration: (generation) => {
    if (get().generation === generation) set({ authRequired: true, isAuthenticated: false, generation: Symbol() });
  },
  storageChanged: () => {
    const stored = readCredential();
    if (!get().credential && !stored.credential) {
      set({ generation: Symbol(), attempt: null, notice: stored.notice });
      return;
    }
    set({ authRequired: true, isAuthenticated: false, generation: Symbol(), attempt: null, notice: 'Saved credentials changed in another window. Verify before resuming.' });
  },
  authRequired: false,
  isAuthenticated: false,

  setAuthRequired: (authRequired) => set({ authRequired }),
  setAuthenticated: (isAuthenticated) => set({ isAuthenticated, authRequired: false }),
}));
