import {
  lazy,
  Suspense,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent,
  type RefObject,
} from 'react';
import { X, Play, AlertCircle } from 'lucide-react';
import { cn } from '../../../lib/cn';
import { formatRelativeTime } from '../../../lib/time';
import {
  fetchAgentRun,
  inputFromRunDetail,
  launchRun,
  LaunchRunError,
  type LaunchRunResponse,
} from '../../../lib/agent-runs';
import { useRunsForSkill } from './useRunsForSkill';

// RJSF is heavy (~90KB gzipped with AJV) and only renders when a skill
// exposes an inputSchema with properties. Lazy-load it so the main
// bundle is unaffected; the chunk only ships when the user clicks the
// Form tab.
const SchemaForm = lazy(() => import('./SchemaForm'));

const LS_PREFIX = 'gridctl.agent.lastInput.';
const RUN_HISTORY_LIMIT = 10;

type Tab = 'json' | 'form';

export interface SkillForLaunch {
  name: string;
  description?: string;
  // inputSchema defaults to {type:"object"} when omitted. Today every
  // TS skill the registry exposes lands here as the default; richer
  // schemas will arrive when TS-schema extraction lands and will
  // automatically activate the Form tab.
  inputSchema?: Record<string, unknown>;
}

interface RunLauncherModalProps {
  skill: SkillForLaunch;
  onClose: () => void;
  onLaunched: (response: LaunchRunResponse) => void;
  returnFocusRef?: RefObject<HTMLElement | null>;
}

/**
 * RunLauncherModal is the per-skill run-start surface. It opens on a
 * Run-button click, lets the operator supply input as JSON (raw
 * textarea — primary) or via a schema-generated Form (progressive
 * enhancement when the skill exposes properties), and POSTs to
 * /api/agent/runs.
 *
 * The modal is focus-trapped — Tab cycles through the focusable
 * controls, ESC closes, focus returns to the originating Run button.
 * The launcher mutates runtime state only; SKILL source files are
 * never touched.
 */
