import { create } from 'zustand';
import { subscribeWithSelector } from 'zustand/middleware';
import type { ResourceType, MCPServerFormData, ResourceFormData, StackFormData } from '../lib/yaml-builder';
import { structurallyEqual } from '../lib/graph/identity';

export type WizardStep = 'type' | 'template' | 'form' | 'review';

// Counts from a loaded OpenAPI spec, published by the operations picker so the
// Review step can quote them after the form has unmounted.
export interface OpenAPIOperationStats {
  // Operations that can become tools (skipped rows excluded).
  total: number;
  // DELETE operations that will become tools under the current filter.
  deleteCount: number;
}

// The MCP server the Review step appended to the stack, keyed to the exact
// YAML that was sent so a revisit with unchanged YAML checks the deployment
// instead of appending again.
export interface AppendedResource {
  resourceType: string;
  name: string;
  yaml: string;
}

export interface WizardDraft {
  id: string;
  name: string;
  resourceType: string;
  formData: Record<string, unknown>;
  // YAML text of a draft saved in YAML mode.
  yaml?: string;
  createdAt: string;
  updatedAt: string;
}

interface WizardState {
  // Modal visibility
  isOpen: boolean;

  // Wizard navigation
  currentStep: WizardStep;
  selectedType: ResourceType | null;
  selectedTemplate: string | null;

  // Form data per resource type (persists across type switches)
  formData: {
    stack: StackFormData;
    'mcp-server': MCPServerFormData;
    resource: ResourceFormData;
  };

  // Expert mode
  expertMode: boolean;
  yamlContent: string;
  yamlError: string | null;

  // Counts from the last OpenAPI spec load. Transient UI state, not config: it
  // lets the Review step say "12 of 517" instead of just "12", and repeat the
  // destructive-operation count at the point of deploy. Null whenever no spec
  // has been loaded this session, which is the normal case for
  // manually-entered operation IDs.
  openapiOperationStats: OpenAPIOperationStats | null;

  // True while a skill or pack import has progress a dismiss would discard.
  // Published by the import wizards; never persisted to sessionStorage.
  importInProgress: boolean;

  // Persisted across close and reload; cleared by reset, loadDraft, and a
  // successful deploy.
  appendedResource: AppendedResource | null;

  // True while a stack append request is pending. Global to the wizard
  // because a reopened Review step can mount before an earlier append
  // settles. Never persisted to sessionStorage.
  appendInFlight: boolean;

  // Drafts
  drafts: WizardDraft[];
  draftsLoading: boolean;

  // Actions
  open: (presetType?: ResourceType) => void;
  close: () => void;
  setStep: (step: WizardStep) => void;
  setSelectedType: (type: ResourceType) => void;
  setSelectedTemplate: (template: string | null) => void;
  updateFormData: (type: ResourceType, data: Record<string, unknown>) => void;
  setExpertMode: (enabled: boolean) => void;
  setYamlContent: (content: string) => void;
  setYamlError: (error: string | null) => void;
  setOpenAPIOperationStats: (stats: OpenAPIOperationStats | null) => void;
  setImportInProgress: (value: boolean) => void;
  setAppendedResource: (record: AppendedResource | null) => void;
  setAppendInFlight: (value: boolean) => void;
  reset: () => void;

  // Draft actions
  setDrafts: (drafts: WizardDraft[]) => void;
  setDraftsLoading: (loading: boolean) => void;
  loadDraft: (draft: WizardDraft) => void;
}

const SESSION_KEY = 'gridctl-wizard-state';

const defaultFormData: WizardState['formData'] = {
  stack: { name: '', version: '1' },
  'mcp-server': { name: '', serverType: 'container' },
  resource: { name: '', image: '' },
};

const clearedYamlState = { expertMode: false, yamlContent: '', yamlError: null };

// isWizardDirty reports whether a reset or a draft load would discard work:
// form data that differs from the defaults, or YAML-mode text.
export function isWizardDirty(
  state: Pick<WizardState, 'formData' | 'expertMode' | 'yamlContent'>,
): boolean {
  if (state.expertMode && state.yamlContent.trim() !== '') return true;
  return (Object.keys(defaultFormData) as (keyof WizardState['formData'])[]).some((type) => {
    // A JSON round trip drops keys set to undefined, which the form and the
    // YAML parser both leave behind for cleared fields.
    const entry: unknown = JSON.parse(JSON.stringify(state.formData[type] ?? {}));
    return !structurallyEqual(entry, defaultFormData[type]);
  });
}

function saveSession(state: Partial<WizardState>) {
  try {
    const toSave = {
      isOpen: state.isOpen,
      currentStep: state.currentStep,
      selectedType: state.selectedType,
      selectedTemplate: state.selectedTemplate,
      formData: state.formData,
      expertMode: state.expertMode,
      yamlContent: state.yamlContent,
      appendedResource: state.appendedResource ?? null,
    };
    sessionStorage.setItem(SESSION_KEY, JSON.stringify(toSave));
  } catch {
    // sessionStorage may be unavailable
  }
}

