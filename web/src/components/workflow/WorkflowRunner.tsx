import { useState, useCallback, useRef, useMemo } from 'react';
import {
  Play,
  CheckCircle2,
  AlertCircle,
  AlertTriangle,
  ChevronDown,
  ChevronRight,
  Crosshair,
  Loader2,
  Clock,
} from 'lucide-react';
import { cn } from '../../lib/cn';
import { ZoomControls } from '../log/ZoomControls';
import { useWorkflowStore } from '../../stores/useWorkflowStore';
import { useWorkflowFontSize } from '../../hooks/useWorkflowFontSize';
import type { SkillInput, StepExecutionResult } from '../../types';

// --- Input form field ---

function InputField({
  name,
  input,
  value,
  onChange,
}: {
  name: string;
  input: SkillInput;
  value: unknown;
  onChange: (name: string, value: unknown) => void;
}) {
  const isRequired = input.required ?? false;

  if ((input.enum ?? []).length > 0) {
    return (
      <div>
        <label className="text-xs text-text-muted block mb-1">
          {name}
          {isRequired && <span className="text-primary ml-0.5">*</span>}
        </label>
        <select
          value={String(value ?? '')}
          onChange={(e) => onChange(name, e.target.value)}
          className="w-full bg-background/60 border border-border/40 rounded-lg px-3 py-1.5 text-xs font-mono text-text-primary focus:outline-none focus:border-primary/50 transition-colors"
        >
          <option value="">Select...</option>
          {(input.enum ?? []).map((opt) => (
            <option key={opt} value={opt}>{opt}</option>
          ))}
        </select>
      </div>
    );
  }

  if (input.type === 'boolean') {
    return (
      <div className="flex items-center gap-2">
        <label className="text-xs text-text-muted">
          {name}
          {isRequired && <span className="text-primary ml-0.5">*</span>}
        </label>
        <button
          onClick={() => onChange(name, !value)}
          className={cn(
            'w-8 h-4 rounded-full transition-all duration-200 relative',
            value ? 'bg-primary' : 'bg-border',
          )}
        >
          <div
            className={cn(
              'w-3 h-3 rounded-full bg-white absolute top-0.5 transition-all duration-200',
              value ? 'left-4' : 'left-0.5',
            )}
          />
        </button>
      </div>
    );
  }

  if (input.type === 'number') {
    return (
      <div>
        <label className="text-xs text-text-muted block mb-1">
          {name}
          {isRequired && <span className="text-primary ml-0.5">*</span>}
        </label>
        <input
          type="number"
          value={value != null ? String(value) : ''}
          onChange={(e) => onChange(name, e.target.value ? Number(e.target.value) : undefined)}
          placeholder={input.default != null ? String(input.default) : ''}
          className="w-full bg-background/60 border border-border/40 rounded-lg px-3 py-1.5 text-xs font-mono text-text-primary placeholder:text-text-muted/40 focus:outline-none focus:border-primary/50 transition-colors"
        />
      </div>
    );
  }

  if (input.type === 'object' || input.type === 'array') {
    return (
      <div>
        <label className="text-xs text-text-muted block mb-1">
          {name}
          {isRequired && <span className="text-primary ml-0.5">*</span>}
          <span className="text-text-muted/40 ml-1">({input.type})</span>
        </label>
        <textarea
          value={typeof value === 'string' ? value : value != null ? JSON.stringify(value, null, 2) : ''}
          onChange={(e) => {
            try {
              onChange(name, JSON.parse(e.target.value));
            } catch {
              onChange(name, e.target.value);
            }
          }}
          rows={3}
          placeholder={input.type === 'object' ? '{}' : '[]'}
          className="w-full bg-background/60 border border-border/40 rounded-lg px-3 py-1.5 text-xs font-mono text-text-primary placeholder:text-text-muted/40 focus:outline-none focus:border-primary/50 resize-y transition-colors"
        />
      </div>
    );
  }

  // Default: string input
  return (
    <div>
      <label className="text-xs text-text-muted block mb-1">
        {name}
        {isRequired && <span className="text-primary ml-0.5">*</span>}
        {input.description && (
          <span className="text-text-muted/40 ml-1 normal-case">{input.description}</span>
        )}
      </label>
      <input
        type="text"
        value={String(value ?? '')}
        onChange={(e) => onChange(name, e.target.value)}
        placeholder={input.default != null ? String(input.default) : ''}
        className="w-full bg-background/60 border border-border/40 rounded-lg px-3 py-1.5 text-xs font-mono text-text-primary placeholder:text-text-muted/40 focus:outline-none focus:border-primary/50 transition-colors"
      />
    </div>
  );
}

