import { useState, useEffect, useCallback, useRef, useMemo, useId } from 'react';
import { createPortal } from 'react-dom';
import {
  Layers,
  Server,
  Database,
  Puzzle,
  KeyRound,
  Globe,
  Plug,
  ArrowLeft,
  ArrowRight,
  RotateCcw,
  X,
  AlertCircle,
  FileCode2,
  Package,
} from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { Panel, Group as PanelGroup, Separator as PanelResizeHandle } from 'react-resizable-panels';
import { cn } from '../../lib/cn';
import { useFocusTrap } from '../../hooks/useFocusTrap';
import { useEscapeLayer } from '../../hooks/useEscapeLayer';
import { Button } from '../ui/Button';
import { ConfirmDialog } from '../ui/ConfirmDialog';
import { isWizardDirty, useWizardStore, type WizardStep } from '../../stores/useWizardStore';
import { useStackStore } from '../../stores/useStackStore';
import { useRegistryStore } from '../../stores/useRegistryStore';
import { useVaultStore } from '../../stores/useVaultStore';
import { buildYAML, customHeaderErrors, parseYAMLToForm, yamlToFormWithLoss, type ResourceType, type WizardFormData, type MCPServerFormData } from '../../lib/yaml-builder';
import { formatOperationsSummary } from '../../lib/openapiOperations';
import { formatNextReason, validateMCPServer, validateResource, validateStack, type WizardErrors } from '../../lib/wizard-validation';
import { catalogEntryToFormData, type CatalogContainerOptions } from '../../lib/catalog';
import { cloneFormData, defaultFormData, templateFormData, type TemplateResourceType } from '../../lib/wizard-templates';
import type { CatalogEntry } from '../../lib/api';
import { TemplateGrid } from './TemplateGrid';
import { CatalogPicker } from './CatalogPicker';
import { YAMLPreview } from './YAMLPreview';
import { ExpertModeToggle } from './ExpertModeToggle';
import { SegmentedControl } from './SegmentedControl';
import { DraftManager } from './DraftManager';
import { ReviewStep } from './steps/ReviewStep';
import { MCPServerForm } from './steps/MCPServerForm';
import { StackForm } from './steps/StackForm';
import { ResourceForm } from './steps/ResourceForm';
import { SkillImportWizard } from './steps/SkillImportWizard';
import { PackImportWizard } from './steps/PackImportWizard';

interface ResourceTypeCard {
  type: ResourceType;
  icon: LucideIcon;
  label: string;
  description: string;
  color: string;
  glowColor: string;
}

const resourceTypes: ResourceTypeCard[] = [
  {
    type: 'stack',
    icon: Layers,
    label: 'Stack',
    description: 'Complete stack with servers and resources',
    color: 'text-primary',
    glowColor: 'color-mix(in srgb, var(--color-primary) 10%, transparent)',
  },
  {
    type: 'mcp-server',
    icon: Server,
    label: 'MCP Server',
    description: 'Container, external, SSH, or OpenAPI server',
    color: 'text-primary',
    glowColor: 'color-mix(in srgb, var(--color-primary) 10%, transparent)',
  },
  {
    type: 'resource',
    icon: Database,
    label: 'Resource',
    description: 'Database, cache, or supporting container',
    color: 'text-secondary',
    glowColor: 'color-mix(in srgb, var(--color-secondary) 10%, transparent)',
  },
  {
    type: 'skill',
    icon: Puzzle,
    label: 'Skill',
    description: 'Import skills from a Git repository',
    color: 'text-secondary',
    glowColor: 'color-mix(in srgb, var(--color-secondary) 10%, transparent)',
  },
  {
    type: 'pack',
    icon: Package,
    label: 'Pack',
    description: 'Import a repo with gridctl-pack.yaml: skills, agents, rules, and wiring as one unit',
    color: 'text-secondary',
    glowColor: 'color-mix(in srgb, var(--color-secondary) 10%, transparent)',
  },
  {
    type: 'secret',
    icon: KeyRound,
    label: 'Variable',
    description: 'Add variables to the encrypted store',
    color: 'text-status-pending',
    glowColor: 'color-mix(in srgb, var(--color-status-pending) 10%, transparent)',
  },
  {
    type: 'global-context',
    icon: Globe,
    label: 'Global Context',
    description: 'Global rules synced to every linked client',
    color: 'text-secondary',
    glowColor: 'color-mix(in srgb, var(--color-secondary) 10%, transparent)',
  },
  {
    type: 'client-link',
    icon: Plug,
    label: 'Client Link',
    description: 'Connect LLM clients to the gateway',
    color: 'text-primary',
    glowColor: 'color-mix(in srgb, var(--color-primary) 10%, transparent)',
  },
];

