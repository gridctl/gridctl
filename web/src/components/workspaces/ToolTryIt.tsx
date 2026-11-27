import {
  lazy,
  Suspense,
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent,
} from 'react';
import { createPortal } from 'react-dom';
import { ChevronDown, ChevronRight, Play, Square } from 'lucide-react';
import { cn } from '../../lib/cn';
import {
  callTool,
  toolCallBodyBytes,
  toolCallRequestBody,
  ToolCallError,
  TOOL_CALL_MAX_BYTES,
  type PolicyToolStatus,
  type ToolCallEnvelope,
} from '../../lib/api';
import { AuthError, GatewayRequestError } from '../../lib/gatewayRequest';
import { describeError } from '../../lib/describeError';
import { TOOL_NAME_DELIMITER } from '../../lib/constants';
import { policyDetailText } from '../../lib/groups';
import { escapeNonPrintable } from '../../lib/nonPrintable';
import { formatLastUsed } from '../../lib/toolAudit';
import {
  argsFromFieldValues,
  canonicalJSON,
  deriveFields,
  fieldValueFromArg,
  fieldValuesFromArgs,
  hasUncheckedKeywords,
  topLevelName,
  validateArguments,
  type FieldValues,
  type FormField,
} from '../../lib/toolSchemaForm';
import { escapeForDisplay, formatElapsed, outcomeLine } from '../../lib/toolCallPresentation';
import { useAuthStore } from '../../stores/useAuthStore';
import { useToolCallHistoryStore, type ToolCallHistoryEntry } from '../../stores/useToolCallHistoryStore';
import { ConfirmDialog } from '../ui/ConfirmDialog';
import { Field } from '../ui/Field';
import { ToolCallResult } from './ToolCallResult';
import type { ToolRow } from '../../hooks/useToolsEditor';

// CodeMirror is heavy; load it only when the JSON editor is shown.
const JsonValueEditor = lazy(() => import('../vault/JsonValueEditor'));

export const CANCELLED_MESSAGE = 'Cancelled. The request was dropped; the tool may still have run.';
const CONNECTION_MESSAGE = 'Could not reach the gateway. The call may or may not have been received.';
const UNCHECKED_NOTE = 'Parts of this schema are not checked in the browser. The server validates the call.';
const DRAFT_NOTE = 'Unsaved allow-list changes are not applied. Calls use the saved allow-list.';
const CONFIRM_ARGS_CAP = 2_000;
const HISTORY_PREVIEW_CHARS = 60;

const NO_HISTORY: ToolCallHistoryEntry[] = [];

const INPUT_CLASS = cn(
  'w-full rounded-md border border-border bg-surface px-2 py-1 text-2xs text-text-primary font-mono',
  'aria-[invalid=true]:border-status-error read-only:opacity-70',
);
const SMALL_ACTION_CLASS =
  'text-3xs text-secondary hover:text-secondary-light transition-colors rounded disabled:opacity-50 disabled:cursor-not-allowed';

type Phase = 'idle' | 'running' | 'done' | 'cancelled' | 'failed';
type Mode = 'form' | 'json';

interface ToolTryItProps {
  serverName: string;
  tool: ToolRow;
  schema?: Record<string, unknown>;
  // Whether the saved allow-list exposes the tool. Never the draft.
  exposed: boolean;
  // Whether the unsaved draft differs from the saved allow-list for this tool.
  draftDiffers: boolean;
  policy?: PolicyToolStatus;
  fontSize: number;
  onNavigate?: (path: string) => void;
}

function isObject(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v);
}

function failureMessage(err: unknown): string {
  if (err instanceof AuthError) {
    return 'The gateway rejected the credential. Verify it to resume; the call was not retried.';
  }
  if (err instanceof GatewayRequestError) {
    if (err.kind === 'connection') return CONNECTION_MESSAGE;
    if (err.kind === 'stale') {
      return 'The gateway credential changed during the call. The tool may or may not have run; the call was not retried.';
    }
  }
  if (err instanceof ToolCallError) return err.message;
  return describeError(err, 'Tool call').text;
}