// --- Step result card ---

function StepResultCard({ step }: { step: StepExecutionResult }) {
  const [expanded, setExpanded] = useState(step.status === 'failed');

  const statusDot: Record<string, string> = {
    success: 'bg-status-running',
    failed: 'bg-status-error',
    skipped: 'bg-text-muted/40',
    running: 'bg-primary animate-pulse',
    pending: 'bg-text-muted',
  };

  return (
    <div
      className={cn(
        'bg-surface-elevated border rounded-lg transition-all duration-200',
        step.status === 'failed' ? 'border-status-error/30 bg-status-error/5' : 'border-border/40',
        step.status === 'skipped' && 'opacity-50',
      )}
    >
      <button
        onClick={() => setExpanded(!expanded)}
        className="w-full flex items-center gap-2 px-3 py-2 text-left"
      >
        <div className={cn('w-1.5 h-1.5 rounded-full flex-shrink-0', statusDot[step.status])} />
        <span className="font-mono text-xs text-text-primary truncate flex-1">{step.id}</span>
        {step.durationMs != null && (
          <span className="font-mono text-[10px] text-text-muted flex-shrink-0">
            {step.durationMs < 1000 ? `${step.durationMs}ms` : `${(step.durationMs / 1000).toFixed(1)}s`}
          </span>
        )}
        <span
          className={cn(
            'text-[10px] px-1.5 py-0.5 rounded-full border flex-shrink-0',
            step.status === 'success' && 'text-status-running border-status-running/30',
            step.status === 'failed' && 'text-status-error border-status-error/30',
            step.status === 'skipped' && 'text-text-muted border-border/30',
          )}
        >
          {step.status}
        </span>
        {expanded ? <ChevronDown size={12} className="text-text-muted" /> : <ChevronRight size={12} className="text-text-muted" />}
      </button>

      {expanded && (
        <div className="px-3 pb-2.5">
          {step.error && (
            <pre className="font-mono workflow-text text-status-error whitespace-pre-wrap break-words p-2 rounded bg-background/60 border border-border/20">
              {step.error}
            </pre>
          )}
          {step.skipReason && (
            <p className="font-mono workflow-text text-text-muted italic">{step.skipReason}</p>
          )}
          {step.attempts != null && step.attempts > 1 && (
            <p className="text-[10px] text-text-muted mt-1">Completed after {step.attempts} attempts</p>
          )}
        </div>
      )}
    </div>
  );
}

// --- Main WorkflowRunner component ---

