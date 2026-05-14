import { useMemo, useState } from 'react';
import { cn } from '../../../lib/cn';
import { formatRelativeTime } from '../../../lib/time';
import type { RunEvent } from '../../../lib/agent-api';
import type { RunTrace } from './useRunTrace';

interface RunOutputViewProps {
  runID: string;
  runTrace: RunTrace;
}

// Soft cap on what we render verbatim inside the inspector. Anything
// beyond this is collapsed behind an expand affordance — the JSON
// stays in memory but is not painted to DOM until the user asks.
const COLLAPSE_THRESHOLD = 10_000;

// Verbatim cap matches the backend recorder's size cap (64 KB) so the
// "truncated" pill in the UI maps 1:1 to the recorder's "truncated:
// true" marker on Output/Arguments payloads.
const TRUNCATION_HINT_BYTES = 64 * 1024;

interface CompletionShape {
  output?: unknown;
  error?: string;
  duration_micros?: number;
  truncated?: boolean;
  status?: string;
}

interface StartedShape {
  parent_run_id?: string;
  skill?: string;
}

/**
 * RunOutputView is the right-pane terminal-output viewer that lights
 * up the moment no node is selected but a run is active. It folds
 * the run's `run_started` + `run_completed` events into a single
 * scannable view: a status header, a JSON viewer for the terminal
 * Output (or a red `<pre>` for Error), and a live event counter
 * while the run is still in-flight.
 *
 * Long outputs stay collapsed behind an expand affordance — never
 * paint > ~10KB of JSON eagerly, the panel is 360px wide and the
 * canvas/inspector layout can't survive a runaway DOM tree.
 */