export function RunLauncherModal({
  skill,
  onClose,
  onLaunched,
  returnFocusRef,
}: RunLauncherModalProps) {
  const [activeTab, setActiveTab] = useState<Tab>('json');
  const [jsonText, setJsonText] = useState<string>(() => readInitialInput(skill.name));
  const [parseError, setParseError] = useState<string | null>(null);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [selectedRunID, setSelectedRunID] = useState<string>('');

  // Recent runs of THIS skill for the "Run like…" picker. The shared
  // hook handles the filter; the modal stays silent on failure (the
  // dropdown simply won't populate — Run like… is optional).
  const { runs: recentRuns, loading: recentLoading } = useRunsForSkill({
    skillName: skill.name,
    limit: RUN_HISTORY_LIMIT,
    fetchLimit: 50,
  });

  const overlayRef = useRef<HTMLDivElement>(null);
  const dialogRef = useRef<HTMLDivElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  const hasRichSchema = useMemo(() => schemaHasProperties(skill.inputSchema), [skill.inputSchema]);

  // Debounced parse — keep the textarea snappy while still flagging
  // bad JSON before the operator hits Run.
  useEffect(() => {
    const handle = window.setTimeout(() => {
      setParseError(validateJSON(jsonText));
    }, 200);
    return () => window.clearTimeout(handle);
  }, [jsonText]);

  // Move focus into the dialog when it mounts; remember the previously
  // focused element so we can return it on close. Capture the return
  // target on mount so the cleanup uses a stable reference even if the
  // ref's current value changes during the modal's lifetime.
  useEffect(() => {
    const previouslyFocused = document.activeElement as HTMLElement | null;
    const returnTarget = returnFocusRef?.current ?? previouslyFocused;
    // Defer one frame so the textarea has rendered.
    const handle = window.requestAnimationFrame(() => {
      textareaRef.current?.focus();
      textareaRef.current?.select();
    });
    return () => {
      window.cancelAnimationFrame(handle);
      returnTarget?.focus?.();
    };
  }, [returnFocusRef]);

  // ESC closes; Tab is trapped within the dialog so focus cannot
  // escape to the underlying IDE chrome.
  const handleKeyDown = useCallback(
    (e: KeyboardEvent<HTMLDivElement>) => {
      if (e.key === 'Escape') {
        e.stopPropagation();
        onClose();
        return;
      }
      if (e.key === 'Tab' && dialogRef.current) {
        trapTab(e, dialogRef.current);
      }
    },
    [onClose],
  );

  const handleSelectRecent = useCallback(
    async (runID: string) => {
      setSelectedRunID(runID);
      if (!runID) return;
      try {
        const detail = await fetchAgentRun(runID);
        const input = inputFromRunDetail(detail);
        setJsonText(JSON.stringify(input, null, 2));
      } catch (err) {
        setSubmitError(
          err instanceof Error
            ? `couldn't load run ${runID.slice(0, 8)}: ${err.message}`
            : `couldn't load run ${runID.slice(0, 8)}`,
        );
      }
    },
    [],
  );

  const handleSubmit = useCallback(async () => {
    setSubmitError(null);
    const parsed = tryParse(jsonText);
    if (parsed.kind === 'error') {
      setParseError(parsed.message);
      return;
    }
    setSubmitting(true);
    try {
      const response = await launchRun({ skill_name: skill.name, input: parsed.value });
      // Persist the last-used input so the next open of this skill's
      // modal pre-fills it. Stored as the raw text the operator typed,
      // not the parsed-and-restringified form, so formatting survives.
      try {
        window.localStorage.setItem(LS_PREFIX + skill.name, jsonText);
      } catch {
        // localStorage may be unavailable / over quota; non-fatal.
      }
      onLaunched(response);
    } catch (err) {
      const message =
        err instanceof LaunchRunError
          ? err.message
          : err instanceof Error
          ? err.message
          : String(err);
      setSubmitError(message);
    } finally {
      setSubmitting(false);
    }
  }, [jsonText, skill.name, onLaunched]);

  const submitDisabled = submitting || parseError !== null;

  return (
    <div
      ref={overlayRef}
      role="presentation"
      className="fixed inset-0 z-40 flex items-center justify-center px-4 py-8 bg-black/60 backdrop-blur-sm animate-fade-in-scale"
      onMouseDown={(e) => {
        // Click on the backdrop (not bubbled from inside the dialog)
        // dismisses, mirroring native <dialog> semantics.
        if (e.target === overlayRef.current) onClose();
      }}
      onKeyDown={handleKeyDown}
    >
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby="run-launcher-title"
        className={cn(
          'relative w-full max-w-2xl max-h-full flex flex-col',
          'rounded-lg border border-border-subtle bg-background shadow-2xl',
          'overflow-hidden',
        )}
      >
        <header className="px-5 py-4 border-b border-border-subtle">
          <div className="flex items-start gap-3">
            <div className="flex-1 min-w-0">
              <div className="font-sans text-text-muted/70 text-[10px] uppercase tracking-[0.3em] mb-1">
                launch run
              </div>
              <h2
                id="run-launcher-title"
                className="font-mono text-base text-text-primary truncate"
              >
                {skill.name}
              </h2>
              {skill.description && (
                <p className="font-sans text-text-muted text-xs mt-1 leading-snug line-clamp-2">
                  {skill.description}
                </p>
              )}
            </div>
            <button
              type="button"
              aria-label="Close"
              onClick={onClose}
              className="text-text-muted hover:text-text-primary p-1 rounded focus:outline-none focus:ring-1 focus:ring-primary/60"
            >
              <X size={16} />
            </button>
          </div>
        </header>

        <div className="px-5 py-3 border-b border-border-subtle bg-surface/20">
          <label className="block font-sans text-text-muted text-[10px] uppercase tracking-[0.3em] mb-1.5">
            run like…
          </label>
          <select
            value={selectedRunID}
            disabled={recentLoading || recentRuns.length === 0}
            onChange={(e) => void handleSelectRecent(e.target.value)}
            className={cn(
              'w-full px-2.5 py-1.5 rounded-md',
              'bg-surface border border-border-subtle text-text-primary',
              'font-mono text-xs',
              'focus:outline-none focus:ring-1 focus:ring-primary/60 focus:border-primary/60',
              'disabled:opacity-50 disabled:cursor-not-allowed',
            )}
          >
            <option value="">
              {recentLoading
                ? 'loading…'
                : recentRuns.length === 0
                ? 'no previous runs'
                : 'start fresh'}
            </option>
            {recentRuns.map((r) => (
              <option key={r.run_id} value={r.run_id}>
                {r.run_id.slice(0, 8)} — {r.status} — {relativeOrDash(r.started_at)}
              </option>
            ))}
          </select>
        </div>

        <div className="px-5 pt-3 flex items-center gap-1 border-b border-border-subtle/50">
          <TabButton
            active={activeTab === 'json'}
            onClick={() => setActiveTab('json')}
            label="JSON"
          />
          {hasRichSchema && (
            <TabButton
              active={activeTab === 'form'}
              onClick={() => setActiveTab('form')}
              label="Form"
            />
          )}
        </div>

        <div className="flex-1 px-5 py-4 overflow-y-auto min-h-0">
          {activeTab === 'json' && (
            <JSONTab
              ref={textareaRef}
              value={jsonText}
              onChange={setJsonText}
              parseError={parseError}
            />
          )}
          {activeTab === 'form' && hasRichSchema && (
            <Suspense
              fallback={
                <div className="font-mono text-xs text-text-muted animate-pulse">
                  loading form…
                </div>
              }
            >
              <SchemaForm
                schema={skill.inputSchema as Record<string, unknown>}
                value={tryParseLoose(jsonText)}
                onChange={(next) => setJsonText(JSON.stringify(next, null, 2))}
              />
            </Suspense>
          )}
        </div>

        {submitError && (
          <div
            role="alert"
            aria-live="polite"
            className="mx-5 mb-3 flex items-start gap-2 px-3 py-2 rounded-md border border-status-error/30 bg-status-error/10 text-status-error text-xs"
          >
            <AlertCircle size={14} className="shrink-0 mt-0.5" />
            <span className="font-mono leading-snug break-words">{submitError}</span>
          </div>
        )}

        <footer className="px-5 py-3 border-t border-border-subtle flex items-center justify-end gap-2 bg-surface/20">
          <button
            type="button"
            onClick={onClose}
            className={cn(
              'px-3 py-1.5 rounded-md font-mono text-xs uppercase tracking-[0.16em]',
              'text-text-muted hover:text-text-primary',
              'border border-transparent hover:border-border-subtle',
              'focus:outline-none focus:ring-1 focus:ring-primary/60',
            )}
          >
            cancel
          </button>
          <button
            type="button"
            onClick={() => void handleSubmit()}
            disabled={submitDisabled}
            className={cn(
              'inline-flex items-center gap-2 px-3 py-1.5 rounded-md',
              'font-mono text-xs uppercase tracking-[0.16em]',
              'border border-primary/40 text-primary-light bg-primary/10',
              'hover:bg-primary/20 hover:border-primary/60 transition-colors',
              'disabled:opacity-50 disabled:cursor-not-allowed',
              'focus:outline-none focus:ring-1 focus:ring-primary/60',
            )}
          >
            <Play size={12} className="translate-x-px" aria-hidden />
            {submitting ? 'starting…' : 'run'}
          </button>
        </footer>
      </div>
    </div>
  );
}

