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
import { addSkillSource, fetchRegistrySkills, fetchRegistryStatus } from '../../../lib/api';
import { useRegistryStore } from '../../../stores/useRegistryStore';
import type { SkillPreview } from '../../../types';

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
  const [previews, setPreviews] = useState<SkillPreview[]>([]);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [configs, setConfigs] = useState<Map<string, SkillConfig>>(new Map());

  // Install state
  const [installing, setInstalling] = useState(false);
  const [installResult, setInstallResult] = useState<{
    imported: string[];
    skipped: { name: string; reason: string }[];
    warnings: string[];
  } | null>(null);

  const stepOrder: ImportStep[] = ['source', 'browse', 'configure', 'install'];
  const stepIdx = stepOrder.indexOf(step);

  const handlePreviewLoaded = useCallback((skills: SkillPreview[], repo: string, refVal: string, pathVal: string) => {
    setPreviews(skills);
    setRepoUrl(repo);
    setRef(refVal);
    setPath(pathVal);

    // Auto-select valid skills that don't already exist
    const autoSelected = new Set<string>();
    const configMap = new Map<string, SkillConfig>();
    for (const sk of skills) {
      if (sk.valid && !sk.exists) {
        autoSelected.add(sk.name);
      }
      configMap.set(sk.name, { name: sk.name, activate: true });
    }
    setSelected(autoSelected);
    setConfigs(configMap);
    setStep('browse');
  }, []);

  const handleInstall = useCallback(async () => {
    setInstalling(true);
    setInstallResult(null);

    try {
      const hasFlagged = previews.some(
        (p) => selected.has(p.name) && (p.findings?.length ?? 0) > 0,
      );

      const result = await addSkillSource({
        repo: repoUrl,
        ref: ref || undefined,
        path: path || undefined,
        trust: hasFlagged,
        noActivate: false,
        selected: [...selected],
      });

      const imported = (result.imported ?? []).map((i) => i.name);
      const skipped = (result.skipped ?? []).map((s) => ({ name: s.name, reason: s.reason }));

      setInstallResult({
        imported,
        skipped,
        warnings: result.warnings ?? [],
      });

      // Refresh registry
      try {
        const [regStatus, regSkills] = await Promise.all([
          fetchRegistryStatus(),
          fetchRegistrySkills(),
        ]);
        useRegistryStore.getState().setStatus(regStatus);
        useRegistryStore.getState().setSkills(regSkills);
      } catch {
        // Polling will catch up
      }

      if (imported.length > 0) {
        showToast('success', `Imported ${imported.length} skill${imported.length > 1 ? 's' : ''}`);
      }
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : 'Import failed');
    } finally {
      setInstalling(false);
      setStep('install');
    }
  }, [repoUrl, ref, path, previews, selected]);

  const canGoNext = () => {
    switch (step) {
      case 'source':
        return false; // handled by AddSourceStep callback
      case 'browse':
        return selected.size > 0;
      case 'configure':
        return true;
      default:
        return false;
    }
  };

  const goNext = () => {
    if (step === 'browse') {
      // Skip configure if no configuration needed
      if (selected.size > 0) {
        const needsConfig = previews.some(
          (p) => selected.has(p.name) && ((p.findings?.length ?? 0) > 0 || p.exists),
        );
        if (!needsConfig) {
          handleInstall();
          return;
        }
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
          <BrowseStep
            previews={previews}
            selected={selected}
            onSelectionChange={setSelected}
          />
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
                ) : step === 'configure' || (step === 'browse' && selected.size > 0) ? (
                  <>
                    <Download size={14} />
                    Install {selected.size} Skill{selected.size !== 1 ? 's' : ''}
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
        <h3 className="text-sm font-medium text-text-primary mb-1">Importing skills...</h3>
        <p className="text-[10px] text-text-muted">Cloning repository and validating skills</p>
      </div>
    );
  }

  if (!result) return null;

  const allSucceeded = result.imported.length > 0 && result.skipped.length === 0;

  return (
    <div className="space-y-4">
      {/* Status header */}
      <div className="flex flex-col items-center py-6">
        <div
          className={cn(
            'w-12 h-12 rounded-full flex items-center justify-center mb-3',
            allSucceeded
              ? 'bg-status-running/10 border border-status-running/20'
              : result.imported.length > 0
                ? 'bg-primary/10 border border-primary/20'
                : 'bg-status-error/10 border border-status-error/20',
          )}
        >
          {allSucceeded ? (
            <CheckCircle2 size={20} className="text-status-running" />
          ) : result.imported.length > 0 ? (
            <AlertTriangle size={20} className="text-primary" />
          ) : (
            <AlertTriangle size={20} className="text-status-error" />
          )}
        </div>
        <h3 className="text-sm font-medium text-text-primary">
          {allSucceeded ? 'Import Complete' : result.imported.length > 0 ? 'Partially Imported' : 'Import Failed'}
        </h3>
        <p className="text-[10px] text-text-muted mt-0.5">
          {result.imported.length} imported, {result.skipped.length} skipped
        </p>
      </div>

      {/* Imported skills */}
      {result.imported.length > 0 && (
        <div className="space-y-1">
          <span className="text-[10px] text-text-muted uppercase tracking-wider px-1">Imported</span>
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

      {/* Skipped skills */}
      {result.skipped.length > 0 && (
        <div className="space-y-1">
          <span className="text-[10px] text-text-muted uppercase tracking-wider px-1">Skipped</span>
          {result.skipped.map((s) => (
            <div
              key={s.name}
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
