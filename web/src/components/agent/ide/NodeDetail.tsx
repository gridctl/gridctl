import { useMemo } from 'react';
import { editorURL, type AgentNode } from '../../../lib/agent-api';
import { styleFor } from './kind-style';
import { TracePill } from './TracePill';
import { ResumeButton } from './ResumeButton';
import type { RunTrace, NodeTrace } from './useRunTrace';
import { cn } from '../../../lib/cn';

interface NodeDetailProps {
  node: AgentNode | null;
  skillDir: string;
  trace: NodeTrace | undefined;
  runID: string | null;
  runTrace: RunTrace;
  onClose: () => void;
}

/**
 * NodeDetail is the right-pane drawer the IDE opens on node
 * selection. Shows the source location, kind metadata, and (when a
 * run is active) a tab strip with prompt / response / structured
 * output / OTel trace ID.
 *
 * The drawer is information-dense by design — production agent
 * developers spend their time bouncing between code and trace data;
 * the panel collapses both into one click-to-jump frame.
 */
export function NodeDetail({
  node,
  skillDir,
  trace,
  runID,
  runTrace,
  onClose,
}: NodeDetailProps) {
  const sliceEvents = useMemo(() => {
    if (!node) return [];
    return runTrace.events.filter((ev) => {
      const p = (ev.payload ?? {}) as Record<string, unknown>;
      return (p['node_id'] === node.id || p['node_name'] === node.id);
    });
  }, [runTrace.events, node]);

  if (!node) {
    return (
      <aside className="h-full bg-surface/40 border-l border-border-subtle p-6 flex flex-col">
        <h3 className="font-sans text-text-muted text-xs uppercase tracking-[0.3em] mb-3">
          Inspector
        </h3>
        <div className="flex-1 flex items-center justify-center text-center text-text-muted text-sm">
          <div>
            <div className="font-sans text-text-muted/40 text-[10px] uppercase tracking-[0.4em] mb-2">
              awaiting selection
            </div>
            <p className="text-text-muted">
              Select a node to see its source location, trace data, and resume control.
            </p>
          </div>
        </div>
      </aside>
    );
  }

  const style = styleFor(node.kind);
  return (
    <aside className="h-full bg-surface/40 border-l border-border-subtle flex flex-col overflow-hidden">
      <header className="px-6 pt-5 pb-4 border-b border-border-subtle flex items-start gap-3">
        <span
          className={cn(
            'inline-flex items-center gap-1 px-2 py-0.5 rounded text-[10px]',
            'uppercase tracking-[0.18em] font-medium font-mono',
            style.badgeBg,
            style.badgeText,
            'border',
            style.border,
          )}
        >
          <span className="text-sm leading-none">{style.glyph}</span>
          {style.label}
        </span>
        <div className="flex-1 min-w-0">
          <div className="font-mono text-sm text-text-primary truncate">{node.label}</div>
          <a
            href={editorURL(skillDir, node.file, node.line)}
            className="text-xs text-text-muted hover:text-primary transition-colors font-mono"
          >
            {node.file}:{node.line}
          </a>
        </div>
        <button
          type="button"
          onClick={onClose}
          className="text-text-muted hover:text-text-primary text-lg leading-none"
          aria-label="Close inspector"
        >
          ×
        </button>
      </header>

      <div className="flex-1 overflow-y-auto px-6 py-5 space-y-6">
        <Section label="Trace">
          <div className="flex items-center gap-3 flex-wrap">
            <TracePill trace={trace} />
            {trace?.spanID && (
              <span className="font-mono text-[11px] text-text-muted">
                otel={trace.spanID.slice(0, 12)}…
              </span>
            )}
            {trace?.model && (
              <span className="font-mono text-[11px] text-text-muted">
                model={trace.model}
              </span>
            )}
            {trace?.promptTokens != null && (
              <span className="font-mono text-[11px] text-text-muted">
                in={trace.promptTokens}
              </span>
            )}
            {trace?.outputTokens != null && (
              <span className="font-mono text-[11px] text-text-muted">
                out={trace.outputTokens}
              </span>
            )}
          </div>
          {trace?.errorMessage && (
            <pre className="mt-3 p-3 rounded bg-status-error/5 border border-status-error/20 text-xs text-status-error whitespace-pre-wrap">
              {trace.errorMessage}
            </pre>
          )}
        </Section>

        <Section label="Source">
          <a
            href={editorURL(skillDir, node.file, node.line)}
            className={cn(
              'block px-3 py-2 rounded border border-border bg-surface',
              'font-mono text-xs text-text-secondary',
              'hover:border-primary/40 hover:text-text-primary transition-colors',
            )}
          >
            <div className="text-[10px] uppercase tracking-[0.2em] text-text-muted/70 mb-1">
              jump to source
            </div>
            <div className="truncate">{node.file}:{node.line}</div>
          </a>
        </Section>

        {sliceEvents.length > 0 && (
          <Section label={`Events (${sliceEvents.length})`}>
            <ul className="space-y-1 font-mono text-[11px]">
              {sliceEvents.slice(-10).map((ev) => (
                <li
                  key={`${ev.seq}-${ev.type}`}
                  className="flex items-baseline gap-3 text-text-muted"
                >
                  <span className="tabular-nums text-text-muted/60 w-12 text-right">
                    #{ev.seq}
                  </span>
                  <span className={cn('px-1.5 py-px rounded', eventTypeColor(ev.type))}>
                    {ev.type}
                  </span>
                </li>
              ))}
            </ul>
          </Section>
        )}

        {runID && trace?.status === 'ok' && (
          <Section label="Resume">
            <ResumeButton runID={runID} fromStep={node.id} />
          </Section>
        )}
      </div>
    </aside>
  );
}

function Section({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <section>
      <h4 className="font-sans text-text-muted text-[10px] uppercase tracking-[0.3em] mb-2">
        {label}
      </h4>
      {children}
    </section>
  );
}

function eventTypeColor(type: string): string {
  if (type.includes('error')) return 'bg-status-error/15 text-status-error';
  if (type.includes('approval')) return 'bg-status-pending/15 text-status-pending';
  if (type.includes('llm')) return 'bg-primary/15 text-primary-light';
  if (type.includes('tool')) return 'bg-secondary/15 text-secondary-light';
  return 'bg-surface text-text-muted';
}
