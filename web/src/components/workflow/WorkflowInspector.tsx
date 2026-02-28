import { useState, useMemo } from 'react';
import { X, ChevronDown, ChevronRight, AlertCircle, Clock, RefreshCw, Filter, Layers } from 'lucide-react';
import { cn } from '../../lib/cn';
import { useWorkflowStore } from '../../stores/useWorkflowStore';
import type { WorkflowStep } from '../../types';

// Highlight {{ ... }} template expressions in argument values
function TemplateValue({ value }: { value: unknown }) {
  if (typeof value !== 'string') {
    return <span className="font-mono text-xs text-text-secondary">{JSON.stringify(value)}</span>;
  }

  const parts = value.split(/(\{\{.*?\}\})/g);
  return (
    <span className="font-mono text-xs">
      {parts.map((part, i) =>
        part.startsWith('{{') ? (
          <span key={i} className="text-primary bg-primary/10 px-1 rounded">
            {part}
          </span>
        ) : (
          <span key={i} className="text-text-secondary">{part}</span>
        ),
      )}
    </span>
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

export function WorkflowInspector() {
  const selectedStepId = useWorkflowStore((s) => s.selectedStepId);
  const setSelectedStep = useWorkflowStore((s) => s.setSelectedStep);
  const definition = useWorkflowStore((s) => s.definition);
  const execution = useWorkflowStore((s) => s.execution);
  const stepStatuses = useWorkflowStore((s) => s.stepStatuses);
  const [resultExpanded, setResultExpanded] = useState(true);

  const step = useMemo<WorkflowStep | null>(() => {
    if (!selectedStepId || !definition) return null;
    for (const level of definition.dag?.levels ?? []) {
      for (const s of level ?? []) {
        if (s.id === selectedStepId) return s;
      }
    }
    return null;
  }, [selectedStepId, definition]);

  const execStep = useMemo(() => {
    if (!selectedStepId || !execution) return null;
    return (execution.steps ?? []).find((s) => s.id === selectedStepId) ?? null;
  }, [selectedStepId, execution]);

  if (!step) return null;

  const status = stepStatuses[step.id] ?? 'pending';

  return (
    <div className="w-[300px] min-w-[280px] max-w-[500px] bg-surface border-l border-border/50 flex flex-col h-full overflow-hidden">
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3 border-b border-border/50 bg-surface-elevated/30 flex-shrink-0">
        <div className="flex items-center gap-2 min-w-0">
          <span className="font-mono text-xs text-text-primary truncate">{step.id}</span>
          <span
            className={cn(
              'text-[10px] px-1.5 py-0.5 rounded-full border',
              status === 'success' && 'text-status-running border-status-running/30 bg-status-running/10',
              status === 'failed' && 'text-status-error border-status-error/30 bg-status-error/10',
              status === 'running' && 'text-primary border-primary/30 bg-primary/10',
              status === 'skipped' && 'text-text-muted border-border/30 bg-surface-highlight',
              status === 'pending' && 'text-text-muted border-border/30',
            )}
          >
            {status}
          </span>
        </div>
        <button
          onClick={() => setSelectedStep(null)}
          className="p-1 text-text-muted hover:text-text-primary transition-colors"
        >
          <X size={14} />
        </button>
      </div>

      {/* Scrollable content */}
      <div className="flex-1 overflow-y-auto scrollbar-dark">
        {/* Tool */}
        <Section title="Tool">
          <div className="flex items-center gap-2">
            {step.tool?.startsWith('registry__') && (
              <Layers size={12} className="text-tertiary flex-shrink-0" />
            )}
            <span className="font-mono text-xs text-text-secondary break-all">{step.tool}</span>
          </div>
        </Section>

        {/* Arguments */}
        {step.args && Object.keys(step.args).length > 0 && (
          <Section title="Arguments">
            <div className="space-y-1.5">
              {Object.entries(step.args).map(([key, val]) => (
                <div key={key} className="flex items-start gap-2">
                  <span className="font-mono text-xs text-text-muted flex-shrink-0">{key}:</span>
                  <TemplateValue value={val} />
                </div>
              ))}
            </div>
          </Section>
        )}

        {/* Dependencies */}
        <Section title="Dependencies">
          {(step.dependsOn ?? []).length > 0 ? (
            <div className="flex flex-wrap gap-1.5">
              {(step.dependsOn ?? []).map((dep) => (
                <button
                  key={dep}
                  onClick={() => setSelectedStep(dep)}
                  className="font-mono text-xs text-primary bg-primary/10 px-2 py-0.5 rounded hover:bg-primary/20 transition-colors"
                >
                  {dep}
                </button>
              ))}
            </div>
          ) : (
            <span className="text-xs text-text-muted/50 italic">None</span>
          )}
        </Section>

        {/* Condition */}
        {step.condition && (
          <Section title="Condition">
            <div className="flex items-start gap-1.5">
              <Filter size={12} className="text-secondary flex-shrink-0 mt-0.5" />
              <TemplateValue value={step.condition} />
            </div>
          </Section>
        )}

        {/* Error handling */}
        <Section title="Error Handling">
          <span className="font-mono text-xs text-text-secondary">
            on_error: {step.onError ?? 'fail'}
          </span>
        </Section>

        {/* Timeout */}
        {step.timeout && (
          <Section title="Timeout">
            <div className="flex items-center gap-1.5">
              <Clock size={12} className="text-text-muted" />
              <span className="font-mono text-xs text-text-secondary">{step.timeout}</span>
            </div>
          </Section>
        )}

        {/* Retry */}
        {step.retry && (
          <Section title="Retry">
            <div className="flex items-center gap-1.5">
              <RefreshCw size={12} className="text-primary" />
              <span className="font-mono text-xs text-text-secondary">
                {step.retry.maxAttempts} attempts
                {step.retry.backoff && `, ${step.retry.backoff} backoff`}
              </span>
            </div>
          </Section>
        )}

        {/* Execution result */}
        {execStep && (
          <div className="border-t border-border/30 px-4 py-3">
            <button
              onClick={() => setResultExpanded(!resultExpanded)}
              className="flex items-center gap-1.5 text-[10px] text-text-muted uppercase tracking-wider hover:text-text-secondary transition-colors w-full"
            >
              {resultExpanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
              Result
              {execStep.durationMs != null && (
                <span className="ml-auto font-mono text-text-muted/60 normal-case">
                  {execStep.durationMs < 1000
                    ? `${execStep.durationMs}ms`
                    : `${(execStep.durationMs / 1000).toFixed(1)}s`}
                </span>
              )}
            </button>

            {resultExpanded && (
              <div className="mt-2">
                {execStep.error ? (
                  <div className="p-2.5 rounded-lg bg-status-error/5 border border-status-error/20">
                    <div className="flex items-start gap-1.5">
                      <AlertCircle size={12} className="text-status-error flex-shrink-0 mt-0.5" />
                      <pre className="font-mono text-xs text-status-error whitespace-pre-wrap break-words">
                        {execStep.error}
                      </pre>
                    </div>
                  </div>
                ) : (
                  <div className="p-2.5 rounded-lg bg-background/60 border border-border/30 max-h-48 overflow-y-auto scrollbar-dark">
                    <pre className="font-mono text-xs text-text-secondary whitespace-pre-wrap break-words">
                      {execStep.status === 'skipped'
                        ? execStep.skipReason ?? 'Skipped'
                        : 'Completed successfully'}
                    </pre>
                  </div>
                )}
                {execStep.attempts != null && execStep.attempts > 1 && (
                  <p className="text-[10px] text-text-muted mt-1.5">
                    Completed after {execStep.attempts} attempts
                  </p>
                )}
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