const stepLabels: Record<WizardStep, string> = {
  type: 'Resource Type',
  template: 'Template',
  form: 'Configure',
  review: 'Review',
};

const stepOrder: WizardStep[] = ['type', 'template', 'form', 'review'];

function getResourceCounts(
  mcpServers: { name: string }[],
  resources: { name: string }[],
  skills: { name: string }[] | null,
  linkedClients: number,
  secretCount: number | null,
  packCount: number | null,
): Record<ResourceType, number | null> {
  return {
    stack: null,
    'mcp-server': mcpServers.length,
    resource: resources.length,
    skill: (skills ?? []).length,
    pack: packCount,
    secret: secretCount,
    'global-context': null,
    'client-link': linkedClients,
  };
}

interface CreationWizardProps {
  onOpenVault?: () => void;
  onOpenGlobalContext?: () => void;
  onOpenConnections?: () => void;
  onDeploy?: () => void;
}

export function CreationWizard({ onOpenVault, onOpenGlobalContext, onOpenConnections, onDeploy }: CreationWizardProps) {
  const {
    isOpen,
    close,
    currentStep,
    setStep,
    selectedType,
    setSelectedType,
    selectedTemplate,
    setSelectedTemplate,
    formData,
    updateFormData,
    applyTemplate,
    expertMode,
    setExpertMode,
    yamlContent,
    setYamlContent,
    yamlError,
    setYamlError,
    openapiOperationStats,
    importInProgress,
    appendInFlight,
    secretsRedacted,
    dismissSecretsRedacted,
    reset,
  } = useWizardStore();
  const titleId = useId();
  const panelRef = useFocusTrap<HTMLDivElement>({ active: isOpen });

  // One-line outcome of the OpenAPI operations filter for the Review step.
  // Only meaningful for an OpenAPI server; every other type leaves it null so
  // the summary grid keeps its four cells.
  const mcpServerData = formData['mcp-server'];
  const operationsSummary = useMemo(() => {
    if (selectedType !== 'mcp-server' || mcpServerData?.serverType !== 'openapi') return null;
    return formatOperationsSummary(
      mcpServerData.openapi?.operations,
      openapiOperationStats?.total ?? null,
      openapiOperationStats?.deleteCount ?? null,
    );
  }, [selectedType, mcpServerData, openapiOperationStats]);

  const mcpServersRaw = useStackStore((s) => s.mcpServers);
  const resourcesRaw = useStackStore((s) => s.resources);
  const mcpServers = useMemo(() => mcpServersRaw ?? [], [mcpServersRaw]);
  const resources = useMemo(() => resourcesRaw ?? [], [resourcesRaw]);
  const skills = useRegistryStore((s) => s.skills);
  const packs = useRegistryStore((s) => s.packs);
  const variables = useVaultStore((s) => s.variables);
  const clients = useStackStore((s) => s.clients);
  const linkedClients = useMemo(() => clients.filter((c) => c.linked).length, [clients]);
  const counts = useMemo(
    () => getResourceCounts(
      mcpServers,
      resources,
      skills,
      linkedClients,
      variables == null ? null : variables.length,
      packs == null ? null : packs.length,
    ),
    [mcpServers, resources, skills, linkedClients, variables, packs],
  );

  const [touched, setTouched] = useState<Set<string>>(() => new Set());
  const [nextAttempted, setNextAttempted] = useState(false);
  const [revealKey, setRevealKey] = useState(0);
  const focusPendingRef = useRef(false);
  const validationScope = `${isOpen}:${selectedType}`;
  const [prevValidationScope, setPrevValidationScope] = useState(validationScope);
  if (prevValidationScope !== validationScope) {
    setPrevValidationScope(validationScope);
    setTouched(new Set());
    setNextAttempted(false);
  }

  // A loaded draft or an applied template is a fresh form even when its type
  // matches the current one.
  const resetValidation = useCallback(() => {
    setTouched(new Set());
    setNextAttempted(false);
  }, []);

  const handleTemplateSelect = useCallback((templateId: string | null) => {
    const data = selectedType && templateId ? templateFormData(selectedType, templateId) : null;
    if (selectedType && templateId && data) {
      applyTemplate(selectedType as TemplateResourceType, templateId, data);
      resetValidation();
      return;
    }
    setSelectedTemplate(templateId);
  }, [selectedType, setSelectedTemplate, applyTemplate, resetValidation]);

  // Catalog entries replace the mcp-server form through the same action
  // templates use, so the form, YAML preview, and review step work unchanged.
  const handleCatalogSelect = useCallback((entry: CatalogEntry, container?: CatalogContainerOptions) => {
    const data: MCPServerFormData = {
      ...cloneFormData(defaultFormData['mcp-server']),
      ...catalogEntryToFormData(entry, container),
    };
    applyTemplate('mcp-server', `catalog:${entry.name}`, data);
    resetValidation();
  }, [applyTemplate, resetValidation]);

  // Skill skips template step; secret, global-context, and client-link
  // close the wizard and open their own dedicated surfaces (Variables
  // workspace, the Library's Context segment, Connections workspace). Linking
  // is stateful and reversible per machine, so it lives in a workspace rather
  // than a one-shot creation flow.
  const handleTypeSelect = useCallback((type: ResourceType) => {
    if (type === 'secret') {
      close();
      onOpenVault?.();
      return;
    }
    if (type === 'global-context') {
      close();
      onOpenGlobalContext?.();
      return;
    }
    if (type === 'client-link') {
      close();
      onOpenConnections?.();
      return;
    }
    setSelectedType(type);
    if (type === 'skill' || type === 'pack') {
      setStep('form');
    }
  }, [setSelectedType, setStep, close, onOpenVault, onOpenGlobalContext, onOpenConnections]);

  const yamlDebounceRef = useRef<ReturnType<typeof setTimeout>>(null);
  const [generatedYaml, setGeneratedYaml] = useState('');

  // Generate YAML from form data (debounced)
  useEffect(() => {
    if (!selectedType || !isOpen) return;
    if (selectedType === 'skill' || selectedType === 'pack' || selectedType === 'secret') return;

    if (yamlDebounceRef.current) clearTimeout(yamlDebounceRef.current);
    yamlDebounceRef.current = setTimeout(() => {
      const currentData = formData[selectedType as keyof typeof formData];
      if (!currentData) return;
      try {
        const yaml = buildYAML({ type: selectedType, data: currentData } as WizardFormData);
        setGeneratedYaml(yaml);
        if (!expertMode) {
          setYamlContent(yaml);
        }
      } catch {
        // YAML build failed silently
      }
    }, 150);

    return () => {
      if (yamlDebounceRef.current) clearTimeout(yamlDebounceRef.current);
    };
  }, [formData, selectedType, isOpen, expertMode, setYamlContent]);

  const [pendingAction, setPendingAction] = useState<'close' | 'reset' | null>(null);

  // Form and YAML state persist across close, so only an import with
  // progress needs a confirm before the wizard closes.
  const requestClose = useCallback(() => {
    if (useWizardStore.getState().importInProgress) {
      setPendingAction('close');
      return;
    }
    close();
  }, [close]);

  const requestReset = useCallback(() => {
    const state = useWizardStore.getState();
    if (state.importInProgress || isWizardDirty(state)) {
      setPendingAction('reset');
      return;
    }
    reset();
  }, [reset]);

  const confirmPendingAction = () => {
    const action = pendingAction;
    setPendingAction(null);
    if (action === 'close') close();
    else if (action === 'reset') reset();
  };

  useEscapeLayer({ active: isOpen, onEscape: requestClose, modal: true, ref: panelRef });

  const editYamlContent = useCallback((content: string) => {
    setYamlContent(content);
    if (useWizardStore.getState().yamlError !== null) setYamlError(null);
  }, [setYamlContent, setYamlError]);

  const handleExpertModeToggle = (enabled: boolean) => {
    if (enabled) {
      // Switching to YAML mode: populate with current form data
      setYamlContent(generatedYaml);
      setYamlError(null);
    } else {
      // Switching back to form: parse YAML back, refusing a switch that
      // would drop, change, or restore anything the form cannot regenerate.
      if (selectedType) {
        const current = formData[selectedType as keyof typeof formData] as unknown as Record<string, unknown>;
        const result = yamlToFormWithLoss(yamlContent, selectedType, current ?? {});
        if ('error' in result) {
          setYamlError(result.error);
          return; // Stay in YAML mode
        }
        if (result.lostKeys.length > 0) {
          setYamlError(lossMessage(result.lostKeys));
          return;
        }
        updateFormData(selectedType as keyof typeof formData, result.data);
        setYamlError(null);
      }
    }
    setExpertMode(enabled);
  };

  const handleDeploy = useCallback(() => {
    reset();
    close();
    onDeploy?.();
  }, [reset, close, onDeploy]);

  const formErrors = useMemo<WizardErrors>(() => {
    const ctx = {
      serverNames: mcpServers.map((s) => s.name),
      resourceNames: resources.map((r) => r.name),
    };
    switch (selectedType) {
      case 'mcp-server':
        return validateMCPServer(formData['mcp-server'], ctx);
      case 'resource':
        return validateResource(formData.resource, ctx);
      case 'stack':
        return validateStack(formData.stack);
      default:
        return {};
    }
  }, [selectedType, formData, mcpServers, resources]);

  const visibleErrors = useMemo<WizardErrors>(() => {
    if (nextAttempted) return formErrors;
    return Object.fromEntries(Object.entries(formErrors).filter(([key]) => touched.has(key)));
  }, [formErrors, touched, nextAttempted]);

  const handleFieldBlur = useCallback((event: React.FocusEvent<HTMLElement>) => {
    const key = event.target.getAttribute?.('data-wizard-field');
    if (!key) return;
    setTouched((prev) => (prev.has(key) ? prev : new Set(prev).add(key)));
  }, []);

  const handleRemoveItem = useCallback((list: 'mcpServers' | 'resources') => {
    setTouched((prev) => new Set([...prev].filter((key) => !key.startsWith(`${list}.`))));
  }, []);

  useEffect(() => {
    if (!focusPendingRef.current) return;
    focusPendingRef.current = false;
    const fields = panelRef.current?.querySelectorAll<HTMLElement>('[data-wizard-field]') ?? [];
    const first = Array.from(fields).find((el) => el.getAttribute('data-wizard-field')! in formErrors);
    first?.focus();
  }, [revealKey, formErrors, panelRef]);

  const canGoNext = () => {
    switch (currentStep) {
      case 'type':
        return selectedType !== null;
      case 'template':
        return selectedTemplate !== null;
      case 'form':
        return headersValid();
      default:
        return false;
    }
  };

  function headersValid(): boolean {
    if (selectedType === 'mcp-server') {
      const server = formData['mcp-server'];
      // Conflict checks mirror the server: only top-level auth: counts.
      // openapi.auth wins Authorization at request time and is not a conflict.
      const authSet = Boolean(server.auth);
      if (server.serverType === 'external' || server.serverType === 'openapi') {
        return Object.keys(customHeaderErrors(server.headers, authSet)).length === 0;
      }
    }
    return true;
  }

  // On the form step Next stays focusable while blocked (aria-disabled rather
  // than disabled) so a click can reveal the errors and keyboard users can
  // reach the reason. YAML mode keeps the native disabled behavior.
  const formGateActive = currentStep === 'form' && !expertMode;
  const nextBlockedReason = !formGateActive
    ? null
    : Object.keys(formErrors).length > 0
      ? formatNextReason(formErrors)
      : !headersValid()
        ? 'Fix the custom header errors'
        : null;
  const nextReasonId = useId();

  const handleBlockedNext = () => {
    setNextAttempted(true);
    focusPendingRef.current = true;
    setRevealKey((k) => k + 1);
  };

  const goNext = () => {
    const idx = stepOrder.indexOf(currentStep);
    if (idx < stepOrder.length - 1) {
      setStep(stepOrder[idx + 1]);
    }
  };

  const goBack = () => {
    const idx = stepOrder.indexOf(currentStep);
    if (idx > 0) {
      setStep(stepOrder[idx - 1]);
    }
  };

  const formValidation: FormValidation = {
    errors: visibleErrors,
    revealKey,
    onFieldBlur: handleFieldBlur,
    onRemoveItem: handleRemoveItem,
  };

  const currentStepIdx = stepOrder.indexOf(currentStep);
  const isImportFlow = (selectedType === 'skill' || selectedType === 'pack') && currentStep !== 'type';
  const titleRef = useRef<HTMLHeadingElement>(null);
  const stepTextRef = useRef<HTMLSpanElement>(null);
  const prevStepRef = useRef(currentStep);

  // Starts at the mounted step so opening the wizard leaves initial focus to
  // the focus trap. On a later step change the step text takes focus; when it
  // is hidden (an import flow), the title does, but only if focus has left the
  // dialog, so a field that autofocused in the new step keeps it.
  useEffect(() => {
    const changed = prevStepRef.current !== currentStep;
    prevStepRef.current = currentStep;
    if (!isOpen || !changed) return;
    if (stepTextRef.current) {
      stepTextRef.current.focus();
      return;
    }
    if (!panelRef.current?.contains(document.activeElement)) {
      titleRef.current?.focus();
    }
  }, [isOpen, currentStep, panelRef]);

  const showPreviewPanel = currentStep === 'form' && selectedType !== 'skill' && selectedType !== 'pack' && selectedType !== 'secret';

  if (!isOpen) return null;

  return createPortal(
    <div className="fixed inset-0 z-modal animate-fade-in-scale bg-background/80 backdrop-blur-sm flex items-center justify-center">
      {/* Backdrop */}
      <div className="absolute inset-0" onClick={requestClose} />

      {/* Panel */}
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        className="relative flex flex-col glass-panel-elevated rounded-xl w-full max-w-5xl mx-4 h-[85vh] shadow-lg transition-[max-width] duration-300 ease-out"
      >
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-border/30 flex-shrink-0">
          <div className="flex items-center gap-4">
            <h2 id={titleId} ref={titleRef} tabIndex={-1} className="text-sm font-medium text-text-primary">
              {currentStep === 'type'
                ? 'Create new'
                : selectedType
                  ? `New ${resourceTypes.find((r) => r.type === selectedType)?.label}`
                  : 'Create'}
            </h2>

            {/* Step Indicator */}
            {!isImportFlow && (
            <>
            <div className="flex items-center gap-1" aria-hidden="true">
              {stepOrder.map((step, i) => (
                <div key={step} className="flex items-center">
                  <div
                    className={cn(
                      'w-1.5 h-1.5 rounded-full transition-all duration-200',
                      i === currentStepIdx
                        ? 'bg-primary w-4'
                        : i < currentStepIdx
                          ? 'bg-primary/40'
                          : 'bg-border',
                    )}
                  />
                  {i < stepOrder.length - 1 && (
                    <div className="w-3 h-px bg-border/30 mx-0.5" />
                  )}
                </div>
              ))}
            </div>
            <span ref={stepTextRef} tabIndex={-1} className="text-3xs text-text-muted tracking-wider">
              Step {currentStepIdx + 1} of {stepOrder.length}: {stepLabels[currentStep]}
            </span>
            </>
            )}
          </div>

          <div className="flex items-center gap-2">
            {/* Expert Mode Toggle (visible during form step) */}
            {currentStep === 'form' && selectedType !== 'skill' && selectedType !== 'pack' && selectedType !== 'secret' && (
              <ExpertModeToggle
                expertMode={expertMode}
                onToggle={handleExpertModeToggle}
              />
            )}

            {/* Draft Manager */}
            <div className="relative">
              <DraftManager onLoad={resetValidation} />
            </div>

            {/* Reset */}
            <button
              type="button"
              onClick={requestReset}
              disabled={appendInFlight}
              title="Start over"
              aria-label="Start over"
              className="p-1.5 rounded-lg hover:bg-surface-highlight transition-colors text-text-muted hover:text-text-secondary disabled:opacity-50 disabled:cursor-not-allowed disabled:hover:bg-transparent"
            >
              <RotateCcw size={14} />
            </button>

            {/* Close */}
            <button
              type="button"
              onClick={requestClose}
              aria-label="Close"
              className="p-1.5 rounded-lg hover:bg-surface-highlight transition-colors"
            >
              <X size={14} className="text-text-muted" />
            </button>
          </div>
        </div>

        {/* Content */}
        <div className="flex-1 min-h-0 overflow-hidden">
          {/* Skill and Pack import wizards — own step flows, no preview panel */}
          {selectedType === 'skill' && currentStep !== 'type' ? (
            <div className="h-full overflow-y-auto scrollbar-dark px-6 py-4">
              <SkillImportWizard />
            </div>
          ) : selectedType === 'pack' && currentStep !== 'type' ? (
            <div className="h-full overflow-y-auto scrollbar-dark px-6 py-4">
              <PackImportWizard />
            </div>
          ) : showPreviewPanel ? (
            <PanelGroup orientation="horizontal" className="h-full" resizeTargetMinimumSize={{ coarse: 24, fine: 16 }}>
              {/* Form Panel */}
              <Panel defaultSize="55%" minSize="40%">
                <div className="flex flex-col h-full">
                  <div className="flex-1 overflow-y-auto scrollbar-dark px-6 py-4">
                    {secretsRedacted && (
                      <div role="status" className="mb-3 flex items-start gap-2 rounded-lg border border-status-pending/30 bg-status-pending/5 px-3 py-2 text-2xs text-text-secondary">
                        <span className="flex-1">Literal secrets are not kept across a reload. Re-enter them or use a stored variable.</span>
                        <button
                          type="button"
                          onClick={dismissSecretsRedacted}
                          aria-label="Dismiss notice"
                          className="p-0.5 text-text-muted hover:text-text-secondary transition-colors"
                        >
                          <X size={12} />
                        </button>
                      </div>
                    )}
                    {renderStepContent(
                      currentStep,
                      selectedType,
                      selectedTemplate,
                      handleTypeSelect,
                      handleTemplateSelect,
                      formData,
                      updateFormData,
                      expertMode,
                      yamlContent,
                      editYamlContent,
                      yamlError,
                      generatedYaml,
                      counts,
                      handleDeploy,
                      handleCatalogSelect,
                      operationsSummary,
                      formValidation,
                    )}
                  </div>
                </div>
              </Panel>

              {/* Resize Handle */}
              <PanelResizeHandle className="w-px bg-border/30 hover:bg-primary/30 transition-colors" />

              {/* Preview Panel */}
              <Panel defaultSize="45%" minSize="30%">
                <YAMLPreview yaml={expertMode ? yamlContent : generatedYaml} resourceType={selectedType ?? 'stack'} />
              </Panel>
            </PanelGroup>
          ) : (
            <div className="h-full overflow-y-auto scrollbar-dark px-6 py-4">
              {renderStepContent(
                currentStep,
                selectedType,
                selectedTemplate,
                handleTypeSelect,
                handleTemplateSelect,
                formData,
                updateFormData,
                expertMode,
                yamlContent,
                editYamlContent,
                yamlError,
                generatedYaml,
                counts,
                handleDeploy,
                handleCatalogSelect,
                operationsSummary,
                formValidation,
              )}
            </div>
          )}
        </div>

        {/* Footer — hidden when an import wizard is active (they have their own) */}
        {!((selectedType === 'skill' || selectedType === 'pack') && currentStep !== 'type') && (
        <div className="flex items-center justify-between px-6 py-3 border-t border-border/30 flex-shrink-0">
          <div>
            {currentStepIdx > 0 && (
              <Button variant="ghost" size="sm" onClick={goBack} disabled={appendInFlight}>
                <ArrowLeft size={14} />
                Back
              </Button>
            )}
          </div>
          <div className="flex items-center gap-3 min-w-0">
            {currentStep !== 'review' && nextBlockedReason && (
              <p id={nextReasonId} title={nextBlockedReason} className="text-2xs text-status-error truncate">
                {nextBlockedReason}
              </p>
            )}
            {currentStep !== 'review' && (
              <Button
                variant="primary"
                size="sm"
                onClick={nextBlockedReason ? handleBlockedNext : goNext}
                disabled={formGateActive ? undefined : !canGoNext()}
                aria-disabled={nextBlockedReason ? true : undefined}
                aria-describedby={nextBlockedReason ? nextReasonId : undefined}
                className="flex-shrink-0 aria-disabled:opacity-50 aria-disabled:cursor-not-allowed aria-disabled:transform-none aria-disabled:shadow-none aria-disabled:hover:translate-y-0 aria-disabled:hover:from-primary aria-disabled:hover:to-primary-dark aria-disabled:hover:shadow-none"
              >
                Next
                <ArrowRight size={14} />
              </Button>
            )}
          </div>
        </div>
        )}

        <ConfirmDialog
          isOpen={pendingAction !== null}
          onClose={() => setPendingAction(null)}
          onConfirm={confirmPendingAction}
          title={pendingAction === 'reset' ? 'Start over?' : 'Discard this import?'}
          message={
            pendingAction === 'reset' ? (
              <p>This clears the form and any YAML edits{importInProgress ? ', and discards the import in progress' : ''}.</p>
            ) : (
              <p>Closing the wizard discards the import in progress. Nothing has been imported yet.</p>
            )
          }
          confirmLabel={pendingAction === 'reset' ? 'Start over' : 'Discard import'}
          variant="danger"
        />
      </div>
    </div>,
    document.body,
  );
}

