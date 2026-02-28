import { create } from 'zustand';
import { subscribeWithSelector } from 'zustand/middleware';
import type { WorkflowDefinition, ExecutionResult } from '../types';
import { fetchWorkflowDefinition, executeWorkflow as executeWorkflowApi, validateWorkflow as validateWorkflowApi } from '../lib/api';

type StepStatus = 'pending' | 'running' | 'success' | 'failed' | 'skipped';

interface ValidationResult {
  valid: boolean;
  errors: string[];
  warnings: string[];
  resolvedArgs?: Record<string, Record<string, unknown>>;
}

interface WorkflowState {
  // Workflow data
  definition: WorkflowDefinition | null;
  loading: boolean;
  error: string | null;

  // Execution state
  execution: ExecutionResult | null;
  executing: boolean;
  stepStatuses: Record<string, StepStatus>;

  // Validation
  validation: ValidationResult | null;
  validating: boolean;

  // UI state
  selectedStepId: string | null;
  followMode: boolean;
  skillName: string | null;

  // Actions
  loadWorkflow: (skillName: string) => Promise<void>;
  executeWorkflow: (skillName: string, args: Record<string, unknown>) => Promise<void>;
  validateWorkflowInputs: (skillName: string, args: Record<string, unknown>) => Promise<void>;
  setSelectedStep: (stepId: string | null) => void;
  toggleFollowMode: () => void;
  reset: () => void;
}

export const useWorkflowStore = create<WorkflowState>()(
  subscribeWithSelector((set, get) => ({
    definition: null,
    loading: false,
    error: null,
    execution: null,
    executing: false,
    stepStatuses: {},
    validation: null,
    validating: false,
    selectedStepId: null,
    followMode: true,
    skillName: null,

    loadWorkflow: async (skillName: string) => {
      set({ loading: true, error: null, skillName });
      try {
        const definition = await fetchWorkflowDefinition(skillName);
        // Initialize step statuses to pending
        const stepStatuses: Record<string, StepStatus> = {};
        for (const level of definition.dag?.levels ?? []) {
          for (const step of level ?? []) {
            stepStatuses[step.id] = 'pending';
          }
        }
        set({ definition, loading: false, stepStatuses, execution: null, validation: null });
      } catch (err) {
        set({
          error: err instanceof Error ? err.message : 'Failed to load workflow',
          loading: false,
        });
      }
    },

    executeWorkflow: async (skillName: string, args: Record<string, unknown>) => {
      const { definition } = get();
      if (!definition) return;

      // Set all steps to pending, then mark first level as running
      const stepStatuses: Record<string, StepStatus> = {};
      for (const level of definition.dag?.levels ?? []) {
        for (const step of level ?? []) {
          stepStatuses[step.id] = 'pending';
        }
      }
      // Mark first level as running
      for (const step of (definition.dag?.levels ?? [])[0] ?? []) {
        stepStatuses[step.id] = 'running';
      }

      set({ executing: true, execution: null, stepStatuses, validation: null });

      try {
        const result = await executeWorkflowApi(skillName, args);
        // Update step statuses from result
        const finalStatuses: Record<string, StepStatus> = { ...stepStatuses };
        for (const step of result.steps ?? []) {
          finalStatuses[step.id] = step.status as StepStatus;
        }
        set({ execution: result, executing: false, stepStatuses: finalStatuses });
      } catch (err) {
        set({
          error: err instanceof Error ? err.message : 'Execution failed',
          executing: false,
        });
      }
    },

    validateWorkflowInputs: async (skillName: string, args: Record<string, unknown>) => {
      set({ validating: true, validation: null });
      try {
        const result = await validateWorkflowApi(skillName, args);
        set({ validation: result, validating: false });
      } catch (err) {
        set({
          validation: {
            valid: false,
            errors: [err instanceof Error ? err.message : 'Validation failed'],
            warnings: [],
          },
          validating: false,
        });
      }
    },

    setSelectedStep: (stepId) => set({ selectedStepId: stepId }),
    toggleFollowMode: () => set((s) => ({ followMode: !s.followMode })),
    reset: () =>
      set({
        definition: null,
        loading: false,
        error: null,
        execution: null,
        executing: false,
        stepStatuses: {},
        validation: null,
        validating: false,
        selectedStepId: null,
        skillName: null,
      }),
  }))
);
