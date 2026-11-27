import { create } from 'zustand';
import { subscribeWithSelector } from 'zustand/middleware';
import type {
  SpecHealth,
  ValidationResult,
  PlanDiff,
  StackSpec,
} from '../types';

interface SpecState {
  // Spec content (live from disk, updated by polling)
  spec: StackSpec | null;
  specLoading: boolean;
  specError: string | null;

  // Validation. validationError is a failed validation request; the last
  // result stays in place.
  validation: ValidationResult | null;
  validationError: string | null;

  // Health
  health: SpecHealth | null;

  // Plan diff (stack file vs the configuration the gateway loaded), as
  // returned by GET /api/stack/plan for the open diff modal.
  plan: PlanDiff | null;
  planLoading: boolean;
  planError: unknown;

  // Compare mode
  compareActive: boolean;

  // Diff modal
  diffModalOpen: boolean;
  diffModalMode: 'apply' | 'compare';

  // Actions
  setSpec: (spec: StackSpec) => void;
  setSpecLoading: (loading: boolean) => void;
  setSpecError: (error: string | null) => void;
  setValidation: (result: ValidationResult) => void;
  setValidationError: (error: string | null) => void;
  setHealth: (health: SpecHealth) => void;
  setPlan: (plan: PlanDiff) => void;
  setPlanLoading: () => void;
  setPlanError: (error: unknown) => void;
  toggleCompare: () => void;
  setCompareActive: (active: boolean) => void;
  openApplyPlan: (plan: PlanDiff) => void;
  openApplyError: (error: unknown) => void;
  openCompareModal: () => void;
  closeDiffModal: () => void;
}

export const useSpecStore = create<SpecState>()(
  subscribeWithSelector((set) => ({
    spec: null,
    specLoading: false,
    specError: null,
    validation: null,
    validationError: null,
    health: null,
    plan: null,
    planLoading: false,
    planError: null,
    compareActive: false,
    diffModalOpen: false,
    diffModalMode: 'apply',

    setSpec: (spec) => set({ spec, specError: null }),
    setSpecLoading: (specLoading) => set({ specLoading }),
    setSpecError: (specError) => set({ specError }),
    setValidation: (validation) => set({ validation, validationError: null }),
    setValidationError: (validationError) => set({ validationError }),
    setHealth: (health) => set({ health }),
    setPlan: (plan) => set({ plan, planLoading: false, planError: null }),
    setPlanLoading: () => set({ plan: null, planLoading: true, planError: null }),
    setPlanError: (planError) => set({ plan: null, planLoading: false, planError }),
    toggleCompare: () => set((s) => ({ compareActive: !s.compareActive })),
    setCompareActive: (compareActive) => set({ compareActive }),
    openApplyPlan: (plan) => set({
      diffModalOpen: true, diffModalMode: 'apply', plan, planLoading: false, planError: null,
    }),
    openApplyError: (planError) => set({
      diffModalOpen: true, diffModalMode: 'apply', plan: null, planLoading: false, planError,
    }),
    openCompareModal: () => set({
      diffModalOpen: true, diffModalMode: 'compare', plan: null, planLoading: true, planError: null,
    }),
    closeDiffModal: () => set({
      diffModalOpen: false, diffModalMode: 'apply', plan: null, planLoading: false, planError: null,
    }),
  }))
);

// Selectors
export const useSpecHealth = () => useSpecStore((s) => s.health);
export const useSpecValidation = () => useSpecStore((s) => s.validation);