interface JSONTabProps {
  value: string;
  onChange: (next: string) => void;
  parseError: string | null;
}

const JSONTab = function JSONTabImpl({
  value,
  onChange,
  parseError,
  ref,
}: JSONTabProps & { ref?: RefObject<HTMLTextAreaElement | null> }) {
  return (
    <div>
      <label
        htmlFor="run-launcher-json-editor"
        className="block font-sans text-text-muted text-[10px] uppercase tracking-[0.3em] mb-1.5"
      >
        input json
      </label>
      <textarea
        ref={ref ?? null}
        id="run-launcher-json-editor"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        spellCheck={false}
        rows={12}
        aria-invalid={parseError ? 'true' : 'false'}
        aria-describedby={parseError ? 'run-launcher-json-error' : undefined}
        className={cn(
          'w-full px-3 py-2 rounded-md resize-y',
          'bg-surface border font-mono text-xs leading-relaxed text-text-primary',
          parseError ? 'border-status-error/40' : 'border-border-subtle',
          'focus:outline-none focus:ring-1 focus:ring-primary/60 focus:border-primary/60',
        )}
      />
      <div className="min-h-[18px] mt-1.5" aria-live="polite">
        {parseError && (
          <p
            id="run-launcher-json-error"
            className="font-mono text-[11px] text-status-error"
          >
            {parseError}
          </p>
        )}
      </div>
    </div>
  );
};