export function RunOutputView({ runID, runTrace }: RunOutputViewProps) {
  const completed = useMemo(
    () => findEvent(runTrace.events, 'run_completed'),
    [runTrace.events],
  );
  const started = useMemo(
    () => findEvent(runTrace.events, 'run_started'),
    [runTrace.events],
  );

  const startedPayload = (started?.payload ?? {}) as StartedShape;
  const completedPayload = (completed?.payload ?? {}) as CompletionShape;

  const completedAt = completed?.time ? new Date(completed.time) : null;
  const completedAtValid = completedAt && !isNaN(completedAt.getTime()) ? completedAt : null;

  const errored = isErrorOutcome(completedPayload);
  const durationLabel = formatDuration(completedPayload.duration_micros);

  if (!completed) {
    return (
      <div className="space-y-4" aria-live="polite">
        <Header
          runID={runID}
          parentRunID={startedPayload.parent_run_id}
          eyebrow="awaiting output"
        />
        <InFlightCard
          eventCount={runTrace.events.length}
          status={runTrace.status}
        />
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <Header
        runID={runID}
        parentRunID={startedPayload.parent_run_id}
        eyebrow={errored ? 'run errored' : 'run completed'}
        accent={errored ? 'error' : 'ok'}
      />
      <div className="flex items-center gap-3 font-mono text-[10px] text-text-muted/80 -mt-1">
        {completedAtValid && (
          <span className="tabular-nums">{formatRelativeTime(completedAtValid)}</span>
        )}
        {durationLabel && (
          <>
            <span className="text-text-muted/30">·</span>
            <span className="tabular-nums">{durationLabel}</span>
          </>
        )}
        <span className="text-text-muted/30">·</span>
        <span className="tabular-nums">{runTrace.events.length} events</span>
      </div>
      {errored ? (
        <ErrorPayload message={completedPayload.error ?? 'unknown error'} />
      ) : (
        <OutputPayload
          output={completedPayload.output}
          truncated={completedPayload.truncated}
        />
      )}
    </div>
  );
}

function findEvent(events: RunEvent[], type: string): RunEvent | undefined {
  for (let i = events.length - 1; i >= 0; i--) {
    if (events[i].type === type) return events[i];
  }
  return undefined;
}

function isErrorOutcome(payload: CompletionShape): boolean {
  if (typeof payload.error === 'string' && payload.error.length > 0) return true;
  if (typeof payload.status === 'string') {
    const s = payload.status.toLowerCase();
    if (s === 'error' || s === 'errored' || s === 'failed') return true;
  }
  return false;
}

function formatDuration(micros: number | undefined): string | null {
  if (micros == null) return null;
  if (micros < 1000) return `${micros}µs`;
  if (micros < 1_000_000) return `${(micros / 1000).toFixed(1)}ms`;
  return `${(micros / 1_000_000).toFixed(2)}s`;
}

interface HeaderProps {
  runID: string;
  parentRunID: string | undefined;
  eyebrow: string;
  accent?: 'ok' | 'error';
}

function Header({ runID, parentRunID, eyebrow, accent }: HeaderProps) {
  const accentClass =
    accent === 'error'
      ? 'text-status-error'
      : accent === 'ok'
        ? 'text-status-running/80'
        : 'text-text-muted/60';
  return (
    <div>
      <div
        className={cn(
          'font-sans text-[10px] uppercase tracking-[0.3em] mb-1',
          accentClass,
        )}
      >
        {eyebrow}
      </div>
      <div className="font-mono text-xs text-text-primary tabular-nums truncate">
        run {runID.slice(0, 8)}
      </div>
      {parentRunID && (
        <div className="font-mono text-[10px] text-text-muted mt-0.5 truncate">
          parent {parentRunID.slice(0, 8)}
        </div>
      )}
    </div>
  );
}

interface InFlightCardProps {
  eventCount: number;
  status: 'connecting' | 'open' | 'error' | 'idle';
}

function InFlightCard({ eventCount, status }: InFlightCardProps) {
  const statusLabel =
    status === 'open'
      ? 'streaming'
      : status === 'connecting'
        ? 'connecting'
        : status === 'error'
          ? 'disconnected'
          : 'idle';
  return (
    <div
      className={cn(
        'rounded-md border border-border-subtle bg-surface/40 px-4 py-5',
        'flex flex-col items-center text-center gap-3',
      )}
    >
      <div className="relative">
        <span
          aria-hidden
          className={cn(
            'block w-2 h-2 rounded-full',
            status === 'error' ? 'bg-status-error' : 'bg-status-running',
          )}
          style={
            status === 'open'
              ? {
                  boxShadow: '0 0 12px var(--color-status-running)',
                  animation: 'pulse 2s ease-in-out infinite',
                }
              : undefined
          }
        />
      </div>
      <div className="font-sans text-text-primary text-sm leading-snug">
        waiting for completion…
      </div>
      <div className="flex items-center gap-2 font-mono text-[10px] text-text-muted/80">
        <span className="tabular-nums">{eventCount} {eventCount === 1 ? 'event' : 'events'}</span>
        <span className="text-text-muted/30">·</span>
        <span className="uppercase tracking-[0.16em]">{statusLabel}</span>
      </div>
    </div>
  );
}

interface OutputPayloadProps {
  output: unknown;
  truncated: boolean | undefined;
}

function OutputPayload({ output, truncated }: OutputPayloadProps) {
  // Use a stable string form so the collapse decision and the
  // rendered text agree. Object outputs stringify pretty; primitives
  // print verbatim (still wrapped in a <pre>).
  const text = useMemo(() => stringify(output), [output]);
  const [expanded, setExpanded] = useState<boolean>(() => text.length <= COLLAPSE_THRESHOLD);
  const empty = output == null || (typeof text === 'string' && text.trim().length === 0);

  if (empty) {
    return (
      <div className="rounded-md border border-border-subtle bg-surface/40 px-3 py-3">
        <Caption>output</Caption>
        <p className="font-sans text-text-muted text-xs leading-snug">
          run produced no output payload.
        </p>
        {truncated && <TruncationPill />}
      </div>
    );
  }

  const tooLong = text.length > COLLAPSE_THRESHOLD;

  return (
    <div>
      <div className="flex items-baseline justify-between mb-1.5">
        <Caption>output</Caption>
        <div className="flex items-center gap-2">
          {truncated && <TruncationPill />}
          <ByteCount bytes={text.length} />
        </div>
      </div>
      <pre
        className={cn(
          'p-3 rounded-md border border-border-subtle bg-surface',
          'font-mono text-[11px] leading-relaxed text-text-primary',
          'whitespace-pre-wrap break-words',
          'max-h-[60vh] overflow-y-auto',
          !expanded && 'max-h-32 overflow-hidden relative',
        )}
      >
        {expanded ? text : preview(text)}
        {!expanded && (
          <span
            aria-hidden
            className="pointer-events-none absolute inset-x-0 bottom-0 h-10 bg-gradient-to-t from-surface to-transparent"
          />
        )}
      </pre>
      {tooLong && (
        <button
          type="button"
          onClick={() => setExpanded((v) => !v)}
          className={cn(
            'mt-1.5 inline-flex items-center gap-1.5 px-2 py-1 rounded',
            'font-mono text-[10px] uppercase tracking-[0.18em]',
            'text-text-muted hover:text-text-primary',
            'border border-border-subtle hover:border-border',
            'focus:outline-none focus-visible:ring-1 focus-visible:ring-primary/60',
          )}
        >
          {expanded ? '⌃ collapse' : `⌄ expand · ${text.length.toLocaleString()} chars`}
        </button>
      )}
    </div>
  );
}

function ErrorPayload({ message }: { message: string }) {
  return (
    <div>
      <Caption className="text-status-error/80">error</Caption>
      <pre
        className={cn(
          'mt-1.5 p-3 rounded-md',
          'bg-status-error/5 border border-status-error/20 text-status-error',
          'font-mono text-[11px] leading-relaxed whitespace-pre-wrap break-words',
        )}
      >
        {message}
      </pre>
    </div>
  );
}

function Caption({ children, className }: { children: React.ReactNode; className?: string }) {
  return (
    <span
      className={cn(
        'font-sans text-text-muted text-[10px] uppercase tracking-[0.3em]',
        className,
      )}
    >
      {children}
    </span>
  );
}

function ByteCount({ bytes }: { bytes: number }) {
  return (
    <span className="font-mono text-[10px] text-text-muted/70 tabular-nums">
      {bytes.toLocaleString()} chars
    </span>
  );
}

function TruncationPill() {
  return (
    <span
      title={`truncated to ${TRUNCATION_HINT_BYTES.toLocaleString()} bytes`}
      className={cn(
        'inline-flex items-center px-1.5 py-px rounded',
        'font-mono text-[9px] uppercase tracking-[0.18em]',
        'bg-status-pending/15 text-status-pending border border-status-pending/30',
      )}
    >
      truncated
    </span>
  );
}

function stringify(value: unknown): string {
  if (value === undefined) return '';
  if (value === null) return 'null';
  if (typeof value === 'string') return value;
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}

function preview(text: string): string {
  // Cap the rendered preview so an expand-required output doesn't
  // shovel half a megabyte of JSON into the DOM behind the gradient.
  if (text.length <= 2_000) return text;
  return text.slice(0, 2_000);
}
