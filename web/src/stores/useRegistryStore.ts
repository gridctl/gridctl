import { create } from 'zustand';
import { subscribeWithSelector } from 'zustand/middleware';
import type { AgentSkill, AgentProjectionStatus, RegistryAgent, RegistryStatus, SkillSourceStatus } from '../types';

interface RegistryState {
  // Data
  skills: AgentSkill[] | null;
  status: RegistryStatus | null;
  // Configured skill sources (provenance). null = not loaded yet.
  sources: SkillSourceStatus[] | null;
  // Imported agent definitions and their per-client projection rows.
  // Parallel slices beside `skills` (not a kind-keyed refactor): too many
  // call sites read `s.skills` directly. null = not loaded yet.
  agents: RegistryAgent[] | null;
  agentStatuses: AgentProjectionStatus[] | null;

  // Loading state
  isLoading: boolean;
  error: string | null;

  // Actions
  setSkills: (skills: AgentSkill[]) => void;
  setStatus: (status: RegistryStatus) => void;
  setSources: (sources: SkillSourceStatus[]) => void;
  setAgents: (agents: RegistryAgent[]) => void;
  setAgentStatuses: (statuses: AgentProjectionStatus[]) => void;
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
    sources: null,
    agents: null,
    agentStatuses: null,
    isLoading: false,
    error: null,

    setSkills: (skills) => set({ skills: skills ?? [] }),
    setStatus: (status) => set({ status }),
    setSources: (sources) => set({ sources: sources ?? [] }),
    setAgents: (agents) => set({ agents: agents ?? [] }),
    setAgentStatuses: (statuses) => set({ agentStatuses: statuses ?? [] }),
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
