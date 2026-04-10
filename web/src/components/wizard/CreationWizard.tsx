import { useState, useEffect, useCallback, useRef, useMemo } from 'react';
import { createPortal } from 'react-dom';
import {
  Layers,
  Server,
  Database,
  Puzzle,
  KeyRound,
  ArrowLeft,
  ArrowRight,
  RotateCcw,
  X,
  AlertCircle,
} from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { Panel, Group as PanelGroup, Separator as PanelResizeHandle } from 'react-resizable-panels';
import { cn } from '../../lib/cn';
import { Button } from '../ui/Button';
import { useWizardStore, type WizardStep } from '../../stores/useWizardStore';
import { useStackStore } from '../../stores/useStackStore';
import { useRegistryStore } from '../../stores/useRegistryStore';
import { buildYAML, parseYAMLToForm, type ResourceType, type WizardFormData, type MCPServerFormData } from '../../lib/yaml-builder';
import { TemplateGrid } from './TemplateGrid';
import { YAMLPreview } from './YAMLPreview';
import { ExpertModeToggle } from './ExpertModeToggle';
import { DraftManager } from './DraftManager';
import { ReviewStep } from './steps/ReviewStep';
import { MCPServerForm } from './steps/MCPServerForm';
import { StackForm } from './steps/StackForm';
import { ResourceForm } from './steps/ResourceForm';
import { SkillImportWizard } from './steps/SkillImportWizard';

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
    glowColor: 'rgba(245, 158, 11, 0.1)',
  },
  {
    type: 'mcp-server',
    icon: Server,
    label: 'MCP Server',
    description: 'Container, external, SSH, or OpenAPI server',
    color: 'text-primary',
    glowColor: 'rgba(245, 158, 11, 0.1)',
  },
  {
    type: 'resource',
    icon: Database,
    label: 'Resource',
    description: 'Database, cache, or supporting container',
    color: 'text-secondary',
    glowColor: 'rgba(13, 148, 136, 0.1)',
  },
  {
    type: 'skill',
    icon: Puzzle,
    label: 'Skill',
    description: 'Import skills from a Git repository',
    color: 'text-secondary',
    glowColor: 'rgba(13, 148, 136, 0.1)',
  },
  {
    type: 'secret',
    icon: KeyRound,
    label: 'Secret',
    description: 'Add secrets to the vault',
    color: 'text-status-pending',
    glowColor: 'rgba(234, 179, 8, 0.1)',
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
): Record<ResourceType, number> {
  return {
    stack: 0, // Not a countable entity
    'mcp-server': mcpServers.length,
    resource: resources.length,
    skill: (skills ?? []).length,
    secret: 0, // Vault count not easily available
  };
}

interface CreationWizardProps {
  onOpenVault?: () => void;
  onDeploy?: () => void;
}