function loadSession(): Partial<WizardState> | null {
  try {
    const raw = sessionStorage.getItem(SESSION_KEY);
    if (!raw) return null;
    return JSON.parse(raw);
  } catch {
    return null;
  }
}

function toAppendedResource(value: unknown): AppendedResource | null {
  if (!value || typeof value !== 'object') return null;
  const { resourceType, name, yaml } = value as Record<string, unknown>;
  if (typeof resourceType !== 'string' || typeof name !== 'string' || typeof yaml !== 'string') return null;
  return { resourceType, name, yaml };
}

const savedState = loadSession();

export const useWizardStore = create<WizardState>()(
  subscribeWithSelector((set, get) => ({
    isOpen: savedState?.isOpen ?? false,
    currentStep: (savedState?.currentStep as WizardStep) ?? 'type',
    selectedType: (savedState?.selectedType as ResourceType) ?? null,
    selectedTemplate: savedState?.selectedTemplate ?? null,
    formData: savedState?.formData ?? { ...defaultFormData },
    expertMode: savedState?.expertMode ?? false,
    yamlContent: savedState?.yamlContent ?? '',
    yamlError: null,
    openapiOperationStats: null,
    importInProgress: false,
    appendedResource: toAppendedResource(savedState?.appendedResource),
    appendInFlight: false,
    drafts: [],
    draftsLoading: false,

    open: (presetType) => {
      const updates: Partial<WizardState> = { isOpen: true };
      if (presetType) {
        if (presetType !== get().selectedType) Object.assign(updates, clearedYamlState);
        updates.selectedType = presetType;
        updates.currentStep = 'template';
      }
      set(updates);
      saveSession({ ...get(), ...updates });
    },

    close: () => {
      set({ isOpen: false, importInProgress: false });
      saveSession({ ...get(), isOpen: false });
    },

    setStep: (step) => {
      set({ currentStep: step });
      saveSession({ ...get(), currentStep: step });
    },

    setSelectedType: (type) => {
      const updates: Partial<WizardState> = { selectedType: type, currentStep: 'template' };
      if (type !== get().selectedType) Object.assign(updates, clearedYamlState);
      set(updates);
      saveSession({ ...get(), ...updates });
    },

    setSelectedTemplate: (template) => {
      set({ selectedTemplate: template, currentStep: 'form' });
      saveSession({ ...get(), selectedTemplate: template, currentStep: 'form' });
    },

    updateFormData: (type: ResourceType, data: Record<string, unknown>) => {
      const current = get().formData;
      const existing = current[type as keyof typeof current] ?? {};
      const updated = { ...current, [type]: { ...existing, ...data } };
      set({ formData: updated });
      saveSession({ ...get(), formData: updated });
    },

    setExpertMode: (enabled) => {
      set({ expertMode: enabled });
      saveSession({ ...get(), expertMode: enabled });
    },

    setYamlContent: (content) => {
      set({ yamlContent: content });
      saveSession({ ...get(), yamlContent: content });
    },

    setYamlError: (error) => {
      set({ yamlError: error });
    },

    // Deliberately not persisted to sessionStorage: it describes a spec fetched
    // this session, and a stale total quoted against a since-edited spec would
    // be worse than no total. After a page reload the Review step falls back to
    // the count-only wording.
    setOpenAPIOperationStats: (stats) => {
      set({ openapiOperationStats: stats });
    },

    setImportInProgress: (value) => {
      set({ importInProgress: value });
    },

    setAppendedResource: (record) => {
      set({ appendedResource: record });
      saveSession({ ...get(), appendedResource: record });
    },

    setAppendInFlight: (value) => {
      set({ appendInFlight: value });
    },

    reset: () => {
      const fresh = {
        currentStep: 'type' as WizardStep,
        selectedType: null,
        selectedTemplate: null,
        formData: { ...defaultFormData },
        expertMode: false,
        yamlContent: '',
        yamlError: null,
        openapiOperationStats: null,
        importInProgress: false,
        appendedResource: null,
      };
      set(fresh);
      saveSession({ ...get(), ...fresh });
    },

    setDrafts: (drafts) => set({ drafts }),
    setDraftsLoading: (loading) => set({ draftsLoading: loading }),

    loadDraft: (draft) => {
      const formData = { ...get().formData };
      const rt = draft.resourceType as ResourceType;
      if (rt in formData) {
        (formData as Record<string, unknown>)[rt] = draft.formData;
      }
      const updates = {
        selectedType: rt,
        currentStep: 'form' as WizardStep,
        formData,
        isOpen: true,
        ...(draft.yaml
          ? { expertMode: true, yamlContent: draft.yaml, yamlError: null }
          : clearedYamlState),
        // The draft carries its own spec; any counts from a previous load
        // describe a different document.
        openapiOperationStats: null,
        appendedResource: null,
      };
      set(updates);
      saveSession({ ...get(), ...updates });
    },
  })),
);