function renderStepContent(
  step: WizardStep,
  selectedType: ResourceType | null,
  selectedTemplate: string | null,
  setSelectedType: (type: ResourceType) => void,
  setSelectedTemplate: (id: string | null) => void,
  formData: ReturnType<typeof useWizardStore.getState>['formData'],
  updateFormData: ReturnType<typeof useWizardStore.getState>['updateFormData'],
  expertMode: boolean,
  yamlContent: string,
  setYamlContent: (content: string) => void,
  yamlError: string | null,
  generatedYaml: string,
  counts: Record<ResourceType, number | null>,
  onDeploy: () => void,
  onCatalogSelect: (entry: CatalogEntry, container?: CatalogContainerOptions) => void,
  operationsSummary: string | null,
  validation: FormValidation,
) {
  switch (step) {
    case 'type':
      return <TypePicker selected={selectedType} onSelect={setSelectedType} counts={counts} />;
    case 'template':
      if (!selectedType) return null;
      if (selectedType === 'mcp-server') {
        return (
          <MCPTemplateStep
            selected={selectedTemplate}
            onTemplateSelect={setSelectedTemplate}
            onCatalogSelect={onCatalogSelect}
          />
        );
      }
      return (
        <TemplateGrid
          resourceType={selectedType}
          selected={selectedTemplate}
          onSelect={setSelectedTemplate}
        />
      );
    case 'form':
      if (!selectedType) return null;
      if (expertMode) {
        return (
          <ExpertEditor
            yaml={yamlContent}
            onChange={setYamlContent}
            error={yamlError}
          />
        );
      }
      return (
        <FormRenderer
          resourceType={selectedType}
          formData={formData}
          updateFormData={updateFormData}
          validation={validation}
        />
      );
    case 'review':
      return (
        <ReviewStep
          yaml={expertMode ? yamlContent : generatedYaml}
          resourceType={selectedType || 'stack'}
          resourceName={
            selectedType
              ? (formData[selectedType as keyof typeof formData] as { name?: string })?.name || ''
              : ''
          }
          onDeploy={onDeploy}
          operationsSummary={operationsSummary}
          server={selectedType === 'mcp-server'
            ? reviewServerData(expertMode ? yamlContent : generatedYaml, formData['mcp-server'])
            : undefined}
        />
      );
  }
}