export function CreationWizard({ onOpenVault, onDeploy }: CreationWizardProps) {
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
    expertMode,
    setExpertMode,
    yamlContent,
    setYamlContent,
    yamlError,
    setYamlError,
    reset,
  } = useWizardStore();

  const mcpServers = useStackStore((s) => s.mcpServers) ?? [];
  const resources = useStackStore((s) => s.resources) ?? [];
  const skills = useRegistryStore((s) => s.skills);
  const counts = useMemo(
    () => getResourceCounts(mcpServers, resources, skills),
    [mcpServers, resources, skills],
  );

  // Map template ID to mcp-server form data so the Configure screen reflects the chosen template
  const mcpTemplateFormData: Record<string, Partial<MCPServerFormData>> = {
    'blank':          { serverType: 'container' },
    'container-http': { serverType: 'container', transport: 'http' },
    'container-stdio':{ serverType: 'container', transport: 'stdio' },
    'external-url':   { serverType: 'external',  transport: 'sse' },
    'local-process':  { serverType: 'local',     transport: 'stdio' },
    'from-source':    { serverType: 'source',    transport: 'http' },
  };

  const handleTemplateSelect = useCallback((templateId: string | null) => {
    if (selectedType === 'mcp-server' && templateId && mcpTemplateFormData[templateId]) {
      updateFormData('mcp-server', mcpTemplateFormData[templateId] as Record<string, unknown>);
    }
    setSelectedTemplate(templateId);
  }, [selectedType, setSelectedTemplate, updateFormData]); // eslint-disable-line react-hooks/exhaustive-deps

  // Skill skips template step; secret closes wizard and opens vault panel
  const handleTypeSelect = useCallback((type: ResourceType) => {
    if (type === 'secret') {
      close();
      onOpenVault?.();
      return;
    }
    setSelectedType(type);
    if (type === 'skill') {
      setStep('form');
    }
  }, [setSelectedType, setStep, close, onOpenVault]);

  const yamlDebounceRef = useRef<ReturnType<typeof setTimeout>>(null);
  const [generatedYaml, setGeneratedYaml] = useState('');

  // Generate YAML from form data (debounced)
  useEffect(() => {
    if (!selectedType || !isOpen) return;
    if (selectedType === 'skill' || selectedType === 'secret') return;

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

  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return;
      const tag = (e.target as HTMLElement)?.tagName;
      if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') {
        (e.target as HTMLElement).blur();
        return;
      }
      close();
    },
    [close],
  );

  useEffect(() => {
    if (isOpen) {
      document.addEventListener('keydown', handleKeyDown);
      return () => document.removeEventListener('keydown', handleKeyDown);
    }
  }, [isOpen, handleKeyDown]);

  const handleExpertModeToggle = (enabled: boolean) => {
    if (enabled) {
      // Switching to YAML mode: populate with current form data
      setYamlContent(generatedYaml);
      setYamlError(null);
    } else {
      // Switching back to form: parse YAML back
      if (yamlContent && selectedType) {
        const parsed = parseYAMLToForm(yamlContent, selectedType);
        if ('error' in parsed) {
          setYamlError(parsed.error);
          return; // Stay in YAML mode
        }
        updateFormData(selectedType as keyof typeof formData, parsed.data as unknown as Record<string, unknown>);
        setYamlError(null);
      }
    }
    setExpertMode(enabled);
  };

  const handleDeploy = useCallback(() => {
    close();
    onDeploy?.();
  }, [close, onDeploy]);

  const canGoNext = () => {
    switch (currentStep) {
      case 'type':
        return selectedType !== null;
      case 'template':
        return selectedTemplate !== null;
      case 'form':
        return true;
      default:
        return false;
    }
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

  const currentStepIdx = stepOrder.indexOf(currentStep);
  const showPreviewPanel = currentStep === 'form' && selectedType !== 'skill' && selectedType !== 'secret';

  if (!isOpen) return null;

  return createPortal(
    <div className="fixed inset-0 z-50 animate-fade-in-scale bg-background/80 backdrop-blur-sm flex items-center justify-center">
      {/* Backdrop */}
      <div className="absolute inset-0" onClick={close} />

      {/* Panel */}
      <div className="relative flex flex-col glass-panel-elevated rounded-xl w-full max-w-5xl mx-4 h-[85vh] shadow-lg transition-[max-width] duration-300 ease-out">
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-border/30 flex-shrink-0">
          <div className="flex items-center gap-4">
            <h2 className="text-sm font-medium text-text-primary">
              {currentStep === 'type'
                ? 'Create New Resource'
                : selectedType
                  ? `New ${resourceTypes.find((r) => r.type === selectedType)?.label}`
                  : 'Create'}
            </h2>

            {/* Step Indicator */}
            <div className="flex items-center gap-1">
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
            <span className="text-[10px] text-text-muted uppercase tracking-wider">
              {stepLabels[currentStep]}
            </span>
          </div>

          <div className="flex items-center gap-2">
            {/* Expert Mode Toggle (visible during form step) */}
            {currentStep === 'form' && selectedType !== 'skill' && selectedType !== 'secret' && (
              <ExpertModeToggle
                expertMode={expertMode}
                onToggle={handleExpertModeToggle}
              />
            )}

            {/* Draft Manager */}
            <div className="relative">
              <DraftManager />
            </div>

            {/* Reset */}
            <button
              onClick={reset}
              title="Start over"
              className="p-1.5 rounded-lg hover:bg-surface-highlight transition-colors text-text-muted hover:text-text-secondary"
            >
              <RotateCcw size={14} />
            </button>

            {/* Close */}
            <button
              onClick={close}
              className="p-1.5 rounded-lg hover:bg-surface-highlight transition-colors"
            >
              <X size={14} className="text-text-muted" />
            </button>
          </div>
        </div>

        {/* Content */}
        <div className="flex-1 min-h-0 overflow-hidden">
          {/* Skill Import Wizard — own step flow, no preview panel */}
          {selectedType === 'skill' && currentStep !== 'type' ? (
            <div className="h-full overflow-y-auto scrollbar-dark px-6 py-4">
              <SkillImportWizard />
            </div>
          ) : showPreviewPanel ? (
            <PanelGroup orientation="horizontal" className="h-full">
              {/* Form Panel */}
              <Panel defaultSize={55} minSize={40}>
                <div className="flex flex-col h-full">
                  <div className="flex-1 overflow-y-auto scrollbar-dark px-6 py-4">
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
                      setYamlContent,
                      yamlError,
                      generatedYaml,
                      counts,
                      handleDeploy,
                    )}
                  </div>
                </div>
              </Panel>

              {/* Resize Handle */}
              <PanelResizeHandle className="w-px bg-border/30 hover:bg-primary/30 transition-colors" />

              {/* Preview Panel */}
              <Panel defaultSize={45} minSize={30}>
                <YAMLPreview yaml={expertMode ? yamlContent : generatedYaml} />
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
                setYamlContent,
                yamlError,
                generatedYaml,
                counts,
                handleDeploy,
              )}
            </div>
          )}
        </div>

        {/* Footer — hidden when skill import wizard is active (it has its own footer) */}
        {!(selectedType === 'skill' && currentStep !== 'type') && (
        <div className="flex items-center justify-between px-6 py-3 border-t border-border/30 flex-shrink-0">
          <div>
            {currentStepIdx > 0 && (
              <Button variant="ghost" size="sm" onClick={goBack}>
                <ArrowLeft size={14} />
                Back
              </Button>
            )}
          </div>
          <div>
            {currentStep !== 'review' && (
              <Button
                variant="primary"
                size="sm"
                onClick={goNext}
                disabled={!canGoNext()}
              >
                Next
                <ArrowRight size={14} />
              </Button>
            )}
          </div>
        </div>
        )}
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
  counts: Record<ResourceType, number>,
  onDeploy: () => void,
) {
  switch (step) {
    case 'type':
      return <TypePicker selected={selectedType} onSelect={setSelectedType} counts={counts} />;
    case 'template':
      if (!selectedType) return null;
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
        />
      );
  }
}

