import { useEffect, useRef, type ReactNode } from 'react';
import { Copy, ScrollText, Waypoints, X } from 'lucide-react';
import type { RunRecord } from '../../lib/api';
import { cn } from '../../lib/cn';
import { formatTotalDuration } from '../../lib/duration';
import { formatLocalDateTime } from '../../lib/time';
import { KVTable } from '../ui/KVTable';
import { copyWithToast } from '../ui/Toast';
import { DispositionBadge } from './DispositionBadge';

interface RunDetailProps {
  record: RunRecord;
  /** The loaded page, for parent, previous, and nested attempt lookups. */
  records: RunRecord[];
  onSelect: (attemptId: string) => void;
  onClose: () => void;
  onViewTrace?: (traceId: string) => void;
  onViewLogs?: (traceId: string) => void;
  narrow?: boolean;
}

const PIVOT_CLASS =
  'flex items-center justify-center gap-1 min-h-6 min-w-6 px-1.5 py-0.5 rounded text-3xs font-medium text-text-muted hover:text-primary hover:bg-surface-highlight transition-colors';

// eslint-disable-next-line react-refresh/only-export-components -- shared with the run row so both read the same
export function runTarget(record: RunRecord): string {
  return record.resolvedServer
    ? `${record.resolvedServer} › ${record.resolvedTool ?? ''}`
    : record.requestedName || 'unresolved';
}

