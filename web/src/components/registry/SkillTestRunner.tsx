import { useState, useEffect } from 'react';
import { Play, Loader2, AlertCircle, CheckCircle } from 'lucide-react';
import { Modal } from '../ui/Modal';
import { testRegistrySkill } from '../../lib/api';
import { cn } from '../../lib/cn';
import type { Skill, ToolCallResult } from '../../types';

interface SkillTestRunnerProps {
  isOpen: boolean;
  onClose: () => void;
  skill: Skill;
}

export function SkillTestRunner({ isOpen, onClose, skill }: SkillTestRunnerProps) {
  const [inputValues, setInputValues] = useState<Record<string, string>>({});
  const [running, setRunning] = useState(false);
  const [result, setResult] = useState<ToolCallResult | null>(null);
  const [error, setError] = useState<string | null>(null);

  // Reset state when skill changes or modal opens
  useEffect(() => {
    if (isOpen) {
      const defaults: Record<string, string> = {};
      for (const param of skill.input ?? []) {
        defaults[param.name] = '';
      }
      setInputValues(defaults);
      setResult(null);
      setError(null);
    }
  }, [skill, isOpen]);

  const handleRun = async () => {
    setRunning(true);
    setResult(null);
    setError(null);
    try {
      const args: Record<string, unknown> = {};
      for (const [key, value] of Object.entries(inputValues)) {
        if (value) args[key] = value;
      }
      const res = await testRegistrySkill(skill.name, args);
      setResult(res);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Test run failed');
    } finally {
      setRunning(false);
    }
  };

  const hasRequiredInputs = (skill.input ?? [])
    .filter((p) => p.required)
    .every((p) => inputValues[p.name]?.trim());

  return (
    <Modal isOpen={isOpen} onClose={onClose} title={`Test: ${skill.name}`}>
      <div className="space-y-4">
        {/* Skill info */}
        <div className="text-xs text-text-secondary leading-relaxed">
          {skill.description}
        </div>

        {/* Tool chain preview */}
        <div className="glass-panel rounded-lg p-3">
          <span className="text-[10px] text-text-muted uppercase tracking-wider block mb-2">
            Tool Chain ({(skill.steps ?? []).length} steps)
          </span>
          <div className="flex items-center gap-1.5 flex-wrap">
            {(skill.steps ?? []).map((step, i) => (
              <span key={i} className="flex items-center gap-1">
                <span className="text-[10px] font-mono text-primary bg-primary/10 px-1.5 py-0.5 rounded">
                  {step.tool}
                </span>
                {i < (skill.steps ?? []).length - 1 && (
                  <span className="text-text-muted/40 text-[10px]">&rarr;</span>
                )}
              </span>
            ))}
          </div>
        </div>

        {/* Input parameters */}
        {(skill.input ?? []).length > 0 && (
          <div>
            <label className="text-xs text-text-secondary font-medium block mb-2">
              Input Parameters
            </label>
            <div className="space-y-2">
              {(skill.input ?? []).map((param) => (
                <div key={param.name}>
                  <div className="flex items-center gap-1.5 mb-1">
                    <span className="text-[10px] font-mono text-primary">{param.name}</span>
                    {param.required && (
                      <span className="text-status-error/70 text-[10px]">*</span>
                    )}
                    {param.description && (
                      <span className="text-[10px] text-text-muted">{param.description}</span>
                    )}
                  </div>
                  <input
                    type="text"
                    value={inputValues[param.name] ?? ''}
                    onChange={(e) =>
                      setInputValues((prev) => ({ ...prev, [param.name]: e.target.value }))
                    }
                    placeholder={`Enter ${param.name}...`}
                    className="w-full bg-surface border border-border/30 rounded-lg px-3 py-2 text-sm font-mono text-text-primary placeholder:text-text-muted focus:border-primary/50 focus:outline-none"
                  />
                </div>
              ))}
            </div>
          </div>
        )}

        {/* Run button */}
        <button
          onClick={handleRun}
          disabled={running || !hasRequiredInputs}
          className={cn(
            'w-full flex items-center justify-center gap-2 px-4 py-2.5 rounded-lg text-xs font-medium transition-all',
            'bg-primary text-background hover:bg-primary/90',
            (running || !hasRequiredInputs) && 'opacity-50 cursor-not-allowed',
          )}
        >
          {running ? (
            <>
              <Loader2 size={14} className="animate-spin" />
              Running...
            </>
          ) : (
            <>
              <Play size={14} />
              Run Test
            </>
          )}
        </button>

        {/* Error */}
        {error && (
          <div className="flex items-start gap-2 p-3 rounded-lg bg-status-error/10 border border-status-error/20">
            <AlertCircle size={14} className="text-status-error flex-shrink-0 mt-0.5" />
            <span className="text-xs text-status-error">{error}</span>
          </div>
        )}

        {/* Result */}
        {result && (
          <div
            className={cn(
              'rounded-lg border p-3',
              result.isError
                ? 'bg-status-error/5 border-status-error/20'
                : 'bg-status-running/5 border-status-running/20',
            )}
          >
            <div className="flex items-center gap-1.5 mb-2">
              {result.isError ? (
                <AlertCircle size={12} className="text-status-error" />
              ) : (
                <CheckCircle size={12} className="text-status-running" />
              )}
              <span
                className={cn(
                  'text-[10px] font-medium uppercase tracking-wider',
                  result.isError ? 'text-status-error' : 'text-status-running',
                )}
              >
                {result.isError ? 'Error' : 'Success'}
              </span>
            </div>
            <div className="space-y-1.5">
              {(result.content ?? []).map((item, i) => (
                <pre
                  key={i}
                  className="text-[11px] font-mono text-text-primary bg-background/60 p-2.5 rounded overflow-x-auto max-h-48 scrollbar-dark whitespace-pre-wrap break-words leading-relaxed"
                >
                  {item.text ?? ''}
                </pre>
              ))}
            </div>
          </div>
        )}
      </div>
    </Modal>
  );
}
