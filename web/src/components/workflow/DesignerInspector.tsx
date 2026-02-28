import { useState, useMemo, useCallback } from 'react';
import {
  X,
  ChevronDown,
  ChevronRight,
  Variable,
} from 'lucide-react';
import { cn } from '../../lib/cn';
import type { WorkflowStep, SkillInput, Tool } from '../../types';

interface DesignerInspectorProps {
  step: WorkflowStep;
  allSteps: WorkflowStep[];
  inputs: Record<string, SkillInput>;
  tools: Tool[];
  onChange: (step: WorkflowStep) => void;
  onClose: () => void;
}

// Variable picker dropdown
function VariablePicker({
  inputs,
  upstreamSteps,
  onInsert,
  onClose,
}: {
  inputs: Record<string, SkillInput>;
  upstreamSteps: string[];
  onInsert: (expr: string) => void;
  onClose: () => void;
}) {
  return (
    <div className="absolute z-50 top-full left-0 mt-1 w-64 max-h-48 overflow-y-auto bg-surface-elevated border border-border/50 rounded-lg shadow-lg scrollbar-dark">
      <div className="px-3 py-1.5 border-b border-border/20">
        <span className="text-[10px] text-text-muted uppercase tracking-wider">Insert Variable</span>
      </div>
      {/* Inputs */}
      {Object.keys(inputs).length > 0 && (
        <div className="border-b border-border/20">
          <div className="px-3 py-1 text-[10px] text-primary uppercase tracking-wider">Inputs</div>
          {Object.keys(inputs).map((name) => (
            <button
              key={name}
              onClick={() => { onInsert(`{{ inputs.${name} }}`); onClose(); }}
              className="w-full text-left px-3 py-1 text-xs font-mono text-text-secondary hover:bg-surface-highlight hover:text-text-primary transition-colors"
            >
              {'{{ inputs.'}{name}{' }}'}
            </button>
          ))}
        </div>
      )}
      {/* Upstream steps */}
      {(upstreamSteps ?? []).map((stepId) => (
        <div key={stepId} className="border-b border-border/20 last:border-0">
          <div className="px-3 py-1 text-[10px] text-text-muted uppercase tracking-wider">Step: {stepId}</div>
          {['.result', '.is_error', '.json.'].map((suffix) => (
            <button
              key={suffix}
              onClick={() => {
                const expr = suffix === '.json.' ? `{{ steps.${stepId}.json. }}` : `{{ steps.${stepId}${suffix} }}`;
                onInsert(expr);
                onClose();
              }}
              className="w-full text-left px-3 py-1 text-xs font-mono text-text-secondary hover:bg-surface-highlight hover:text-text-primary transition-colors"
            >
              {'{{ steps.'}{stepId}{suffix}{suffix === '.json.' ? ' }}' : ' }}'}
            </button>
          ))}
        </div>
      ))}
      {Object.keys(inputs).length === 0 && upstreamSteps.length === 0 && (
        <p className="px-3 py-2 text-xs text-text-muted/40 italic">No variables available</p>
      )}
    </div>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="border-t border-border/30 px-4 py-3">
      <h4 className="text-[10px] text-text-muted uppercase tracking-wider mb-2">{title}</h4>
      {children}
    </div>
  );
}

function ArgField({
  argKey,
  value,
  onChange,
  inputs,
  upstreamSteps,
}: {
  argKey: string;
  value: unknown;
  onChange: (key: string, value: unknown) => void;
  inputs: Record<string, SkillInput>;
  upstreamSteps: string[];
}) {
  const [showPicker, setShowPicker] = useState(false);
  const strValue = typeof value === 'string' ? value : value != null ? JSON.stringify(value) : '';

  return (
    <div className="flex items-start gap-1.5 relative">
      <span className="font-mono text-xs text-text-muted flex-shrink-0 mt-1.5">{argKey}:</span>
      <div className="flex-1 relative">
        <input
          value={strValue}
          onChange={(e) => onChange(argKey, e.target.value)}
          className="w-full bg-background/60 border border-border/40 rounded px-2 py-1 text-xs font-mono text-text-primary focus:outline-none focus:border-primary/50 transition-colors pr-6"
        />
        <button
          onClick={() => setShowPicker(!showPicker)}
          className="absolute right-1 top-1/2 -translate-y-1/2 p-0.5 text-text-muted/40 hover:text-primary transition-colors"
          title="Insert variable"
        >
          <Variable size={10} />
        </button>
        {showPicker && (
          <VariablePicker
            inputs={inputs}
            upstreamSteps={upstreamSteps}
            onInsert={(expr) => onChange(argKey, strValue + expr)}
            onClose={() => setShowPicker(false)}
          />
        )}
      </div>
    </div>
  );
}

