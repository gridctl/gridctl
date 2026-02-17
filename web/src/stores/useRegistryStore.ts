import { create } from 'zustand';
import { subscribeWithSelector } from 'zustand/middleware';
import type { AgentSkill, RegistryStatus } from '../types';

interface RegistryState {
  // Data
  skills: AgentSkill[] | null;
  status: RegistryStatus | null;

  // Loading state
  isLoading: boolean;
  error: string | null;

  // Actions
  setSkills: (skills: AgentSkill[]) => void;
  setStatus: (status: RegistryStatus) => void;
  setLoading: (loading: boolean) => void;
  setError: (error: string | null) => void;

  // Computed helpers
  hasContent: () => boolean;
  activeSkillCount: () => number;
}

export const useRegistryStore = create<RegistryState>()(
  subscribeWithSelector((set, get) => ({
    skills: null,
    status: null,
    isLoading: false,
    error: null,

    setSkills: (skills) => set({ skills: skills ?? [] }),
    setStatus: (status) => set({ status }),
    setLoading: (isLoading) => set({ isLoading }),
    setError: (error) => set({ error }),

    hasContent: () => {
      const { skills } = get();
      return (skills ?? []).length > 0;
    },
    activeSkillCount: () => {
      return (get().skills ?? []).filter((s) => s.state === 'active').length;
    },
  }))
);