export function WorkflowRunner() {
  const definition = useWorkflowStore((s) => s.definition);
  const skillName = useWorkflowStore((s) => s.skillName);
  const executing = useWorkflowStore((s) => s.executing);
  const execution = useWorkflowStore((s) => s.execution);
  const runWorkflow = useWorkflowStore((s) => s.executeWorkflow);
  const validateInputs = useWorkflowStore((s) => s.validateWorkflowInputs);
  const validation = useWorkflowStore((s) => s.validation);
  const validating = useWorkflowStore((s) => s.validating);
  const followMode = useWorkflowStore((s) => s.followMode);
  const toggleFollowMode = useWorkflowStore((s) => s.toggleFollowMode);

  const [inputValues, setInputValues] = useState<Record<string, unknown>>({});
  const [resultsExpanded, setResultsExpanded] = useState(true);
  const resultsRef = useRef<HTMLDivElement>(null);
  const { fontSize, zoomIn, zoomOut, resetZoom, isMin, isMax, isDefault } = useWorkflowFontSize(resultsRef);

  // Build initial values from defaults
  const inputs = useMemo(() => definition?.inputs ?? {}, [definition]);

  const handleInputChange = useCallback((name: string, value: unknown) => {
    setInputValues((prev) => ({ ...prev, [name]: value }));
  }, []);

  // Merge defaults with user values
  const buildArgs = useCallback((): Record<string, unknown> => {
    const args: Record<string, unknown> = {};
    for (const [name, input] of Object.entries(inputs)) {
      const val = inputValues[name];
      if (val !== undefined && val !== '') {
        args[name] = val;
      } else if (input.default != null) {
        args[name] = input.default;
      }
    }
    return args;
  }, [inputs, inputValues]);

  const handleValidate = useCallback(async () => {
    if (!skillName) return;
    await validateInputs(skillName, buildArgs());
  }, [skillName, validateInputs, buildArgs]);

  const handleRun = useCallback(async () => {
    if (!skillName || executing) return;
    await runWorkflow(skillName, buildArgs());
    setResultsExpanded(true);
  }, [skillName, executing, runWorkflow, buildArgs]);

  if (!definition) return null;

  const inputEntries = Object.entries(inputs);
  const steps = execution?.steps ?? [];

  return (
    <div className="border-t border-border/50 bg-surface/50 flex flex-col min-h-0">
      {/* Toolbar */}
      <div className="flex items-center justify-between px-4 py-2 border-b border-border/30 flex-shrink-0">
        <div className="flex items-center gap-2">
          <Play size={14} className="text-primary" />
          <span className="text-xs font-medium text-text-primary">Test Workflow</span>
        </div>
        <div className="flex items-center gap-2">
          <ZoomControls
            fontSize={fontSize}
            onZoomIn={zoomIn}
            onZoomOut={zoomOut}
            onReset={resetZoom}
            isMin={isMin}
            isMax={isMax}
            isDefault={isDefault}
          />

          <button
            onClick={toggleFollowMode}
            title="Auto-pan to running steps"
            className={cn(
              'p-1.5 rounded-lg text-xs transition-all duration-200 flex items-center gap-1',
              followMode
                ? 'bg-primary/20 text-primary border border-primary/30'
                : 'text-text-muted hover:text-text-secondary border border-transparent',
            )}
          >
            <Crosshair size={12} />
          </button>

          <button
            onClick={handleValidate}
            disabled={validating}
            className="btn-secondary text-xs py-1 px-3 flex items-center gap-1"
          >
            {validating ? <Loader2 size={12} className="animate-spin" /> : <CheckCircle2 size={12} />}
            Validate
          </button>

          <button
            onClick={handleRun}
            disabled={executing}
            className="btn-primary text-xs py-1 px-3 flex items-center gap-1"
          >
            {executing ? <Loader2 size={12} className="animate-spin" /> : <Play size={12} />}
            {executing ? 'Running...' : 'Run'}
          </button>
        </div>
      </div>

      {/* Scrollable content */}
      <div ref={resultsRef} className="flex-1 overflow-y-auto scrollbar-dark min-h-0" style={{ '--workflow-font-size': `${fontSize}px` } as React.CSSProperties}>
        {/* Input fields */}
        {inputEntries.length > 0 && (
          <div className="px-4 py-3 border-b border-border/20">
            <h4 className="text-[10px] text-text-muted uppercase tracking-wider mb-2">Inputs</h4>
            <div className="grid grid-cols-2 gap-3">
              {inputEntries.map(([name, input]) => (
                <InputField
                  key={name}
                  name={name}
                  input={input}
                  value={inputValues[name] ?? input.default ?? ''}
                  onChange={handleInputChange}
                />
              ))}
            </div>
          </div>
        )}

        {/* Validation results */}
        {validation && (
          <div className="px-4 py-2 border-b border-border/20">
            {validation.valid ? (
              <div className="flex items-center gap-1.5 text-xs text-status-running">
                <CheckCircle2 size={12} />
                Validation passed
              </div>
            ) : (
              <div className="space-y-1">
                {(validation.errors ?? []).map((err, i) => (
                  <div key={i} className="flex items-start gap-1.5 text-xs text-status-error">
                    <AlertCircle size={12} className="flex-shrink-0 mt-0.5" />
                    {err}
                  </div>
                ))}
              </div>
            )}
            {(validation.warnings ?? []).length > 0 && (
              <div className="mt-1 space-y-1">
                {(validation.warnings ?? []).map((warn, i) => (
                  <div key={i} className="flex items-start gap-1.5 text-xs text-status-pending">
                    <AlertTriangle size={12} className="flex-shrink-0 mt-0.5" />
                    {warn}
                  </div>
                ))}
              </div>
            )}
          </div>
        )}

        {/* Execution results */}
        {steps.length > 0 && (
          <div className="px-4 py-3">
            <button
              onClick={() => setResultsExpanded(!resultsExpanded)}
              className="flex items-center gap-1.5 text-[10px] text-text-muted uppercase tracking-wider hover:text-text-secondary transition-colors w-full mb-2"
            >
              {resultsExpanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
              Execution Results ({steps.length} steps)
              {execution?.durationMs != null && (
                <span className="ml-auto font-mono text-text-muted/60 normal-case flex items-center gap-1">
                  <Clock size={10} />
                  {execution.durationMs < 1000
                    ? `${execution.durationMs}ms`
                    : `${(execution.durationMs / 1000).toFixed(1)}s`}
                </span>
              )}
            </button>

            {resultsExpanded && (
              <div className="space-y-2">
                {steps.map((step) => (
                  <StepResultCard key={step.id} step={step} />
                ))}
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