function confirmDeclaration(annotations: ToolRow['annotations']): string {
  if (annotations?.destructiveHint === true) return 'The server declares this tool destructive.';
  if (!annotations || Object.keys(annotations).length === 0) {
    return 'The server declared no annotations, so MCP treats this tool as potentially destructive.';
  }
  return 'The server did not declare whether this tool is destructive, so MCP treats it as potentially destructive.';
}

function historyPreview(args: Record<string, unknown>): string {
  const text = escapeNonPrintable(canonicalJSON(args));
  return text.length > HISTORY_PREVIEW_CHARS ? `${text.slice(0, HISTORY_PREVIEW_CHARS)}…` : text;
}

// ToolTryIt is the Tools detail rail's "Try it" section: it builds an argument
// object from a form or raw JSON, validates it against a schema subset, and
// sends it through POST /api/tools/call with the fixed `ui` caller label. The
// gateway's gates all apply; this section adds no way around them.
export function ToolTryIt({
  serverName,
  tool,
  schema,
  exposed,
  draftDiffers,
  policy,
  fontSize,
  onNavigate,
}: ToolTryItProps) {
  const prefixed = `${serverName}${TOOL_NAME_DELIMITER}${tool.name}`;
  const open = useToolCallHistoryStore((s) => s.tryItOpen);
  const setOpen = useToolCallHistoryStore((s) => s.setTryItOpen);
  const history = useToolCallHistoryStore((s) => s.byTool[prefixed]) ?? NO_HISTORY;
  const pushHistory = useToolCallHistoryStore((s) => s.push);
  const authRequired = useAuthStore((s) => s.authRequired);

  const model = useMemo(() => deriveFields(schema), [schema]);
  const unchecked = useMemo(() => hasUncheckedKeywords(schema), [schema]);
  const hasForm = schema !== undefined && model.formable;
  const anyDefaults = model.fields.some((f) => f.hasDefault);

  const [mode, setMode] = useState<Mode>(hasForm && model.fields.length > 0 ? 'form' : 'json');
  const [values, setValues] = useState<FieldValues>({});
  const [extra, setExtra] = useState<Record<string, unknown>>({});
  const [multiline, setMultiline] = useState<Record<string, boolean>>({});
  const [jsonText, setJsonText] = useState('{}');
  const [touched, setTouched] = useState<Record<string, boolean>>({});
  const [attempted, setAttempted] = useState(false);
  const [modeError, setModeError] = useState<string | null>(null);
  const [localError, setLocalError] = useState<string | null>(null);
  const [phase, setPhase] = useState<Phase>('idle');
  const [envelope, setEnvelope] = useState<ToolCallEnvelope | null>(null);
  const [elapsedMs, setElapsedMs] = useState(0);
  const [failure, setFailure] = useState<string | null>(null);
  const [confirmArgs, setConfirmArgs] = useState<Record<string, unknown> | null>(null);
  const [startedAt, setStartedAt] = useState(0);
  const [now, setNow] = useState(0);

  const regionId = useId();
  const reasonId = useId();
  const jsonErrorsId = useId();
  const historyId = useId();

  // A per-call token: a cancelled, superseded, or unmounted call never writes.
  const tokenRef = useRef<symbol | null>(null);
  const abortRef = useRef<AbortController | null>(null);
  const startedRef = useRef(0);
  const fieldRefs = useRef<Record<string, HTMLElement | null>>({});
  const editorWrapRef = useRef<HTMLDivElement | null>(null);

  const running = phase === 'running';

  useEffect(() => {
    if (!running) return;
    const timer = setInterval(() => setNow(Date.now()), 100);
    return () => clearInterval(timer);
  }, [running]);

  useEffect(() => {
    const tokens = tokenRef;
    const aborts = abortRef;
    return () => {
      tokens.current = null;
      aborts.current?.abort();
    };
  }, []);

  const formState = useMemo(() => {
    const built = argsFromFieldValues(model, values, extra);
    const checked = validateArguments(schema, built.args);
    return { args: built.args, errors: { ...checked.errors, ...built.errors } };
  }, [model, values, extra, schema]);

  const jsonState = useMemo((): { args: Record<string, unknown> | null; errors: Record<string, string> } => {
    let parsed: unknown;
    try {
      parsed = JSON.parse(jsonText);
    } catch {
      return { args: null, errors: { '': 'Arguments must be valid JSON' } };
    }
    if (!isObject(parsed)) return { args: null, errors: { '': 'Arguments must be a JSON object' } };
    return { args: parsed, errors: validateArguments(schema, parsed).errors };
  }, [jsonText, schema]);

  // First error per top-level field; nested paths keep their suffix.
  const fieldErrors = useMemo(() => {
    const out: Record<string, string> = {};
    for (const [pointer, message] of Object.entries(formState.errors)) {
      const name = topLevelName(pointer) ?? '';
      if (name in out) continue;
      const rest = pointer.split('/').slice(2).join('/');
      out[name] = rest ? `At /${rest}: ${message}` : message;
    }
    return out;
  }, [formState.errors]);

  const fieldNames = useMemo(() => new Set(model.fields.map((f) => f.name)), [model]);
  const extraErrors = Object.entries(fieldErrors).filter(([name]) => !fieldNames.has(name));

  const blockedReason = !exposed
    ? `Not exposed. This tool is hidden by the saved allow-list for ${serverName}, so the gateway rejects calls to it.`
    : policy && policy.action !== 'allow'
      ? policyDetailText(policy)
      : authRequired
        ? 'Calls are paused until the gateway credential is verified.'
        : null;

  const annotations = tool.annotations;
  const needsConfirm = !(annotations?.readOnlyHint === true || annotations?.destructiveHint === false);

  const focusFirstInvalid = (errors: Record<string, string>) => {
    if (mode === 'json') {
      editorWrapRef.current?.querySelector<HTMLElement>('textarea, [contenteditable="true"]')?.focus();
      return;
    }
    const names = new Set(Object.keys(errors).map((p) => topLevelName(p) ?? ''));
    const first = model.fields.find((f) => names.has(f.name));
    if (first) fieldRefs.current[first.name]?.focus();
  };

  const send = async (args: Record<string, unknown>) => {
    if (toolCallBodyBytes(toolCallRequestBody(prefixed, args)) > TOOL_CALL_MAX_BYTES) {
      setLocalError('The arguments are larger than the 2 MiB request limit. Nothing was sent.');
      return;
    }
    const controller = new AbortController();
    abortRef.current = controller;
    const token = Symbol('tool-call');
    tokenRef.current = token;
    const started = Date.now();
    startedRef.current = started;
    setStartedAt(started);
    setNow(started);
    setFailure(null);
    setPhase('running');
    pushHistory(prefixed, args);
    try {
      const env = await callTool(prefixed, args, { signal: controller.signal });
      if (tokenRef.current !== token) return;
      tokenRef.current = null;
      setEnvelope(env);
      setElapsedMs(Date.now() - started);
      setPhase('done');
    } catch (err) {
      if (tokenRef.current !== token) return;
      tokenRef.current = null;
      setEnvelope(null);
      setElapsedMs(Date.now() - started);
      setFailure(failureMessage(err));
      setPhase('failed');
    }
  };

  const cancel = () => {
    if (!tokenRef.current) return;
    tokenRef.current = null;
    abortRef.current?.abort();
    setElapsedMs(Date.now() - startedRef.current);
    setEnvelope(null);
    setPhase('cancelled');
  };

  const run = () => {
    if (running || blockedReason || confirmArgs) return;
    setLocalError(null);
    setAttempted(true);
    const current = mode === 'form' ? formState : jsonState;
    if (!current.args || Object.keys(current.errors).length > 0) {
      focusFirstInvalid(current.errors);
      return;
    }
    if (needsConfirm) {
      setConfirmArgs(current.args);
      return;
    }
    void send(current.args);
  };

  // The editor keymap is built once per callback identity; keep it stable.
  const runRef = useRef(run);
  useLayoutEffect(() => {
    runRef.current = run;
  });
  const submitFromEditor = useCallback(() => runRef.current(), []);

  const handleKeyDown = (e: KeyboardEvent<HTMLElement>) => {
    if (phase !== 'running' || e.key !== 'Escape') return;
    e.stopPropagation();
    cancel();
  };

  const switchToJSON = () => {
    const built = argsFromFieldValues(model, values, extra);
    const bad = Object.keys(built.errors);
    if (bad.length > 0) {
      setTouched((t) => ({ ...t, ...Object.fromEntries(bad.map((p) => [topLevelName(p) ?? '', true])) }));
      setModeError('Fix the highlighted fields before switching to JSON.');
      return;
    }
    setJsonText(JSON.stringify(built.args, null, 2));
    setModeError(null);
    setMode('json');
  };

  const switchToForm = () => {
    if (!jsonState.args) {
      setModeError('The JSON must be a valid object before switching to Form.');
      return;
    }
    const next = fieldValuesFromArgs(model, jsonState.args);
    setValues(next.values);
    setExtra(next.extra);
    setModeError(null);
    setMode('form');
  };

  const loadHistory = (entry: ToolCallHistoryEntry) => {
    setModeError(null);
    if (mode === 'json') {
      setJsonText(JSON.stringify(entry.args, null, 2));
      return;
    }
    const next = fieldValuesFromArgs(model, entry.args);
    setValues(next.values);
    setExtra(next.extra);
  };

  const insertDefaults = () => {
    setValues((current) => {
      const next = { ...current };
      for (const field of model.fields) {
        if (!field.hasDefault || next[field.name] !== undefined) continue;
        const converted = fieldValueFromArg(field, field.default);
        if (converted !== null) next[field.name] = converted;
      }
      return next;
    });
  };

  const setField = (name: string, value: string | undefined) =>
    setValues((current) => {
      const next = { ...current };
      if (value === undefined) delete next[name];
      else next[name] = value;
      return next;
    });

  const statusText =
    phase === 'running'
      ? 'Running'
      : phase === 'done' && envelope
        ? outcomeLine(envelope, elapsedMs)
        : phase === 'cancelled'
          ? CANCELLED_MESSAGE
          : phase === 'failed'
            ? failure ?? ''
            : '';

  const jsonErrors = Object.entries(jsonState.errors);
  const Chevron = open ? ChevronDown : ChevronRight;

  return (
    <section className="space-y-2" onKeyDown={handleKeyDown} aria-label="Try it">
      <h3 className="text-3xs uppercase tracking-[0.18em] text-text-muted">
        <button
          type="button"
          aria-expanded={open}
          aria-controls={regionId}
          onClick={() => setOpen(!open)}
          className="inline-flex items-center gap-1 uppercase tracking-[0.18em] hover:text-text-secondary transition-colors rounded"
        >
          <Chevron size={12} aria-hidden="true" />
          Try it
        </button>
      </h3>
      {!open && <p className="text-2xs text-text-muted">Call this tool through the gateway</p>}
      <div id={regionId} hidden={!open} className="space-y-3">
        {open && (
          <>
            {draftDiffers && <p className="text-3xs text-status-pending leading-relaxed">{DRAFT_NOTE}</p>}
            {unchecked && <p className="text-3xs text-text-muted leading-relaxed">{UNCHECKED_NOTE}</p>}
            {hasForm && model.fields.length === 0 && (
              <p className="text-2xs text-text-muted">This tool takes no arguments</p>
            )}

            <div className="flex items-center gap-2 flex-wrap">
              {hasForm && (
                <div role="group" aria-label="Argument editor" className="inline-flex rounded-md border border-border overflow-hidden">
                  {(['form', 'json'] as const).map((m) => (
                    <button
                      key={m}
                      type="button"
                      aria-pressed={mode === m}
                      disabled={running}
                      onClick={() => (m === 'json' ? mode !== 'json' && switchToJSON() : mode !== 'form' && switchToForm())}
                      className={cn(
                        'px-2 py-0.5 text-3xs transition-colors disabled:cursor-not-allowed',
                        mode === m ? 'bg-primary/15 text-primary' : 'text-text-secondary hover:text-text-primary',
                      )}
                    >
                      {m === 'form' ? 'Form' : 'JSON'}
                    </button>
                  ))}
                </div>
              )}
              {mode === 'form' && anyDefaults && (
                <button type="button" onClick={insertDefaults} disabled={running} className={SMALL_ACTION_CLASS}>
                  Insert defaults
                </button>
              )}
              <label htmlFor={historyId} className="sr-only">
                History
              </label>
              <select
                id={historyId}
                value=""
                disabled={history.length === 0 || running}
                onChange={(e) => {
                  const entry = history[Number(e.target.value)];
                  if (entry) loadHistory(entry);
                }}
                className="ml-auto max-w-[60%] rounded-md border border-border bg-surface px-1.5 py-0.5 text-3xs text-text-secondary disabled:opacity-50"
              >
                <option value="">{history.length > 0 ? 'History' : 'No history'}</option>
                {history.map((entry, i) => (
                  <option key={`${entry.at}-${i}`} value={i}>
                    {historyPreview(entry.args)} · {formatLastUsed(new Date(entry.at).toISOString())}
                  </option>
                ))}
              </select>
            </div>
            {modeError && <p className="text-3xs text-status-error">{modeError}</p>}

            {mode === 'form' ? (
              <div className="space-y-3">
                {model.fields.map((field) => (
                  <FormFieldInput
                    key={field.name}
                    field={field}
                    value={values[field.name]}
                    error={attempted || touched[field.name] ? fieldErrors[field.name] : undefined}
                    multiline={Boolean(multiline[field.name])}
                    readOnly={running}
                    onChange={(v) => setField(field.name, v)}
                    onBlur={() => setTouched((t) => (t[field.name] ? t : { ...t, [field.name]: true }))}
                    onToggleMultiline={() => setMultiline((m) => ({ ...m, [field.name]: !m[field.name] }))}
                    onEnter={run}
                    inputRef={(el) => {
                      fieldRefs.current[field.name] = el;
                    }}
                  />
                ))}
                {Object.keys(extra).length > 0 && (
                  <div className="space-y-1">
                    <p className="text-3xs uppercase tracking-[0.18em] text-text-muted">Additional arguments</p>
                    <pre className="rounded-md border border-border/30 bg-background/80 px-2 py-1.5 text-3xs font-mono text-text-secondary whitespace-pre-wrap break-words">
                      {escapeForDisplay(JSON.stringify(extra, null, 2))}
                    </pre>
                    <p className="text-3xs text-text-muted">
                      Not in the schema form. Sent unchanged; edit them in JSON.
                    </p>
                    {attempted &&
                      extraErrors.map(([name, message]) => (
                        <p key={name} className="text-3xs text-status-error">
                          {escapeNonPrintable(name)}: {message}
                        </p>
                      ))}
                  </div>
                )}
              </div>
            ) : (
              <div className="space-y-1">
                <div ref={editorWrapRef}>
                  <Suspense fallback={<p className="text-2xs text-text-muted">Loading editor…</p>}>
                    <JsonValueEditor
                      value={jsonText}
                      onChange={setJsonText}
                      onSubmit={submitFromEditor}
                      readOnly={running}
                      ariaLabel="Arguments JSON"
                      compact
                    />
                  </Suspense>
                </div>
                {attempted && jsonErrors.length > 0 && (
                  <ul id={jsonErrorsId} aria-label="Argument errors" className="space-y-0.5">
                    {jsonErrors.map(([pointer, message]) => (
                      <li key={pointer} className="text-3xs text-status-error">
                        {pointer ? `${escapeNonPrintable(pointer)}: ${message}` : message}
                      </li>
                    ))}
                  </ul>
                )}
              </div>
            )}

            <div className="flex items-start gap-2 flex-wrap">
              <button
                type="button"
                onClick={running ? cancel : run}
                disabled={!running && Boolean(blockedReason)}
                aria-describedby={!running && blockedReason ? reasonId : undefined}
                className={cn(
                  'inline-flex items-center gap-1.5 px-3 py-1 text-2xs font-medium rounded-md transition-colors',
                  'disabled:opacity-50 disabled:cursor-not-allowed',
                  running
                    ? 'bg-surface-highlight text-text-primary hover:bg-surface-highlight/70'
                    : 'bg-primary text-background hover:bg-primary-light',
                )}
              >
                {running ? <Square size={10} aria-hidden="true" /> : <Play size={10} aria-hidden="true" />}
                {running ? 'Cancel' : 'Run'}
              </button>
              {running && (
                <span className="text-3xs font-mono text-text-muted mt-1">
                  Running · {formatElapsed(Math.max(0, now - startedAt))}
                </span>
              )}
              {!running && blockedReason && (
                <p id={reasonId} className="flex-1 min-w-0 text-3xs text-text-secondary leading-relaxed mt-1">
                  {blockedReason}
                </p>
              )}
            </div>

            <p className="sr-only" role="status" aria-live="polite">
              {statusText}
            </p>

            {localError && <p className="text-2xs text-status-error">{localError}</p>}
            {phase === 'cancelled' && <p className="text-2xs text-status-pending">{CANCELLED_MESSAGE}</p>}
            {phase === 'failed' && failure && <p className="text-2xs text-status-error">{failure}</p>}
            {envelope && (phase === 'done' || phase === 'running') && (
              <ToolCallResult
                envelope={envelope}
                elapsedMs={elapsedMs}
                stale={phase === 'running'}
                serverName={serverName}
                toolName={tool.name}
                fontSize={fontSize}
                onNavigate={onNavigate}
              />
            )}
          </>
        )}
      </div>
      {confirmArgs &&
        createPortal(
          <ConfirmDialog
            isOpen
            variant="danger"
            title="Run this tool?"
            confirmLabel="Run"
            onClose={() => setConfirmArgs(null)}
            onConfirm={() => {
              const args = confirmArgs;
              setConfirmArgs(null);
              void send(args);
            }}
            message={<ConfirmMessage prefixed={prefixed} annotations={annotations} args={confirmArgs} />}
          />,
          document.body,
        )}
    </section>
  );
}