const LOST_KEYS_SHOWN = 5;

function lossMessage(keys: string[]): string {
  const shown = keys.slice(0, LOST_KEYS_SHOWN).join(', ');
  const more = keys.length > LOST_KEYS_SHOWN ? `, and ${keys.length - LOST_KEYS_SHOWN} more` : '';
  return `Switching to Form would drop, change, or restore: ${shown}${more}. Stay in YAML mode and continue to Review, or undo these edits.`;
}

function reviewServerData(yaml: string, fallback: MCPServerFormData): MCPServerFormData {
  const parsed = parseYAMLToForm(yaml, 'mcp-server');
  return 'error' in parsed ? fallback : parsed.data as MCPServerFormData;
}

// MCP-server template step: local templates or the server catalog
// (curated set plus MCP Registry), switched by a segmented toggle.
function MCPTemplateStep({
  selected,
  onTemplateSelect,
  onCatalogSelect,
}: {
  selected: string | null;
  onTemplateSelect: (id: string | null) => void;
  onCatalogSelect: (entry: CatalogEntry, container?: CatalogContainerOptions) => void;
}) {
  const [mode, setMode] = useState<'templates' | 'catalog'>('templates');

  return (
    <div className="h-full flex flex-col">
      <SegmentedControl
        ariaLabel="Server source"
        value={mode}
        onChange={setMode}
        className="self-start mb-4"
        options={[
          { value: 'templates', label: 'Templates', icon: FileCode2 },
          { value: 'catalog', label: 'Catalog', icon: Package },
        ]}
      />
      {mode === 'templates' ? (
        <TemplateGrid resourceType="mcp-server" selected={selected} onSelect={onTemplateSelect} />
      ) : (
        <div className="flex-1 min-h-0 rounded-xl border border-border-subtle bg-surface-overlay-1 overflow-hidden">
          <CatalogPicker onSelect={onCatalogSelect} />
        </div>
      )}
    </div>
  );
}

