import type { ReactNode } from 'react';
import { Wrench } from 'lucide-react';
import { cn } from '../../lib/cn';
import { CodeViewer } from '../ui/CodeViewer';
import { InspectorHeader, PaneAnchor } from '../inspector';
import { formatLastUsed, type AuditState } from '../../lib/toolAudit';
import type { ToolRow } from '../../hooks/useToolsEditor';

// Per-state styling for the audit row, mirroring the list's AUDIT_STYLES.
const AUDIT_LABEL: Record<AuditState, { text: string; dot: string; label: string }> = {
  used: { text: 'text-status-running', dot: 'bg-status-running', label: 'used' },
  unused: { text: 'text-status-pending', dot: 'bg-status-pending', label: 'unused' },
  disabled: { text: 'text-text-muted', dot: 'bg-text-muted/50', label: 'disabled' },
};

interface ToolDetailPanelProps {
  serverName: string;
  // The selected tool (name + description), or null when nothing is selected.
  tool: ToolRow | null;
  // The tool's input schema (JSON Schema), or undefined when unavailable.
  schema?: Record<string, unknown>;
  // Whether the tool is in the server's exposed whitelist.
  enabled: boolean;
  auditMode: boolean;
  auditState: AuditState | null;
  lastCalledAt?: string;
  onClose: () => void;
}

// ToolDetailPanel fills the Tools workspace right rail with the selected tool's
// description, input schema, and metadata. Schema is rendered with the shared
// CodeViewer so it matches the Spec tab's tokenized treatment. When no tool is
// selected it shows an explicit prompt rather than a blank gap.
export function ToolDetailPanel({
  serverName,
  tool,
  schema,
  enabled,
  auditMode,
  auditState,
  lastCalledAt,
  onClose,
}: ToolDetailPanelProps) {
  return (
    <aside className="relative h-full flex flex-col bg-surface-elevated border-l border-border">
      {tool ? (
        <>
          <PaneAnchor />
          <InspectorHeader
            title={tool.name}
            icon={Wrench}
            accent="primary"
            onClose={onClose}
            subtitle={
              <div className="flex items-center gap-1.5 flex-wrap mt-0.5">
                <span className="text-[10px] font-mono text-primary/80 truncate">{serverName}</span>
                <span
                  className={cn(
                    'text-[10px] font-mono px-1.5 py-0.5 rounded',
                    enabled
                      ? 'bg-primary/15 text-primary'
                      : 'bg-surface-elevated text-text-muted',
                  )}
                >
                  {enabled ? 'enabled' : 'disabled'}
                </span>
              </div>
            }
          />

          <div className="flex-1 min-h-0 overflow-y-auto scrollbar-dark px-4 py-4 space-y-5">
            <Section title="Description">
              {tool.description ? (
                <p className="text-xs text-text-secondary leading-relaxed whitespace-pre-wrap break-words">
                  {tool.description}
                </p>
              ) : (
                <p className="text-[11px] text-text-muted/70 italic">No description available.</p>
              )}
            </Section>

            <Section title="Input schema">
              {schema ? (
                <CodeViewer
                  language="json"
                  content={JSON.stringify(schema, null, 2)}
                  ariaLabel={`${tool.name} input schema`}
                  className="rounded-md border border-border/30 bg-background/80 max-h-[45vh]"
                />
              ) : (
                <p className="text-[11px] text-text-muted/70 italic">No input schema available.</p>
              )}
            </Section>

            {auditMode && auditState && (
              <Section title="Usage">
                <div
                  className={cn(
                    'flex items-center gap-1.5 text-[11px]',
                    AUDIT_LABEL[auditState].text,
                  )}
                >
                  <span
                    className={cn('inline-block w-1.5 h-1.5 rounded-full', AUDIT_LABEL[auditState].dot)}
                    aria-hidden="true"
                  />
                  <span>{AUDIT_LABEL[auditState].label}</span>
                  {auditState !== 'disabled' && (
                    <span className="text-text-muted/70">· {formatLastUsed(lastCalledAt)}</span>
                  )}
                </div>
              </Section>
            )}
          </div>
        </>
      ) : (
        <ToolDetailEmpty />
      )}
    </aside>
  );
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="space-y-2">
      <h3 className="text-[10px] uppercase tracking-[0.18em] text-text-muted/70">{title}</h3>
      {children}
    </section>
  );
}

function ToolDetailEmpty() {
  return (
    <div className="h-full flex items-center justify-center px-6 text-center">
      <div className="space-y-3">
        <div className="mx-auto w-12 h-12 rounded-2xl bg-surface-highlight/40 border border-border/40 flex items-center justify-center">
          <Wrench size={20} className="text-text-muted/60" aria-hidden="true" />
        </div>
        <p className="text-xs text-text-muted leading-relaxed max-w-[220px]">
          Select a tool to view its description and input schema.
        </p>
      </div>
    </div>
  );
}