// Resource Type Picker — 3x2 glass-panel grid
function TypePicker({
  selected,
  onSelect,
  counts,
}: {
  selected: ResourceType | null;
  onSelect: (type: ResourceType) => void;
  counts: Record<ResourceType, number>;
}) {
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
      <div className="grid grid-cols-3 gap-3 max-w-2xl mx-auto">
        {resourceTypes.map((rt, i) => {
          const Icon = rt.icon;
          const isSelected = selected === rt.type;
          const count = counts[rt.type];
          return (
            <button
              key={rt.type}
              onClick={() => onSelect(rt.type)}
              className={cn(
                'group relative flex flex-col items-center text-center p-5 rounded-xl border transition-all duration-200',
                'bg-white/[0.03] hover:bg-white/[0.06]',
                'animate-fade-in-up',
                isSelected
                  ? 'border-primary/50 shadow-[0_0_24px_rgba(245,158,11,0.1)]'
                  : 'border-white/[0.06] hover:border-white/[0.12]',
              )}
              style={{ animationDelay: `${i * 50}ms`, animationFillMode: 'backwards' }}
            >
              {count > 0 && (
                <div className="absolute top-2.5 right-2.5 text-[10px] font-medium text-text-muted bg-surface-highlight px-1.5 py-0.5 rounded-full">
                  {count}
                </div>
              )}
              <div
                className={cn(
                  'w-10 h-10 rounded-xl flex items-center justify-center mb-3 transition-all duration-200',
                  'bg-surface-elevated border border-border/40',
                  'group-hover:border-white/[0.12]',
                  isSelected && 'border-primary/30',
                )}
                style={
                  isSelected
                    ? { boxShadow: `0 0 20px ${rt.glowColor}` }
                    : undefined
                }
              >
                <Icon size={18} className={cn(rt.color, 'transition-transform duration-200 group-hover:scale-110')} />
              </div>
              <div className="text-sm font-medium text-text-primary mb-1">
                {rt.label}
              </div>
              <div className="text-[10px] text-text-muted leading-relaxed">
                {rt.description}
              </div>
            </button>
          );
        })}
      </div>
    </div>
  );
}

// Form renderer for each resource type
function FormRenderer({
  resourceType,
  formData,
  updateFormData,
}: {
  resourceType: ResourceType;
  formData: ReturnType<typeof useWizardStore.getState>['formData'];
  updateFormData: ReturnType<typeof useWizardStore.getState>['updateFormData'];
}) {
  // MCP Server — full form
  if (resourceType === 'mcp-server') {
    return (
      <MCPServerForm
        data={formData['mcp-server']}
        onChange={(partial) => updateFormData('mcp-server', partial as Record<string, unknown>)}
      />
    );
  }

  // Stack — full composition form
  if (resourceType === 'stack') {
    return (
      <StackForm
        data={formData['stack']}
        onChange={(partial) => updateFormData('stack', partial as Record<string, unknown>)}
      />
    );
  }

  // Resource — full form with presets
  if (resourceType === 'resource') {
    return (
      <ResourceForm
        data={formData['resource']}
        onChange={(partial) => updateFormData('resource', partial as Record<string, unknown>)}
      />
    );
  }

  // Secret — placeholder (vault panel handles secrets)
  return (
    <div className="space-y-4">
      <div className="text-xs text-text-muted uppercase tracking-wider font-medium mb-2">
        Configure {resourceType}
      </div>
      <div className="mt-6 p-4 rounded-xl bg-white/[0.02] border border-white/[0.04] text-center">
        <p className="text-xs text-text-muted">
          Use the Vault panel to manage secrets.
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
    <div className="space-y-3 h-full">
      {error && (
        <div className="flex items-center gap-2 px-3 py-2 rounded-lg bg-status-error/10 border border-status-error/20 text-status-error text-xs">
          <AlertCircle size={12} />
          <span>{error}</span>
        </div>
      )}
      <textarea
        value={yaml}
        onChange={(e) => onChange(e.target.value)}
        spellCheck={false}
        className={cn(
          'w-full h-[calc(100%-2rem)] font-mono text-[11px] leading-[1.7] resize-none',
          'bg-background/40 border rounded-xl px-4 py-3',
          'focus:outline-none focus:border-primary/50 text-text-primary',
          'placeholder:text-text-muted/50 scrollbar-dark transition-colors',
          error ? 'border-status-error/30' : 'border-border/40',
        )}
        placeholder="# Enter your YAML here..."
      />
    </div>
  );
}