const STACK_GATED_TYPES: ResourceType[] = ['mcp-server', 'resource'];

// Resource Type Picker — 3x2 glass-panel grid
function TypePicker({
  selected,
  onSelect,
  counts,
}: {
  selected: ResourceType | null;
  onSelect: (type: ResourceType) => void;
  counts: Record<ResourceType, number | null>;
}) {
  const stackName = useStackStore((s) => s.stackName);
  const hasActiveStack = stackName !== '';

  return (
    <div className="py-4">
      <div className="text-center mb-8">
        <h3 className="text-lg font-medium text-text-primary mb-1">
          What would you like to create?
        </h3>
        <p className="text-xs text-text-muted">
          Choose a resource type to begin building your spec
        </p>
      </div>
      <div className="grid grid-cols-1 min-[400px]:grid-cols-2 md:grid-cols-3 gap-3 max-w-2xl mx-auto">
        {resourceTypes.map((rt, i) => {
          const Icon = rt.icon;
          const isSelected = selected === rt.type;
          const count = counts[rt.type];
          const showCount = typeof count === 'number' && count > 0;
          const isGated = STACK_GATED_TYPES.includes(rt.type) && !hasActiveStack;
          return (
            <button
              key={rt.type}
              type="button"
              onClick={() => !isGated && onSelect(rt.type)}
              aria-disabled={isGated ? 'true' : undefined}
              aria-pressed={isGated ? undefined : isSelected}
              className={cn(
                'group relative flex flex-col items-center text-center p-5 rounded-xl border transition-all duration-200 focus-visible:-outline-offset-2',
                'animate-fade-in-up',
                isGated
                  ? 'cursor-not-allowed bg-surface-overlay-2 border-border-subtle'
                  : [
                      'bg-surface-overlay-2 hover:bg-surface-overlay-3',
                      isSelected
                        ? 'border-primary/50 shadow-[0_0_24px_--alpha(var(--color-primary)/10%)]'
                        : 'border-border-subtle hover:border-border',
                    ],
              )}
              style={{ animationDelay: `${i * 50}ms`, animationFillMode: 'backwards' }}
            >
              {showCount && (
                <div className={cn('absolute top-2.5 right-2.5 text-3xs font-medium text-text-muted bg-surface-highlight px-1.5 py-0.5 rounded-full', isGated && 'opacity-40')}>
                  {count}
                </div>
              )}
              <div
                className={cn(
                  'w-10 h-10 rounded-xl flex items-center justify-center mb-3 transition-all duration-200',
                  'bg-surface-elevated border border-border/40',
                  isGated ? 'opacity-40' : 'group-hover:border-border',
                  isSelected && !isGated && 'border-primary/30',
                )}
                style={
                  isSelected && !isGated
                    ? { boxShadow: `0 0 20px ${rt.glowColor}` }
                    : undefined
                }
              >
                <Icon
                  size={18}
                  className={cn(
                    rt.color,
                    !isGated && 'transition-transform duration-200 group-hover:scale-110',
                  )}
                />
              </div>
              <div className={cn('text-sm font-medium text-text-primary mb-1', isGated && 'opacity-40')}>
                {rt.label}
              </div>
              {isGated ? (
                <div className="text-3xs text-text-secondary leading-relaxed">
                  Requires an active stack. Create a Stack first.
                </div>
              ) : (
                <div className="text-3xs text-text-muted leading-relaxed">
                  {rt.description}
                </div>
              )}
            </button>
          );
        })}
      </div>
    </div>
  );
}