export function RunDetail({ record, records, onSelect, onClose, onViewTrace, onViewLogs, narrow }: RunDetailProps) {
  const target = runTarget(record);
  const asideRef = useRef<HTMLElement>(null);
  // Following a parent, previous, or nested link can remove the focused
  // button; keep focus in the detail instead of dropping it to <body>.
  const shownId = useRef(record.attemptId);
  useEffect(() => {
    if (shownId.current === record.attemptId) return;
    shownId.current = record.attemptId;
    const active = document.activeElement;
    if (!active || active === document.body) asideRef.current?.focus();
  }, [record.attemptId]);
  const children = records.filter((r) => r.parentAttemptId === record.attemptId);
  const loaded = (id?: string) => Boolean(id) && records.some((r) => r.attemptId === id);
  const traceId = record.traceId;
  const hasLinks = Boolean(traceId) && Boolean(onViewTrace || onViewLogs);

  const outcome: [string, ReactNode][] = [['Disposition', record.disposition]];
  if (record.stage) outcome.push(['Stage', record.stage]);
  if (record.reason) outcome.push(['Reason', record.reason]);

  const targetRows: [string, ReactNode][] = [];
  const named = (label: string, value?: string, omitted?: string) => {
    if (value) targetRows.push([label, value]);
    else if (omitted) targetRows.push([label, `omitted (${omitted})`]);
  };
  named('Requested name', record.requestedName, record.requestedNameOmitted);
  named('Server', record.resolvedServer, record.resolvedServerOmitted);
  named('Tool', record.resolvedTool, record.resolvedToolOmitted);
  if (record.replicaId !== undefined) targetRows.push(['Replica', String(record.replicaId)]);

  const timing: [string, ReactNode][] = [
    ['Started', <time key="s" dateTime={record.startedAt} title={record.startedAt}>{formatLocalDateTime(record.startedAt)}</time>],
    ['Returned', <time key="r" dateTime={record.returnedAt} title={record.returnedAt}>{formatLocalDateTime(record.returnedAt)}</time>],
    ['Duration', formatTotalDuration(record.durationMs)],
  ];
  if (record.downstreamDurationMs !== undefined) {
    timing.push(['Downstream', formatTotalDuration(record.downstreamDurationMs)]);
  }

  const ids: [string, ReactNode][] = [
    ['Attempt', <IdValue key="a" label="Attempt" value={record.attemptId} />],
  ];
  if (traceId) ids.push(['Trace', <IdValue key="t" label="Trace" value={traceId} />]);
  ids.push([
    'Parent',
    record.parentAttemptId ? (
      <IdValue key="p" label="Parent" value={record.parentAttemptId} onSelect={loaded(record.parentAttemptId) ? onSelect : undefined} />
    ) : (
      'none'
    ),
  ]);
  ids.push(['Root', <IdValue key="root" label="Root" value={record.rootAttemptId || record.attemptId} />]);
  ids.push([
    'Previous',
    record.previousAttemptId ? (
      <IdValue key="prev" label="Previous" value={record.previousAttemptId} onSelect={loaded(record.previousAttemptId) ? onSelect : undefined} />
    ) : (
      'none'
    ),
  ]);

  const caller: [string, ReactNode][] = [
    ['Client', record.clientLabel || 'not recorded'],
    ['Access', record.accessLabel || 'not recorded'],
  ];
  const recordRows: [string, ReactNode][] = [
    ['Sequence', String(record.sequence)],
    ['Recorder instance', record.recorderInstanceId],
    ['Schema version', String(record.schemaVersion)],
  ];

  return (
    <aside
      ref={asideRef}
      tabIndex={-1}
      data-run-detail=""
      className={cn(
        'border-border overflow-auto flex flex-col min-h-0',
        narrow ? 'w-full border-t' : 'w-96 flex-shrink-0 border-l',
      )}
      aria-label="Run detail"
    >
      <div className="flex items-center gap-2 px-3 h-9 flex-shrink-0 border-b border-border/30 bg-surface-elevated/20 min-w-0">
        <span className="text-xs font-medium text-text-primary truncate font-mono" title={target}>
          {target}
        </span>
        <DispositionBadge value={record.disposition} className="flex-shrink-0" />
        <button
          type="button"
          onClick={onClose}
          className="ml-auto size-6 inline-flex items-center justify-center rounded-md hover:bg-surface-highlight transition-colors flex-shrink-0"
          aria-label="Close run detail"
        >
          <X size={12} className="text-text-muted" aria-hidden="true" />
        </button>
      </div>
      <div className="p-3 space-y-4">
        {traceId ? (
          hasLinks && (
            <div className="flex flex-wrap items-center gap-1">
              {onViewTrace && (
                <button
                  type="button"
                  onClick={() => onViewTrace(traceId)}
                  title="Open this run's trace. Traces are held in memory and may no longer be available."
                  className={PIVOT_CLASS}
                >
                  <Waypoints size={11} className="flex-shrink-0" aria-hidden="true" />
                  <span>View trace</span>
                </button>
              )}
              {onViewLogs && (
                <button
                  type="button"
                  onClick={() => onViewLogs(traceId)}
                  title="View logs for this run's trace"
                  className={PIVOT_CLASS}
                >
                  <ScrollText size={11} className="flex-shrink-0" aria-hidden="true" />
                  <span>View logs</span>
                </button>
              )}
            </div>
          )
        ) : (
          <p className="text-xs text-text-muted">No trace was sampled for this run.</p>
        )}
        <Section label="Outcome" rows={outcome} />
        {targetRows.length > 0 && <Section label="Target" rows={targetRows} />}
        <Section label="Timing" rows={timing} />
        <Section label="IDs" rows={ids} />
        <Section label="Caller" rows={caller} />
        <Section label="Record" rows={recordRows} />
        {children.length > 0 && (
          <section>
            <SectionLabel>Nested attempts</SectionLabel>
            <ul className="space-y-1 text-xs font-mono">
              {children.map((c) => (
                <li key={c.attemptId} className="flex items-center gap-2 min-w-0">
                  <button
                    type="button"
                    className="underline truncate text-text-primary"
                    onClick={() => onSelect(c.attemptId)}
                  >
                    {c.requestedName || c.attemptId}
                  </button>
                  <DispositionBadge value={c.disposition} />
                </li>
              ))}
            </ul>
          </section>
        )}
        {record.parentAttemptId && !loaded(record.parentAttemptId) && (
          <p className="text-xs text-text-muted">Parent record is unavailable, not still running.</p>
        )}
      </div>
    </aside>
  );
}

function SectionLabel({ children }: { children: ReactNode }) {
  return (
    <h3 className="mb-2 text-3xs font-medium text-text-muted uppercase tracking-wider">{children}</h3>
  );
}

function Section({ label, rows }: { label: string; rows: [string, ReactNode][] }) {
  return (
    <section>
      <SectionLabel>{label}</SectionLabel>
      <KVTable rows={rows} />
    </section>
  );
}

function IdValue({ label, value, onSelect }: { label: string; value: string; onSelect?: (id: string) => void }) {
  return (
    <span className="flex items-start gap-1 min-w-0">
      {onSelect ? (
        <button
          type="button"
          className="underline text-left break-all text-text-primary"
          onClick={() => onSelect(value)}
          title={`Open ${label.toLowerCase()} run`}
        >
          {value}
        </button>
      ) : (
        <span className="break-all">{value}</span>
      )}
      <button
        type="button"
        onClick={() => void copyWithToast(value, `${label} ID`)}
        title={`Copy ${label.toLowerCase()} ID`}
        aria-label={`Copy ${label.toLowerCase()} ID`}
        className="size-5 inline-flex items-center justify-center rounded text-text-muted hover:text-primary hover:bg-surface-highlight transition-colors flex-shrink-0"
      >
        <Copy size={10} aria-hidden="true" />
      </button>
    </span>
  );
}