function TabButton({
  active,
  onClick,
  label,
}: {
  active: boolean;
  onClick: () => void;
  label: string;
}) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      onClick={onClick}
      className={cn(
        'px-3 py-1.5 -mb-px',
        'font-mono text-[10px] uppercase tracking-[0.2em]',
        'border-b-2 transition-colors',
        active
          ? 'border-primary text-text-primary'
          : 'border-transparent text-text-muted hover:text-text-primary',
        'focus:outline-none focus-visible:ring-1 focus-visible:ring-primary/60',
      )}
    >
      {label}
    </button>
  );
}

function readInitialInput(skillName: string): string {
  try {
    const saved = window.localStorage.getItem(LS_PREFIX + skillName);
    if (saved !== null) return saved;
  } catch {
    // localStorage unavailable
  }
  return '{}';
}

function schemaHasProperties(schema: Record<string, unknown> | undefined): boolean {
  if (!schema) return false;
  const props = schema.properties;
  return (
    typeof props === 'object' &&
    props !== null &&
    Object.keys(props as Record<string, unknown>).length > 0
  );
}

type ParseResult =
  | { kind: 'ok'; value: Record<string, unknown> }
  | { kind: 'error'; message: string };

function tryParse(text: string): ParseResult {
  const trimmed = text.trim();
  if (trimmed === '') {
    return { kind: 'ok', value: {} };
  }
  try {
    const parsed = JSON.parse(trimmed);
    if (parsed === null || typeof parsed !== 'object' || Array.isArray(parsed)) {
      return { kind: 'error', message: 'input must be a JSON object' };
    }
    return { kind: 'ok', value: parsed as Record<string, unknown> };
  } catch (err) {
    return {
      kind: 'error',
      message: err instanceof Error ? err.message : 'invalid JSON',
    };
  }
}

function tryParseLoose(text: string): Record<string, unknown> {
  const result = tryParse(text);
  return result.kind === 'ok' ? result.value : {};
}

function validateJSON(text: string): string | null {
  const result = tryParse(text);
  return result.kind === 'error' ? result.message : null;
}

function trapTab(e: KeyboardEvent<HTMLDivElement>, container: HTMLElement) {
  const focusable = container.querySelectorAll<HTMLElement>(
    'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])',
  );
  if (focusable.length === 0) return;
  const first = focusable[0];
  const last = focusable[focusable.length - 1];
  const active = document.activeElement;
  if (e.shiftKey) {
    if (active === first || !container.contains(active)) {
      e.preventDefault();
      last.focus();
    }
  } else {
    if (active === last) {
      e.preventDefault();
      first.focus();
    }
  }
}

function relativeOrDash(when: string | undefined): string {
  if (!when) return '—';
  const parsed = new Date(when);
  if (isNaN(parsed.getTime())) return '—';
  return formatRelativeTime(parsed);
}