interface FormValidation {
  errors: WizardErrors;
  revealKey: number;
  onFieldBlur: (event: React.FocusEvent<HTMLElement>) => void;
  onRemoveItem: (list: 'mcpServers' | 'resources') => void;
}

// Form renderer for each resource type. Fields carry data-wizard-field, and a
// single bubbling blur handler here marks them touched.
function FormRenderer({
  resourceType,
  formData,
  updateFormData,
  validation,
}: {
  resourceType: ResourceType;
  formData: ReturnType<typeof useWizardStore.getState>['formData'];
  updateFormData: ReturnType<typeof useWizardStore.getState>['updateFormData'];
  validation: FormValidation;
}) {
  // MCP Server — full form
  if (resourceType === 'mcp-server') {
    return (
      <div onBlur={validation.onFieldBlur}>
        <MCPServerForm
          data={formData['mcp-server']}
          onChange={(partial) => updateFormData('mcp-server', partial as Record<string, unknown>)}
          errors={validation.errors}
          revealKey={validation.revealKey}
        />
      </div>
    );
  }

  // Stack — full composition form
  if (resourceType === 'stack') {
    return (
      <div onBlur={validation.onFieldBlur}>
        <StackForm
          data={formData['stack']}
          onChange={(partial) => updateFormData('stack', partial as Record<string, unknown>)}
          errors={validation.errors}
          revealKey={validation.revealKey}
          onRemoveItem={validation.onRemoveItem}
        />
      </div>
    );
  }

  // Resource: full form
  if (resourceType === 'resource') {
    return (
      <div onBlur={validation.onFieldBlur}>
        <ResourceForm
          data={formData['resource']}
          onChange={(partial) => updateFormData('resource', partial as Record<string, unknown>)}
          errors={validation.errors}
          revealKey={validation.revealKey}
        />
      </div>
    );
  }

  // Secret — placeholder (vault panel handles secrets)
  return (
    <div className="space-y-4">
      <div className="text-xs text-text-muted uppercase tracking-wider font-medium mb-2">
        Configure {resourceType}
      </div>
      <div className="mt-6 p-4 rounded-xl bg-surface-overlay-1 border border-border-subtle text-center">
        <p className="text-xs text-text-muted">
          Use the Variables workspace to manage variables.
        </p>
      </div>
    </div>
  );
}

