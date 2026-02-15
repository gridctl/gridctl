import { create } from 'zustand';
import { subscribeWithSelector } from 'zustand/middleware';
import type { Prompt, Skill, RegistryStatus } from '../types';

interface RegistryState {
  // Data
  prompts: Prompt[];
  skills: Skill[];
  status: RegistryStatus | null;

  // UI state
  isLoading: boolean;
  error: string | null;

  // Actions
  setPrompts: (prompts: Prompt[]) => void;
  setSkills: (skills: Skill[]) => void;
  setStatus: (status: RegistryStatus) => void;
  setLoading: (loading: boolean) => void;
  setError: (error: string | null) => void;

  // Computed helpers
  hasContent: () => boolean;
  activePromptCount: () => number;
  activeSkillCount: () => number;
}

export const useRegistryStore = create<RegistryState>()(
  subscribeWithSelector((set, get) => ({
    // Initial state
    prompts: [],
    skills: [],
    status: null,
    isLoading: false,
    error: null,

    // Actions
    setPrompts: (prompts) => set({ prompts: prompts ?? [] }),
    setSkills: (skills) => set({ skills: skills ?? [] }),
    setStatus: (status) => set({ status }),
    setLoading: (isLoading) => set({ isLoading }),
    setError: (error) => set({ error }),

    // Computed
    hasContent: () => {
      const { prompts, skills } = get();
      return (prompts ?? []).length > 0 || (skills ?? []).length > 0;
    },
    activePromptCount: () => {
      return (get().prompts ?? []).filter((p) => p.state === 'active').length;
    },
    activeSkillCount: () => {
      return (get().skills ?? []).filter((s) => s.state === 'active').length;
    },
  }))
);