function ConfirmMessage({
  prefixed,
  annotations,
  args,
}: {
  prefixed: string;
  annotations: ToolRow['annotations'];
  args: Record<string, unknown>;
}) {
  const full = escapeForDisplay(JSON.stringify(args, null, 2));
  const truncated = full.length > CONFIRM_ARGS_CAP;
  return (
    <>
      <p>
        Call <code className="font-mono text-text-primary break-all">{escapeNonPrintable(prefixed)}</code> through
        the gateway.
      </p>
      <p>{confirmDeclaration(annotations)}</p>
      <pre
        aria-label="Arguments"
        className="max-h-48 overflow-auto rounded-md border border-border/30 bg-background/80 px-2 py-1.5 text-3xs font-mono text-text-secondary whitespace-pre-wrap break-words"
      >
        {truncated ? full.slice(0, CONFIRM_ARGS_CAP) : full}
      </pre>
      {truncated && (
        <p>
          Showing the first {CONFIRM_ARGS_CAP.toLocaleString('en-US')} of {full.length.toLocaleString('en-US')}{' '}
          characters.
        </p>
      )}
    </>
  );
}

function enumOptionLabel(value: unknown): string {
  return escapeNonPrintable(typeof value === 'string' ? value : JSON.stringify(value));
}

function FormFieldInput({
  field,
  value,
  error,
  multiline,
  readOnly,
  onChange,
  onBlur,
  onToggleMultiline,
  onEnter,
  inputRef,
}: {
  field: FormField;
  value: string | undefined;
  error?: string;
  multiline: boolean;
  readOnly: boolean;
  onChange: (value: string | undefined) => void;
  onBlur: () => void;
  onToggleMultiline: () => void;
  onEnter: () => void;
  inputRef: (el: HTMLElement | null) => void;
}) {
  const defaultText = field.hasDefault ? JSON.stringify(field.default) ?? '' : undefined;
  const help =
    field.description || defaultText !== undefined ? (
      <>
        {field.description && <span className="block whitespace-pre-wrap break-words">{field.description}</span>}
        {defaultText !== undefined && (
          <span className="block break-words">Default: {escapeNonPrintable(defaultText)} (sent only if set)</span>
        )}
      </>
    ) : undefined;
  const placeholder = defaultText !== undefined ? `Default: ${defaultText}` : undefined;
  const onSingleLineKey = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter' && !e.nativeEvent.isComposing) {
      e.preventDefault();
      onEnter();
    }
  };

  return (
    <Field
      label={
        <>
          <span className="font-mono text-text-primary break-all">{field.name}</span>{' '}
          <span className="font-mono text-text-muted">{field.typeLabel}</span>
        </>
      }
      required={field.required}
      help={help}
      error={error}
      labelClassName="block text-2xs text-text-secondary mb-1"
    >
      {({ id, describedBy, invalid }) => {
        const common = {
          id,
          'aria-describedby': describedBy,
          'aria-invalid': invalid,
          'aria-required': field.required || undefined,
          onBlur,
        };
        switch (field.kind) {
          case 'string':
            return (
              <div className="space-y-1">
                {multiline ? (
                  <textarea
                    {...common}
                    ref={inputRef}
                    rows={4}
                    readOnly={readOnly}
                    value={value ?? ''}
                    placeholder={placeholder}
                    onChange={(e) => onChange(e.target.value === '' ? undefined : e.target.value)}
                    className={cn(INPUT_CLASS, 'resize-y')}
                  />
                ) : (
                  <input
                    {...common}
                    ref={inputRef}
                    type="text"
                    readOnly={readOnly}
                    value={value ?? ''}
                    placeholder={placeholder}
                    onChange={(e) => onChange(e.target.value === '' ? undefined : e.target.value)}
                    onKeyDown={onSingleLineKey}
                    className={INPUT_CLASS}
                  />
                )}
                <div className="flex items-center gap-3">
                  <button
                    type="button"
                    aria-pressed={multiline}
                    onClick={onToggleMultiline}
                    className={SMALL_ACTION_CLASS}
                  >
                    {multiline ? 'Single line' : 'Multiline'}
                  </button>
                  {field.required && (
                    <button
                      type="button"
                      aria-pressed={value === ''}
                      disabled={readOnly}
                      onClick={() => onChange(value === '' ? undefined : '')}
                      className={SMALL_ACTION_CLASS}
                    >
                      Send empty string
                    </button>
                  )}
                </div>
              </div>
            );
          case 'number':
          case 'integer':
            return (
              <input
                {...common}
                ref={inputRef}
                type="text"
                inputMode={field.kind === 'integer' ? 'numeric' : 'decimal'}
                readOnly={readOnly}
                value={value ?? ''}
                placeholder={placeholder}
                onChange={(e) => onChange(e.target.value === '' ? undefined : e.target.value)}
                onKeyDown={onSingleLineKey}
                className={INPUT_CLASS}
              />
            );
          case 'boolean':
            return (
              <select
                {...common}
                ref={inputRef}
                disabled={readOnly}
                value={value ?? ''}
                onChange={(e) => onChange(e.target.value === '' ? undefined : e.target.value)}
                className={INPUT_CLASS}
              >
                {(!field.required || value === undefined) && (
                  <option value="">{field.required ? 'Choose true or false' : 'unset'}</option>
                )}
                <option value="true">true</option>
                <option value="false">false</option>
              </select>
            );
          case 'enum':
            return (
              <select
                {...common}
                ref={inputRef}
                disabled={readOnly}
                value={value ?? ''}
                onChange={(e) => onChange(e.target.value === '' ? undefined : e.target.value)}
                className={INPUT_CLASS}
              >
                {(!field.required || value === undefined) && (
                  <option value="">{field.required ? 'Choose a value' : 'unset'}</option>
                )}
                {(field.enumValues ?? []).map((option, i) => (
                  <option key={i} value={String(i)}>
                    {enumOptionLabel(option)}
                  </option>
                ))}
              </select>
            );
          case 'json':
            return (
              <textarea
                {...common}
                ref={inputRef}
                rows={3}
                readOnly={readOnly}
                value={value ?? ''}
                placeholder={placeholder ?? 'JSON value'}
                onChange={(e) => onChange(e.target.value.trim() === '' ? undefined : e.target.value)}
                className={cn(INPUT_CLASS, 'resize-y')}
              />
            );
        }
      }}
    </Field>
  );
}
