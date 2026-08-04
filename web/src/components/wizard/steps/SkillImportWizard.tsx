import { useState, useCallback } from 'react';
import {
  ArrowLeft,
  ArrowRight,
  Download,
  CheckCircle2,
  AlertTriangle,
  Loader2,
} from 'lucide-react';
import { cn } from '../../../lib/cn';
import { Button } from '../../ui/Button';
import { AddSourceStep } from './AddSourceStep';
import { BrowseStep } from './BrowseStep';
import { showToast } from '../../ui/Toast';
import {
  addSkillSource,
  fetchAgentProjectionStatus,
  fetchRegistryAgents,
  fetchRegistrySkills,
  fetchRegistryStatus,
  type SkillAuth,
} from '../../../lib/api';
import { useRegistryStore } from '../../../stores/useRegistryStore';
import type { AgentPreview, SkillPreview } from '../../../types';

type ImportStep = 'source' | 'browse' | 'configure' | 'install';

interface SkillConfig {
  name: string;
  activate: boolean;
}

export function SkillImportWizard() {
  const [step, setStep] = useState<ImportStep>('source');
  const [repoUrl, setRepoUrl] = useState('');
  const [ref, setRef] = useState('');
  const [path, setPath] = useState('');
  const [auth, setAuth] = useState<SkillAuth | undefined>(undefined);
  const [previews, setPreviews] = useState<SkillPreview[]>([]);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [configs, setConfigs] = useState<Map<string, SkillConfig>>(new Map());
  // Agents discovered alongside skills (agents/*.md). Same import path,
  // separate selection: the importer needs an explicit agent selection —
  // a skill selection alone deliberately skips agents.
  const [agentPreviews, setAgentPreviews] = useState<AgentPreview[]>([]);
  const [selectedAgents, setSelectedAgents] = useState<Set<string>>(new Set());
  // Explicit acknowledgment before flagged agents install. Skills route
  // flagged items through the configure step; agents have no per-item
  // settings, so the trust grant needs its own visible consent instead of
  // riding silently on a checked box.
  const [agentsTrustAck, setAgentsTrustAck] = useState(false);

  // Install state
  const [installing, setInstalling] = useState(false);
  const [installResult, setInstallResult] = useState<{
    imported: string[];
    skipped: { name: string; reason: string }[];
    warnings: string[];
    importedAgents: string[];
    skippedAgents: { name: string; reason: string }[];
  } | null>(null);

  const stepOrder: ImportStep[] = ['source', 'browse', 'configure', 'install'];
  const stepIdx = stepOrder.indexOf(step);

  const handlePreviewLoaded = useCallback(
    (
      skills: SkillPreview[],
      repo: string,
      refVal: string,
      pathVal: string,
      authVal: SkillAuth | undefined,
      agents: AgentPreview[],
    ) => {
      setPreviews(skills);
      setAgentPreviews(agents);
      setRepoUrl(repo);
      setRef(refVal);
      setPath(pathVal);
      setAuth(authVal);

      // Auto-select valid skills and agents that don't already exist
      const autoSelected = new Set<string>();
      const configMap = new Map<string, SkillConfig>();
      for (const sk of skills) {
        if (sk.valid && !sk.exists) {
          autoSelected.add(sk.name);
        }
        configMap.set(sk.name, { name: sk.name, activate: true });
      }
      const autoAgents = new Set<string>();
      for (const a of agents) {
        if (a.valid && !a.exists) autoAgents.add(a.name);
      }
      setSelected(autoSelected);
      setSelectedAgents(autoAgents);
      setAgentsTrustAck(false);
      setConfigs(configMap);
      setStep('browse');
    },
    [],
  );

  const handleInstall = useCallback(async () => {
    setInstalling(true);
    setInstallResult(null);

    try {
      const hasFlagged =
        previews.some((p) => selected.has(p.name) && (p.findings?.length ?? 0) > 0) ||
        agentPreviews.some((a) => selectedAgents.has(a.name) && (a.findings?.length ?? 0) > 0);

      const result = await addSkillSource({
        repo: repoUrl,
        ref: ref || undefined,
        path: path || undefined,
        trust: hasFlagged,
        noActivate: false,
        selected: [...selected],
        selectedAgents: [...selectedAgents],
        auth,
      });

      const imported = (result.imported ?? []).map((i) => i.name);
      const skipped = (result.skipped ?? []).map((s) => ({ name: s.name, reason: s.reason }));
      const importedAgents = (result.importedAgents ?? []).map((a) => a.name);
      const skippedAgents = (result.skippedAgents ?? []).map((s) => ({ name: s.name, reason: s.reason }));

      setInstallResult({
        imported,
        skipped,
        warnings: result.warnings ?? [],
        importedAgents,
        skippedAgents,
      });

      // Refresh registry (agents included, so the Agents segment shows the
      // import without a reload)
      try {
        const [regStatus, regSkills, regAgents, agentStatuses] = await Promise.all([
          fetchRegistryStatus(),
          fetchRegistrySkills(),
          fetchRegistryAgents(),
          fetchAgentProjectionStatus(),
        ]);
        useRegistryStore.getState().setStatus(regStatus);
        useRegistryStore.getState().setSkills(regSkills);
        useRegistryStore.getState().setAgents(regAgents);
        useRegistryStore.getState().setAgentStatuses(agentStatuses);
      } catch {
        // Polling will catch up
      }

      if (imported.length > 0 || importedAgents.length > 0) {
        const parts: string[] = [];
        if (imported.length > 0) parts.push(`${imported.length} skill${imported.length > 1 ? 's' : ''}`);
        if (importedAgents.length > 0) parts.push(`${importedAgents.length} agent${importedAgents.length > 1 ? 's' : ''}`);
        showToast('success', `Imported ${parts.join(', ')}`);
      }
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : 'Import failed');
    } finally {
      setInstalling(false);
      setStep('install');
    }
  }, [repoUrl, ref, path, previews, selected, agentPreviews, selectedAgents, auth]);

  const flaggedAgentsSelected = agentPreviews.some(
    (a) => selectedAgents.has(a.name) && (a.findings?.length ?? 0) > 0,
  );

  const canGoNext = () => {
    switch (step) {
      case 'source':
        return false; // handled by AddSourceStep callback
      case 'browse':
        return (
          (selected.size > 0 || selectedAgents.size > 0) &&
          (!flaggedAgentsSelected || agentsTrustAck)
        );
      case 'configure':
        return true;
      default:
        return false;
    }
  };

  const goNext = () => {
    if (step === 'browse') {
      // Skip configure if no configuration needed. Agents have no per-item
      // settings (their findings show inline in the browse section), so
      // only selected skills route through the configure step.
      const needsConfig =
        selected.size > 0 &&
        previews.some(
          (p) => selected.has(p.name) && ((p.findings?.length ?? 0) > 0 || p.exists),
        );
      if (!needsConfig) {
        handleInstall();
        return;
      }
      setStep('configure');
    } else if (step === 'configure') {
      handleInstall();
    }
  };

  const goBack = () => {
    if (stepIdx > 0) {
      setStep(stepOrder[stepIdx - 1]);
    }
  };

  return (
    <div className="flex flex-col h-full">
      {/* Step Indicator */}
      <div className="flex items-center gap-3 px-1 pb-4 mb-4 border-b border-border/20">
        {(['Add Source', 'Browse & Select', 'Configure', 'Review & Install'] as const).map((label, i) => (
          <div key={label} className="flex items-center gap-2">
            <div
              className={cn(
                'flex items-center justify-center w-5 h-5 rounded-full text-[10px] font-bold transition-all',
                i === stepIdx
                  ? 'bg-primary text-background'
                  : i < stepIdx
                    ? 'bg-primary/20 text-primary'
                    : 'bg-surface-highlight text-text-muted',
              )}
            >
              {i < stepIdx ? <CheckCircle2 size={12} /> : i + 1}
            </div>
            <span
              className={cn(
                'text-[10px] tracking-wider uppercase',
                i === stepIdx ? 'text-text-primary font-medium' : 'text-text-muted',
              )}
            >
              {label}
            </span>
            {i < 3 && <div className="w-4 h-px bg-border/30" />}
          </div>
        ))}
      </div>

      {/* Content */}
      <div className="flex-1 min-h-0 overflow-y-auto scrollbar-dark">
        {step === 'source' && (
          <AddSourceStep onPreviewLoaded={handlePreviewLoaded} />
        )}

        {step === 'browse' && (
          <>
            {/* An agents-only repo skips the skills panel entirely — a
                zero-skill BrowseStep would render an empty two-pane block
                above the actual content. */}
            {(previews.length > 0 || agentPreviews.length === 0) && (
              <BrowseStep
                previews={previews}
                selected={selected}
                onSelectionChange={setSelected}
              />
            )}
            {agentPreviews.length > 0 && (
              <AgentsBrowseSection
                previews={agentPreviews}
                selected={selectedAgents}
                onSelectionChange={setSelectedAgents}
              />
            )}
            {flaggedAgentsSelected && (
              <label className="mt-3 flex items-start gap-2 px-4 py-3 rounded-xl border border-status-pending/30 bg-status-pending/5 cursor-pointer">
                <input
                  type="checkbox"
                  checked={agentsTrustAck}
                  onChange={(e) => setAgentsTrustAck(e.target.checked)}
                  className="mt-0.5 rounded border-border/40 bg-background/60 text-primary focus:ring-primary/50"
                />
                <span className="text-xs text-text-secondary">
                  Import the flagged agents anyway. This grants the import trust to proceed
                  despite the security findings listed above.
                </span>
              </label>
            )}
          </>
        )}

        {step === 'configure' && (
          <ConfigureStep
            previews={previews.filter((p) => selected.has(p.name))}
            configs={configs}
            onConfigChange={setConfigs}
          />
        )}

        {step === 'install' && (
          <InstallStep
            installing={installing}
            result={installResult}
          />
        )}
      </div>

      {/* Footer */}
      {step !== 'install' && (
        <div className="flex items-center justify-between pt-3 mt-3 border-t border-border/20">
          <div>
            {stepIdx > 0 && (
              <Button variant="ghost" size="sm" onClick={goBack}>
                <ArrowLeft size={14} />
                Back
              </Button>
            )}
          </div>
          <div>
            {step !== 'source' && (
              <Button
                variant="primary"
                size="sm"
                onClick={goNext}
                disabled={!canGoNext() || installing}
              >
                {installing ? (
                  <>
                    <Loader2 size={14} className="animate-spin" />
                    Installing...
                  </>
                ) : step === 'configure' || (step === 'browse' && (selected.size > 0 || selectedAgents.size > 0)) ? (
                  <>
                    <Download size={14} />
                    Install{' '}
                    {[
                      selected.size > 0 ? `${selected.size} Skill${selected.size !== 1 ? 's' : ''}` : null,
                      selectedAgents.size > 0 ? `${selectedAgents.size} Agent${selectedAgents.size !== 1 ? 's' : ''}` : null,
                    ]
                      .filter(Boolean)
                      .join(', ')}
                  </>
                ) : (
                  <>
                    Next
                    <ArrowRight size={14} />
                  </>
                )}
              </Button>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

// Configure step — per-skill accordion with settings
function ConfigureStep({
  previews,
  configs,
  onConfigChange,
}: {
  previews: SkillPreview[];
  configs: Map<string, SkillConfig>;
  onConfigChange: (configs: Map<string, SkillConfig>) => void;
}) {
  const [expanded, setExpanded] = useState<string | null>(
    previews.length > 0 ? previews[0].name : null,
  );

  const updateConfig = (name: string, updates: Partial<SkillConfig>) => {
    const next = new Map(configs);
    const current = next.get(name) ?? { name, activate: true };
    next.set(name, { ...current, ...updates });
    onConfigChange(next);
  };

  return (
    <div className="space-y-2">
      <div className="mb-3">
        <h3 className="text-sm font-medium text-text-primary">Configure Skills</h3>
        <p className="text-[10px] text-text-muted mt-0.5">
          Review settings before installing
        </p>
      </div>

      {previews.map((preview) => {
        const config = configs.get(preview.name) ?? { name: preview.name, activate: true };
        const isExpanded = expanded === preview.name;
        const hasFindings = (preview.findings?.length ?? 0) > 0;

        return (
          <div
            key={preview.name}
            className="rounded-xl border border-border/20 bg-white/[0.02] overflow-hidden"
          >
            <button
              onClick={() => setExpanded(isExpanded ? null : preview.name)}
              className="w-full flex items-center gap-3 px-4 py-3 hover:bg-white/[0.02] transition-colors"
            >
              <span className="text-xs font-medium text-text-primary flex-1 text-left">
                {preview.name}
              </span>
              {hasFindings && (
                <span className="text-[9px] px-1.5 py-0.5 rounded-full bg-status-pending/10 text-status-pending flex items-center gap-1">
                  <AlertTriangle size={8} />
                  {preview.findings?.length} finding{(preview.findings?.length ?? 0) !== 1 ? 's' : ''}
                </span>
              )}
              {preview.exists && (
                <span className="text-[9px] px-1.5 py-0.5 rounded-full bg-primary/10 text-primary">
                  exists
                </span>
              )}
            </button>

            {isExpanded && (
              <div className="px-4 pb-4 pt-1 border-t border-border/10 space-y-3">
                {/* Activate toggle */}
                <label className="flex items-center gap-2 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={config.activate}
                    onChange={(e) => updateConfig(preview.name, { activate: e.target.checked })}
                    className="rounded border-border/40 bg-background/60 text-primary focus:ring-primary/50"
                  />
                  <span className="text-xs text-text-secondary">Activate after import</span>
                </label>

                {/* Security findings */}
                {hasFindings && (
                  <div className="space-y-1">
                    <span className="text-[10px] text-text-muted uppercase tracking-wider">Security Findings</span>
                    {preview.findings?.map((f, i) => (
                      <div
                        key={i}
                        className={cn(
                          'text-[10px] px-2 py-1.5 rounded-lg flex items-start gap-1.5',
                          f.severity === 'danger'
                            ? 'bg-status-error/10 text-status-error'
                            : 'bg-status-pending/10 text-status-pending',
                        )}
                      >
                        <AlertTriangle size={10} className="flex-shrink-0 mt-0.5" />
                        <span>{f.description}</span>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}

// Browse-section for agents discovered alongside skills. Names are listed
// individually on purpose: at agent scale (typically under ten) names let
// the user sanity-check provenance before committing, which the skill
// list cannot afford at three digits.
function AgentsBrowseSection({
  previews,
  selected,
  onSelectionChange,
}: {
  previews: AgentPreview[];
  selected: Set<string>;
  onSelectionChange: (next: Set<string>) => void;
}) {
  const toggle = (name: string) => {
    const next = new Set(selected);
    if (next.has(name)) next.delete(name);
    else next.add(name);
    onSelectionChange(next);
  };

  return (
    <div className="mt-4 space-y-2">
      <div>
        <h3 className="text-sm font-medium text-text-primary">Agents</h3>
        <p className="text-[10px] text-text-muted mt-0.5">
          Subagent definitions found under agents/*.md; projected to client agent directories
        </p>
      </div>
      {previews.map((a) => {
        const findingsCount = a.findings?.length ?? 0;
        return (
          <label
            key={a.name}
            className="flex items-start gap-3 px-4 py-3 rounded-xl border border-border/20 bg-white/[0.02] cursor-pointer hover:bg-white/[0.03] transition-colors"
          >
            <input
              type="checkbox"
              checked={selected.has(a.name)}
              onChange={() => toggle(a.name)}
              disabled={!a.valid}
              className="mt-0.5 rounded border-border/40 bg-background/60 text-primary focus:ring-primary/50"
            />
            <span className="flex-1 min-w-0">
              <span className="flex items-center gap-2">
                <span className="text-xs font-medium text-text-primary">{a.name}</span>
                {findingsCount > 0 && (
                  <span className="text-[9px] px-1.5 py-0.5 rounded-full bg-status-pending/10 text-status-pending flex items-center gap-1">
                    <AlertTriangle size={8} />
                    {findingsCount} finding{findingsCount !== 1 ? 's' : ''}
                  </span>
                )}
                {a.exists && (
                  <span className="text-[9px] px-1.5 py-0.5 rounded-full bg-primary/10 text-primary">
                    exists
                  </span>
                )}
              </span>
              <span className="block text-[10px] text-text-muted mt-0.5 truncate">
                {a.description}
              </span>
              {!a.valid && a.errors && a.errors.length > 0 && (
                <span className="block text-[10px] text-status-error mt-0.5">{a.errors[0]}</span>
              )}
              {/* Finding descriptions render inline: selecting a flagged
                  agent grants trust for the import, so the user must be
                  able to read what was flagged before deciding. */}
              {(a.findings ?? []).map((f, i) => (
                <span
                  key={i}
                  className={cn(
                    'mt-1 text-[10px] px-2 py-1 rounded-lg flex items-start gap-1.5',
                    f.severity === 'danger'
                      ? 'bg-status-error/10 text-status-error'
                      : 'bg-status-pending/10 text-status-pending',
                  )}
                >
                  <AlertTriangle size={10} className="flex-shrink-0 mt-0.5" />
                  <span>{f.description}</span>
                </span>
              ))}
            </span>
          </label>
        );
      })}
    </div>
  );
}

// Install step — progress and results
function InstallStep({
  installing,
  result,
}: {
  installing: boolean;
  result: {
    imported: string[];
    skipped: { name: string; reason: string }[];
    warnings: string[];
    importedAgents: string[];
    skippedAgents: { name: string; reason: string }[];
  } | null;
}) {
  if (installing) {
    return (
      <div className="flex flex-col items-center justify-center py-12">
        <div className="relative mb-4">
          <div className="w-12 h-12 rounded-full bg-primary/10 border border-primary/20 flex items-center justify-center">
            <Loader2 size={20} className="text-primary animate-spin" />
          </div>
          <div className="absolute inset-0 rounded-full border-2 border-primary/30 animate-ping" />
        </div>
        <h3 className="text-sm font-medium text-text-primary mb-1">Importing...</h3>
        <p className="text-[10px] text-text-muted">Cloning repository and validating its contents</p>
      </div>
    );
  }

  if (!result) return null;

  const totalImported = result.imported.length + result.importedAgents.length;
  const totalSkipped = result.skipped.length + result.skippedAgents.length;
  const allSucceeded = totalImported > 0 && totalSkipped === 0;

  return (
    <div className="space-y-4">
      {/* Status header */}
      <div className="flex flex-col items-center py-6">
        <div
          className={cn(
            'w-12 h-12 rounded-full flex items-center justify-center mb-3',
            allSucceeded
              ? 'bg-status-running/10 border border-status-running/20'
              : totalImported > 0
                ? 'bg-primary/10 border border-primary/20'
                : 'bg-status-error/10 border border-status-error/20',
          )}
        >
          {allSucceeded ? (
            <CheckCircle2 size={20} className="text-status-running" />
          ) : totalImported > 0 ? (
            <AlertTriangle size={20} className="text-primary" />
          ) : (
            <AlertTriangle size={20} className="text-status-error" />
          )}
        </div>
        <h3 className="text-sm font-medium text-text-primary">
          {allSucceeded ? 'Import Complete' : totalImported > 0 ? 'Partially Imported' : 'Import Failed'}
        </h3>
        <p className="text-[10px] text-text-muted mt-0.5">
          {[
            `${result.imported.length} skill${result.imported.length !== 1 ? 's' : ''}`,
            result.importedAgents.length > 0
              ? `${result.importedAgents.length} agent${result.importedAgents.length !== 1 ? 's' : ''}`
              : null,
          ]
            .filter(Boolean)
            .join(', ')}{' '}
          imported, {totalSkipped} skipped
        </p>
      </div>

      {/* Imported skills */}
      {result.imported.length > 0 && (
        <div className="space-y-1">
          <span className="text-[10px] text-text-muted uppercase tracking-wider px-1">Imported skills</span>
          {result.imported.map((name) => (
            <div
              key={name}
              className="flex items-center gap-2 px-3 py-2 rounded-lg bg-status-running/5 border border-status-running/10"
            >
              <CheckCircle2 size={12} className="text-status-running flex-shrink-0" />
              <span className="text-xs text-text-primary font-medium">{name}</span>
            </div>
          ))}
        </div>
      )}

      {/* Imported agents, named individually */}
      {result.importedAgents.length > 0 && (
        <div className="space-y-1">
          <span className="text-[10px] text-text-muted uppercase tracking-wider px-1">Imported agents</span>
          {result.importedAgents.map((name) => (
            <div
              key={name}
              className="flex items-center gap-2 px-3 py-2 rounded-lg bg-status-running/5 border border-status-running/10"
            >
              <CheckCircle2 size={12} className="text-status-running flex-shrink-0" />
              <span className="text-xs text-text-primary font-medium">{name}</span>
            </div>
          ))}
        </div>
      )}

      {/* Skipped skills and agents. Keys are kind-prefixed: skills and
          agents are separate namespaces, so one name can appear in both. */}
      {totalSkipped > 0 && (
        <div className="space-y-1">
          <span className="text-[10px] text-text-muted uppercase tracking-wider px-1">Skipped</span>
          {[
            ...result.skipped.map((s) => ({ ...s, key: `skill-${s.name}` })),
            ...result.skippedAgents.map((s) => ({ ...s, key: `agent-${s.name}` })),
          ].map((s) => (
            <div
              key={s.key}
              className="flex items-start gap-2 px-3 py-2 rounded-lg bg-status-pending/5 border border-status-pending/10"
            >
              <AlertTriangle size={12} className="text-status-pending flex-shrink-0 mt-0.5" />
              <div>
                <span className="text-xs text-text-primary font-medium">{s.name}</span>
                <p className="text-[10px] text-text-muted mt-0.5">{s.reason}</p>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Warnings */}
      {result.warnings.length > 0 && (
        <div className="space-y-1">
          <span className="text-[10px] text-text-muted uppercase tracking-wider px-1">Warnings</span>
          {result.warnings.map((w, i) => (
            <div
              key={i}
              className="text-[10px] px-3 py-2 rounded-lg bg-status-pending/5 text-status-pending"
            >
              {w}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
