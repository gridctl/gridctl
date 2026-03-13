import { create } from 'zustand';
import { subscribeWithSelector } from 'zustand/middleware';
import type {
  SpecHealth,
  ValidationResult,
  PlanDiff,
  StackSpec,
} from '../types';

interface SpecState {
  // Spec content
  spec: StackSpec | null;
  specLoading: boolean;
  specError: string | null;

  // Validation
  validation: ValidationResult | null;

  // Health
  health: SpecHealth | null;

  // Plan diff (spec vs running)
  plan: PlanDiff | null;

  // Compare mode
  compareActive: boolean;

  // Diff modal
  diffModalOpen: boolean;
  pendingSpec: string | null;

  // Actions
  setSpec: (spec: StackSpec) => void;
  setSpecLoading: (loading: boolean) => void;
  setSpecError: (error: string | null) => void;
  setValidation: (result: ValidationResult) => void;
  setHealth: (health: SpecHealth) => void;
  setPlan: (plan: PlanDiff) => void;
  toggleCompare: () => void;
  setCompareActive: (active: boolean) => void;
  openDiffModal: (pendingSpec: string) => void;
  closeDiffModal: () => void;
}

export const useSpecStore = create<SpecState>()(
  subscribeWithSelector((set) => ({
    spec: null,
    specLoading: false,
    specError: null,
    validation: null,
    health: null,
    plan: null,
    compareActive: false,
    diffModalOpen: false,
    pendingSpec: null,

    setSpec: (spec) => set({ spec, specError: null }),
    setSpecLoading: (specLoading) => set({ specLoading }),
    setSpecError: (specError) => set({ specError }),
    setValidation: (validation) => set({ validation }),
    setHealth: (health) => set({ health }),
    setPlan: (plan) => set({ plan }),
    toggleCompare: () => set((s) => ({ compareActive: !s.compareActive })),
    setCompareActive: (compareActive) => set({ compareActive }),
    openDiffModal: (pendingSpec) => set({ diffModalOpen: true, pendingSpec }),
    closeDiffModal: () => set({ diffModalOpen: false, pendingSpec: null }),
  }))
);

// Selectors
export const useSpecHealth = () => useSpecStore((s) => s.health);
export const useSpecValidation = () => useSpecStore((s) => s.validation);
