import { create } from 'zustand';

interface AuthState {
  authRequired: boolean;
  isAuthenticated: boolean;

  setAuthRequired: (required: boolean) => void;
  setAuthenticated: (authenticated: boolean) => void;
}

export const useAuthStore = create<AuthState>()((set) => ({
  authRequired: false,
  isAuthenticated: false,

  setAuthRequired: (authRequired) => set({ authRequired }),
  setAuthenticated: (isAuthenticated) => set({ isAuthenticated, authRequired: false }),
}));
