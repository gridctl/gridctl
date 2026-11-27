import { useId, useMemo, useRef, useState, type MouseEvent, type ReactNode } from 'react';
import { ArrowUpRight, ChevronDown, ChevronRight, Copy, Wrench } from 'lucide-react';
import { cn } from '../../lib/cn';
import { formatCompactNumber } from '../../lib/format';
import { CodeViewer } from '../ui/CodeViewer';
import { copyWithToast } from '../ui/Toast';
import { ZoomControls } from '../ui/ZoomControls';
import { InspectorHeader, PaneAnchor } from '../inspector';
import { InspectorStat } from '../metrics/metricsShared';
import { pinStatusMeta } from '../pins/pinStatus';
import { formatLastUsed, type AuditState } from '../../lib/toolAudit';
import { CHIP_TONE_CLASS, annotationChips, policyChip, policyDetailText } from '../../lib/groups';
import { TOOL_NAME_DELIMITER } from '../../lib/constants';
import { clampDescription } from '../../lib/toolDescription';
import { schemaParameters, type SchemaParameter } from '../../lib/toolSchema';
import type { PolicyToolStatus, ServerPins } from '../../lib/api';
import { useTextZoom } from '../../hooks/useTextZoom';
import { ToolTryIt } from './ToolTryIt';
import { TOOLS_TEXT_ZOOM } from '../../lib/textZoomDefaults';
import type { ToolRow } from '../../hooks/useToolsEditor';
import type { ToolUsageStat } from '../../types';

// Per-state styling for the audit row, mirroring the list's AUDIT_STYLES.
const AUDIT_LABEL: Record<AuditState, { text: string; dot: string; label: string }> = {
  used: { text: 'text-status-running', dot: 'bg-status-running', label: 'used' },
  unused: { text: 'text-status-pending', dot: 'bg-status-pending', label: 'unused' },
  disabled: { text: 'text-text-muted', dot: 'bg-text-muted/50', label: 'disabled' },
};

const MAX_ENUM_VALUES = 8;
const MAX_DEFAULT_CHARS = 80;

const HEADER_ACTION_CLASS =
  'inline-flex items-center gap-1 text-3xs text-secondary hover:text-secondary-light transition-colors rounded';

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
  // The tool's observed usage (calls/tokens), when any call has been
  // recorded. Shown whenever present — not gated on Audit Mode. Also gates
  // the Metrics link, since Metrics only has rows for tools with usage.
  usage?: ToolUsageStat;
  lastCalledAt?: string;
  // Verdict from GET /api/policy for this prefixed tool, when a block is configured.
  policy?: PolicyToolStatus;
  // The server's entry from GET /api/pins. Pin status is per server.
  pin?: ServerPins;
  // True when the saved allow-list is non-empty and excludes this tool. Such
  // tools are normally absent from the pin set, so the chip stays silent.
  hiddenByAllowList?: boolean;
  // Whether the saved allow-list exposes the tool (gates Try it). Defaults to
  // `enabled` for callers that do not distinguish saved from draft.
  exposed?: boolean;
  // Whether the unsaved draft differs from the saved allow-list for this tool.
  draftDiffers?: boolean;
  // Handles an unmodified click on a header link. Without it links behave as
  // plain anchors.
  onNavigate?: (path: string) => void;
  onClose: () => void;
}