// Expert mode YAML editor (textarea-based)
function ExpertEditor({
  yaml,
  onChange,
  error,
}: {
  yaml: string;
  onChange: (content: string) => void;
  error: string | null;
}) {
  return (
    <div className="flex flex-col gap-3 h-full">
      {error && (
        <div role="alert" className="flex items-center gap-2 px-3 py-2 rounded-lg bg-status-error/10 border border-status-error/20 text-status-error text-xs">
          <AlertCircle size={12} />
          <span>{error}</span>
        </div>
      )}
      <div className="flex items-start gap-2 px-3 py-2 rounded-lg bg-background/40 border border-border/40 text-text-muted text-2xs">
        <AlertCircle size={12} className="mt-0.5 shrink-0" />
        <span>
          The form holds only some of these settings. Switching to Form is refused when it
          would drop, change, or restore anything edited here. Review works from YAML mode.
        </span>
      </div>
      <textarea
        value={yaml}
        onChange={(e) => onChange(e.target.value)}
        aria-label="YAML"
        spellCheck={false}
        className={cn(
          'w-full flex-1 min-h-0 font-mono text-2xs leading-[1.7] resize-none',
          'bg-background/40 border rounded-xl px-4 py-3',
          'text-text-primary',
          'placeholder:text-text-muted scrollbar-dark transition-colors',
          error ? 'border-status-error/30' : 'border-border/40',
        )}
        placeholder="# Enter your YAML here..."
      />
    </div>
  );
}