export function DesignerInspector({
  step,
  allSteps,
  inputs,
  tools,
  onChange,
  onClose,
}: DesignerInspectorProps) {
  const [retryExpanded, setRetryExpanded] = useState(!!step.retry);

  // Find upstream steps (steps that could have completed before this one)
  const upstreamSteps = useMemo(() => {
    const deps = new Set<string>();
    const queue = [...(step.dependsOn ?? [])];
    while (queue.length > 0) {
      const id = queue.shift()!;
      if (deps.has(id)) continue;
      deps.add(id);
      const depStep = (allSteps ?? []).find((s) => s.id === id);
      if (depStep) {
        for (const d of depStep.dependsOn ?? []) {
          queue.push(d);
        }
      }
    }
    return Array.from(deps);
  }, [step.dependsOn, allSteps]);

  const updateField = useCallback(
    <K extends keyof WorkflowStep>(field: K, value: WorkflowStep[K]) => {
      onChange({ ...step, [field]: value });
    },
    [step, onChange],
  );

  const updateArg = useCallback(
    (key: string, value: unknown) => {
      const args = { ...(step.args ?? {}), [key]: value };
      onChange({ ...step, args });
    },
    [step, onChange],
  );

  const addArg = useCallback(() => {
    const args = { ...(step.args ?? {}), [`arg-${Object.keys(step.args ?? {}).length + 1}`]: '' };
    onChange({ ...step, args });
  }, [step, onChange]);

  const removeArg = useCallback(
    (key: string) => {
      const args = { ...(step.args ?? {}) };
      delete args[key];
      onChange({ ...step, args });
    },
    [step, onChange],
  );

  return (
    <div className="w-[320px] min-w-[280px] max-w-[500px] bg-surface border-l border-border/50 flex flex-col h-full overflow-hidden flex-shrink-0">
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3 border-b border-border/50 bg-surface-elevated/30 flex-shrink-0">
        <span className="text-[10px] text-text-muted uppercase tracking-wider">Step Properties</span>
        <button onClick={onClose} className="p-1 text-text-muted hover:text-text-primary transition-colors">
          <X size={14} />
        </button>
      </div>

      {/* Scrollable content */}
      <div className="flex-1 overflow-y-auto scrollbar-dark">
        {/* Step ID */}
        <Section title="Step ID">
          <input
            value={step.id}
            onChange={(e) => updateField('id', e.target.value.replace(/[^a-z0-9-]/g, ''))}
            className="w-full bg-background/60 border border-border/40 rounded-lg px-3 py-1.5 text-xs font-mono text-text-primary focus:outline-none focus:border-primary/50 transition-colors"
          />
        </Section>

        {/* Tool */}
        <Section title="Tool">
          <select
            value={step.tool}
            onChange={(e) => updateField('tool', e.target.value)}
            className="w-full bg-background/60 border border-border/40 rounded-lg px-3 py-1.5 text-xs font-mono text-text-primary focus:outline-none focus:border-primary/50 transition-colors"
          >
            <option value="">Select tool...</option>
            {(tools ?? []).map((t) => (
              <option key={t.name} value={t.name}>{t.name}</option>
            ))}
          </select>
        </Section>

        {/* Arguments */}
        <Section title="Arguments">
          <div className="space-y-2">
            {Object.entries(step.args ?? {}).map(([key, val]) => (
              <div key={key} className="flex items-start gap-1">
                <ArgField
                  argKey={key}
                  value={val}
                  onChange={updateArg}
                  inputs={inputs}
                  upstreamSteps={upstreamSteps}
                />
                <button
                  onClick={() => removeArg(key)}
                  className="p-0.5 text-text-muted/30 hover:text-status-error mt-1 transition-colors"
                >
                  <X size={10} />
                </button>
              </div>
            ))}
            <button
              onClick={addArg}
              className="text-xs text-primary hover:text-primary/80 flex items-center gap-0.5 transition-colors"
            >
              + Add argument
            </button>
          </div>
        </Section>

        {/* Condition */}
        <Section title="Condition">
          <div className="relative">
            <input
              value={step.condition ?? ''}
              onChange={(e) => updateField('condition', e.target.value || undefined)}
              placeholder="e.g. {{ steps.check.json.valid == true }}"
              className="w-full bg-background/60 border border-border/40 rounded-lg px-3 py-1.5 text-xs font-mono text-text-primary placeholder:text-text-muted/30 focus:outline-none focus:border-primary/50 transition-colors"
            />
          </div>
        </Section>

        {/* Error handling */}
        <Section title="Error Handling">
          <select
            value={step.onError ?? 'fail'}
            onChange={(e) => updateField('onError', e.target.value === 'fail' ? undefined : e.target.value)}
            className="w-full bg-background/60 border border-border/40 rounded-lg px-3 py-1.5 text-xs font-mono text-text-primary focus:outline-none focus:border-primary/50 transition-colors"
          >
            <option value="fail">fail</option>
            <option value="skip">skip</option>
            <option value="continue">continue</option>
          </select>
        </Section>

        {/* Timeout */}
        <Section title="Timeout">
          <input
            value={step.timeout ?? ''}
            onChange={(e) => updateField('timeout', e.target.value || undefined)}
            placeholder="e.g. 30s, 1m, 500ms"
            className="w-full bg-background/60 border border-border/40 rounded-lg px-3 py-1.5 text-xs font-mono text-text-primary placeholder:text-text-muted/30 focus:outline-none focus:border-primary/50 transition-colors"
          />
        </Section>

        {/* Retry */}
        <div className="border-t border-border/30 px-4 py-3">
          <button
            onClick={() => {
              if (step.retry) {
                updateField('retry', undefined);
                setRetryExpanded(false);
              } else {
                updateField('retry', { maxAttempts: 3, backoff: '1s' });
                setRetryExpanded(true);
              }
            }}
            className="flex items-center gap-1.5 text-[10px] text-text-muted uppercase tracking-wider hover:text-text-secondary transition-colors w-full"
          >
            {retryExpanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
            Retry
            <span className={cn('ml-auto text-[9px] px-1.5 rounded', step.retry ? 'text-primary bg-primary/10' : 'text-text-muted/40')}>
              {step.retry ? 'on' : 'off'}
            </span>
          </button>

          {retryExpanded && step.retry && (
            <div className="mt-2 space-y-2">
              <div className="flex items-center gap-2">
                <span className="text-xs text-text-muted">Attempts:</span>
                <input
                  type="number"
                  min={1}
                  max={10}
                  value={step.retry.maxAttempts}
                  onChange={(e) => updateField('retry', { ...step.retry!, maxAttempts: Number(e.target.value) || 1 })}
                  className="w-16 bg-background/60 border border-border/40 rounded px-2 py-0.5 text-xs font-mono text-text-primary focus:outline-none focus:border-primary/50"
                />
              </div>
              <div className="flex items-center gap-2">
                <span className="text-xs text-text-muted">Backoff:</span>
                <input
                  value={step.retry.backoff ?? ''}
                  onChange={(e) => updateField('retry', { ...step.retry!, backoff: e.target.value || undefined })}
                  placeholder="1s"
                  className="w-20 bg-background/60 border border-border/40 rounded px-2 py-0.5 text-xs font-mono text-text-primary placeholder:text-text-muted/30 focus:outline-none focus:border-primary/50"
                />
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