// ToolDetailPanel fills the Tools workspace right rail with the selected tool's
// description, parameters, raw input schema, and metadata. Schema is rendered
// with the shared CodeViewer so it matches the Spec tab's tokenized treatment.
// When no tool is selected it shows an explicit prompt rather than a blank gap.
export function ToolDetailPanel({
  serverName,
  tool,
  schema,
  enabled,
  auditMode,
  auditState,
  usage,
  lastCalledAt,
  policy,
  pin,
  hiddenByAllowList = false,
  exposed = enabled,
  draftDiffers = false,
  onNavigate,
  onClose,
}: ToolDetailPanelProps) {
  // Content text-size control for the description + JSON schema, scoped to this
  // pane (own storageKey) like the Logs/Traces zoom; 12px stays the default.
  const contentRef = useRef<HTMLDivElement>(null);
  const { fontSize, zoomIn, zoomOut, resetZoom, isMin, isMax, isDefault } = useTextZoom({
    storageKey: TOOLS_TEXT_ZOOM.name,
    defaultSize: TOOLS_TEXT_ZOOM.defaultSize,
    minSize: TOOLS_TEXT_ZOOM.minSize,
    maxSize: TOOLS_TEXT_ZOOM.maxSize,
    step: TOOLS_TEXT_ZOOM.step,
    containerRef: contentRef,
  });

  // The user's raw-schema choice, once made, holds across tool selections.
  const [rawOpenChoice, setRawOpenChoice] = useState<boolean | null>(null);

  const parameters = useMemo(() => schemaParameters(schema), [schema]);
  const schemaText = useMemo(() => (schema ? JSON.stringify(schema, null, 2) : ''), [schema]);

  const prefixed = tool ? `${serverName}${TOOL_NAME_DELIMITER}${tool.name}` : '';
  const tracesPath = tool
    ? `/traces?q=${encodeURIComponent(tool.name)}&server=${encodeURIComponent(serverName)}&errors=0`
    : '';
  const metricsPath = `/metrics?scope=tools&selected=${encodeURIComponent(prefixed)}`;

  return (
    <aside aria-label="Tool details" className="relative h-full flex flex-col bg-surface-elevated border-l border-border">
      {tool ? (
        <>
          <PaneAnchor />
          <InspectorHeader
            title={tool.name}
            icon={Wrench}
            accent="primary"
            onClose={onClose}
            actions={
              <ZoomControls
                fontSize={fontSize}
                onZoomIn={zoomIn}
                onZoomOut={zoomOut}
                onReset={resetZoom}
                isMin={isMin}
                isMax={isMax}
                isDefault={isDefault}
              />
            }
            subtitle={
              <div className="flex items-center gap-1.5 flex-wrap mt-0.5">
                <span className="text-3xs font-mono text-primary truncate">{serverName}</span>
                <span
                  className={cn(
                    'text-3xs font-mono px-1.5 py-0.5 rounded',
                    enabled
                      ? 'bg-primary/15 text-primary'
                      : 'bg-surface-elevated text-text-muted',
                  )}
                >
                  {enabled ? 'enabled' : 'disabled'}
                </span>
                <PinChip
                  serverName={serverName}
                  toolName={tool.name}
                  pin={pin}
                  hiddenByAllowList={hiddenByAllowList}
                  onNavigate={onNavigate}
                />
                <button
                  type="button"
                  onClick={() => void copyWithToast(prefixed, 'Tool name')}
                  aria-label={`Copy prefixed name ${prefixed}`}
                  className={HEADER_ACTION_CLASS}
                >
                  <Copy size={10} aria-hidden="true" />
                  Copy name
                </button>
                <NavLink path={tracesPath} onNavigate={onNavigate} className={HEADER_ACTION_CLASS}>
                  <ArrowUpRight size={10} aria-hidden="true" />
                  Traces
                </NavLink>
                {usage && (
                  <NavLink path={metricsPath} onNavigate={onNavigate} className={HEADER_ACTION_CLASS}>
                    <ArrowUpRight size={10} aria-hidden="true" />
                    Metrics
                  </NavLink>
                )}
              </div>
            }
          />

          <div
            ref={contentRef}
            className="flex-1 min-h-0 overflow-y-auto scrollbar-dark px-4 py-4 space-y-5"
          >
            <ToolTryIt
              key={prefixed}
              serverName={serverName}
              tool={tool}
              schema={schema}
              exposed={exposed}
              draftDiffers={draftDiffers}
              policy={policy}
              fontSize={fontSize}
              onNavigate={onNavigate}
            />

            <Section title="Description">
              {tool.description ? (
                <DescriptionBlock
                  key={`${serverName}/${tool.name}`}
                  text={tool.description}
                  fontSize={fontSize}
                />
              ) : (
                <p className="text-2xs text-text-muted italic">No description available.</p>
              )}
            </Section>

            <Section title="Parameters">
              {schema ? (
                <div className="space-y-3">
                  <ParametersTable
                    toolName={tool.name}
                    rows={parameters.rows}
                    notes={parameters.notes}
                    empty={parameters.empty}
                    fontSize={fontSize}
                  />
                  <RawSchema
                    toolName={tool.name}
                    content={schemaText}
                    fontSize={fontSize}
                    open={rawOpenChoice ?? !(parameters.rows.length > 0 || parameters.empty)}
                    onToggle={(next) => setRawOpenChoice(next)}
                  />
                </div>
              ) : (
                <p className="text-2xs text-text-muted italic">No input schema available.</p>
              )}
            </Section>

            <Section title="Hints">
              <HintsBlock annotations={tool.annotations} />
            </Section>

            <PolicyBlock policy={policy} />

            {(usage || (auditMode && auditState)) && (
              <Section title="Usage">
                <div className="space-y-2">
                  {auditMode && auditState && (
                    <div
                      className={cn(
                        'flex items-center gap-1.5 text-2xs',
                        AUDIT_LABEL[auditState].text,
                      )}
                    >
                      <span
                        className={cn('inline-block w-1.5 h-1.5 rounded-full', AUDIT_LABEL[auditState].dot)}
                        aria-hidden="true"
                      />
                      <span>{AUDIT_LABEL[auditState].label}</span>
                      {auditState !== 'disabled' && (
                        <span className="text-text-muted">· {formatLastUsed(lastCalledAt)}</span>
                      )}
                    </div>
                  )}
                  {usage && (
                    <div className="grid grid-cols-2 gap-2">
                      <InspectorStat label="Calls" value={formatCompactNumber(usage.calls)} className="text-text-primary" />
                      <InspectorStat label="Last used" value={formatLastUsed(usage.lastCalledAt)} className="text-text-secondary" />
                      <InspectorStat label="Input" value={formatCompactNumber(usage.inputTokens ?? 0)} className="text-secondary" />
                      <InspectorStat label="Output" value={formatCompactNumber(usage.outputTokens ?? 0)} className="text-primary" />
                    </div>
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

// A real anchor so modified clicks open a new tab; an unmodified primary
// click is handed to onNavigate, which applies the unsaved-edits guard.
function NavLink({
  path,
  onNavigate,
  className,
  'aria-label': ariaLabel,
  children,
}: {
  path: string;
  onNavigate?: (path: string) => void;
  className?: string;
  'aria-label'?: string;
  children: ReactNode;
}) {
  const handleClick = (e: MouseEvent<HTMLAnchorElement>) => {
    if (!onNavigate) return;
    if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    e.preventDefault();
    onNavigate(path);
  };
  return (
    <a href={path} onClick={handleClick} aria-label={ariaLabel} className={className}>
      {children}
    </a>
  );
}

function PinChip({
  serverName,
  toolName,
  pin,
  hiddenByAllowList,
  onNavigate,
}: {
  serverName: string;
  toolName: string;
  pin?: ServerPins;
  hiddenByAllowList: boolean;
  onNavigate?: (path: string) => void;
}) {
  if (!pin) return null;
  const inPinSet = Object.prototype.hasOwnProperty.call(pin.tools ?? {}, toolName);
  if (!inPinSet && hiddenByAllowList) return null;

  let label: string;
  let colorClass: string;
  if (inPinSet) {
    const meta = pinStatusMeta(pin.status);
    label = pin.status === 'drift' ? 'Server drift' : meta.label;
    colorClass = meta.colorClass;
  } else {
    label = 'Not in pin set';
    colorClass = 'text-text-muted';
  }
  const drift = pin.status === 'drift';
  const path = `/pins?server=${encodeURIComponent(serverName)}${drift ? '&view=drift' : ''}`;

  return (
    <NavLink
      path={path}
      onNavigate={onNavigate}
      aria-label={`Pin status: ${label}. Open in Pins`}
      className={cn(
        'text-3xs font-mono px-1.5 py-0.5 rounded bg-surface-highlight hover:bg-surface-highlight/70 transition-colors',
        colorClass,
      )}
    >
      {label}
    </NavLink>
  );
}

function DescriptionBlock({ text, fontSize }: { text: string; fontSize: number }) {
  const id = useId();
  const [expanded, setExpanded] = useState(false);
  // Descriptions are attacker-controlled instructions to the model, so they
  // stay plain text and a shortened view always says how much it hides.
  const { visible, hiddenChars } = useMemo(() => clampDescription(text), [text]);
  const clamped = hiddenChars > 0;
  return (
    <div className="space-y-1">
      <p
        id={id}
        className="text-text-secondary leading-relaxed whitespace-pre-wrap break-words"
        style={{ fontSize }}
      >
        {clamped && !expanded ? `${visible}…` : text}
      </p>
      {clamped && (
        <button
          type="button"
          aria-expanded={expanded}
          aria-controls={id}
          onClick={() => setExpanded((v) => !v)}
          className="text-3xs text-secondary hover:text-secondary-light transition-colors rounded"
        >
          {expanded
            ? 'Show less'
            : `Show more (${hiddenChars.toLocaleString('en-US')} more ${hiddenChars === 1 ? 'character' : 'characters'})`}
        </button>
      )}
    </div>
  );
}

function truncateDefault(text: string): string {
  return text.length > MAX_DEFAULT_CHARS ? `${text.slice(0, MAX_DEFAULT_CHARS)}…` : text;
}

function enumText(values: string[]): string {
  const shown = values.slice(0, MAX_ENUM_VALUES).join(', ');
  const rest = values.length - MAX_ENUM_VALUES;
  return rest > 0 ? `${shown} +${rest} more` : shown;
}

function ParametersTable({
  toolName,
  rows,
  notes,
  empty,
  fontSize,
}: {
  toolName: string;
  rows: SchemaParameter[];
  notes: string[];
  empty: boolean;
  fontSize: number;
}) {
  return (
    <div className="space-y-2">
      {rows.length > 0 ? (
        <div className="rounded-md border border-border/30 overflow-x-auto">
          <table
            aria-label={`${toolName} parameters`}
            className="w-full border-collapse text-left"
            style={{ fontSize }}
          >
            <thead>
              <tr className="border-b border-border/30 text-3xs uppercase tracking-[0.12em] text-text-muted">
                <th scope="col" className="px-2 py-1.5 font-medium">Parameter</th>
                <th scope="col" className="px-2 py-1.5 font-medium">Type</th>
                <th scope="col" className="px-2 py-1.5 font-medium">Description</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <tr key={row.name} className="border-b border-border/20 last:border-b-0 align-top">
                  <td className="px-2 py-1.5">
                    <span className="font-mono text-text-primary break-all">{row.name}</span>
                    {row.required && (
                      <span className="block text-3xs text-status-pending">required</span>
                    )}
                  </td>
                  <td className="px-2 py-1.5 font-mono text-text-muted break-all">{row.type}</td>
                  <td className="px-2 py-1.5 text-text-secondary">
                    {row.description && (
                      <p className="whitespace-pre-wrap break-words">{row.description}</p>
                    )}
                    {row.defaultText !== undefined && (
                      <p className="text-text-muted break-words">Default: {truncateDefault(row.defaultText)}</p>
                    )}
                    {row.enumValues && row.enumValues.length > 0 && (
                      <p className="text-text-muted break-words">One of: {enumText(row.enumValues)}</p>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <p className="text-2xs text-text-muted italic">
          {empty ? 'No parameters.' : 'Parameters cannot be summarized; see the raw schema.'}
        </p>
      )}
      {notes.map((note) => (
        <p key={note} className="text-3xs text-text-muted leading-relaxed">
          {note}
        </p>
      ))}
    </div>
  );
}

function RawSchema({
  toolName,
  content,
  fontSize,
  open,
  onToggle,
}: {
  toolName: string;
  content: string;
  fontSize: number;
  open: boolean;
  onToggle: (open: boolean) => void;
}) {
  const regionId = useId();
  const Chevron = open ? ChevronDown : ChevronRight;
  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <button
          type="button"
          aria-expanded={open}
          aria-controls={regionId}
          onClick={() => onToggle(!open)}
          className="inline-flex items-center gap-1 text-3xs uppercase tracking-[0.18em] text-text-muted hover:text-text-secondary transition-colors rounded"
        >
          <Chevron size={12} aria-hidden="true" />
          Raw schema
        </button>
        <button
          type="button"
          onClick={() => void copyWithToast(content, 'Input schema')}
          aria-label="Copy input schema"
          className={cn(HEADER_ACTION_CLASS, 'ml-auto')}
        >
          <Copy size={10} aria-hidden="true" />
          Copy
        </button>
      </div>
      <div id={regionId}>
        {open && (
          <CodeViewer
            language="json"
            content={content}
            fontSize={fontSize}
            ariaLabel={`${toolName} input schema`}
            className="rounded-md border border-border/30 bg-background/80 max-h-[45vh]"
          />
        )}
      </div>
    </div>
  );
}

// HintsBlock lists the tool's declared MCP annotations with plain-language
// descriptions. Annotations are server-reported claims the spec says to treat
// as untrusted, so the block always carries that caveat; an unannotated tool
// gets the spec's pessimistic default spelled out instead of a blank section.
function HintsBlock({ annotations }: { annotations?: ToolRow['annotations'] }) {
  const chips = annotationChips(annotations);
  if (chips.length === 0) {
    return (
      <p className="text-2xs text-text-muted leading-relaxed">
        No annotations declared. Per the MCP spec, undeclared tools are treated as
        potentially destructive and open-world.
      </p>
    );
  }
  return (
    <div className="space-y-2">
      <ul className="space-y-1.5">
        {chips.map((chip) => (
          <li key={chip.label} className="flex items-start gap-2">
            <span
              className={cn(
                'flex-shrink-0 px-1.5 py-px rounded text-3xs font-medium mt-px',
                CHIP_TONE_CLASS[chip.tone],
              )}
            >
              {chip.label}
            </span>
            <span className="text-2xs text-text-secondary leading-relaxed">{chip.title}</span>
          </li>
        ))}
      </ul>
      <p className="text-3xs text-text-muted italic">
        Reported by the server, not verified by gridctl.
      </p>
    </div>
  );
}

function PolicyBlock({ policy }: { policy?: PolicyToolStatus }) {
  const text = policyDetailText(policy);
  const chip = policyChip(policy);
  if (!text || !chip) return null;
  return (
    <Section title="Policy">
      <div className="flex items-start gap-2">
        <span
          title={chip.title}
          className={cn(
            'flex-shrink-0 px-1.5 py-px rounded text-3xs font-medium mt-px',
            CHIP_TONE_CLASS[chip.tone],
          )}
        >
          {chip.label}
        </span>
        <span className="text-2xs text-text-secondary leading-relaxed">{text}</span>
      </div>
    </Section>
  );
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="space-y-2">
      <h3 className="text-3xs uppercase tracking-[0.18em] text-text-muted">{title}</h3>
      {children}
    </section>
  );
}

function ToolDetailEmpty() {
  return (
    <div className="h-full flex items-center justify-center px-6 text-center">
      <div className="space-y-3">
        <div className="mx-auto w-12 h-12 rounded-2xl bg-surface-highlight/40 border border-border/40 flex items-center justify-center">
          <Wrench size={20} className="text-text-muted" aria-hidden="true" />
        </div>
        <p className="text-xs text-text-muted leading-relaxed max-w-[220px]">
          Select a tool to view its description and input schema.
        </p>
      </div>
    </div>
  );
}
