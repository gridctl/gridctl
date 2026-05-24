import { lazy, Suspense, useEffect, useId, useState } from 'react';
import { Eye, EyeOff } from 'lucide-react';
import { cn } from '../../lib/cn';
import { BoolToggle } from './BoolToggle';
import { NumberValueInput } from './NumberValueInput';
import { ListTagInput } from './ListTagInput';
import { validateVariableInput, getValuePlaceholder } from './variableTypeHelpers';
import type { VariableType } from '../../lib/api';

// CodeMirror is heavy; keep it out of the main chunk and load it only when a
// json value is actually edited.
const JsonValueEditor = lazy(() => import('./JsonValueEditor'));

export interface VariableValueInputProps {
  type: VariableType;
  // The current human/input form of the value — i.e. exactly what
  // validateVariableInput accepts (comma-separated for `list`, "true"/"false"
  // for `bool`, raw text for `json`/`number`). Callers keep normalizing via
  // validateVariableInput(type, value) before hitting the API.
  value: string;
  onChange: (next: string) => void;
  isSecret: boolean;
  // Whether the value is currently visible. Rich editors only render once a
  // secret is revealed — you can't meaningfully highlight or chip a masked
  // value. Plaintext (isSecret=false) is always "revealed".
  revealed: boolean;
  onToggleReveal: () => void;
  onValidityChange?: (valid: boolean) => void;
  // Enter (string/number) or Cmd/Ctrl+Enter (json) request a submit.
  onRequestSubmit?: () => void;
  // Escape requests a cancel (used by the inline edit row).
  onRequestCancel?: () => void;
  compact?: boolean;
  enableZoom?: boolean;
  placeholder?: string;
  autoFocus?: boolean;
}

// VariableValueInput renders the right editor for a variable's type and is the
// single value-editing surface shared by the add form, the inline edit row,
// the wizard popover, and bulk import. It owns widget selection, the
// secret-reveal gate, and validity reporting; it always emits the human form
// so the existing validateVariableInput pipeline stays the source of truth.
export function VariableValueInput({
  type,
  value,
  onChange,
  isSecret,
  revealed,
  onToggleReveal,
  onValidityChange,
  onRequestSubmit,
  onRequestCancel,
  compact,
  enableZoom,
  placeholder,
  autoFocus,
}: VariableValueInputProps) {
  const errorId = useId();
  const [touched, setTouched] = useState(false);

  // A bool always has a concrete value so the toggle is meaningful and the
  // surrounding form's "value required" guard is satisfied. Seed false when the
  // incoming value isn't a recognized token (e.g. a freshly-blanked edit).
  useEffect(() => {
    if (type === 'bool' && value !== 'true' && value !== 'false') {
      onChange('false');
    }
  }, [type, value, onChange]);

  const validation = validateVariableInput(type, value);
  const valid = validation.ok;
  useEffect(() => {
    onValidityChange?.(valid);
  }, [valid, onValidityChange]);

  // Reset the touched flag when switching types so a stale error from a prior
  // type doesn't linger on a fresh widget. Uses React's documented
  // adjust-state-during-render pattern rather than an effect.
  const [prevType, setPrevType] = useState(type);
  if (type !== prevType) {
    setPrevType(type);
    setTouched(false);
  }

  // Only surface an inline error for a non-empty value that fails its type
  // (e.g. malformed JSON). An empty required field isn't "wrong" yet — the
  // disabled Add/Save button already conveys that — so we don't scold it.
  const showError = touched && !valid && value !== '';
  const describedBy = showError ? errorId : undefined;
  const errorNode = showError ? (
    <p id={errorId} className="mt-1 text-[10px] text-status-error">
      {!valid ? validation.error : ''}
    </p>
  ) : null;

  // bool: a toggle is coherent even for secrets (a single on/off bit), so it
  // always renders regardless of reveal state.
  if (type === 'bool') {
    return (
      <div>
        <BoolToggle
          checked={value === 'true'}
          onChange={(b) => onChange(b ? 'true' : 'false')}
          compact={compact}
        />
      </div>
    );
  }

  // string: a single-line field that masks while typing when secret (the
  // classic API-key case), with an eye to reveal. Structured types don't take
  // this path — see below.
  if (type === 'string') {
    return (
      <div>
        <div className="relative">
          <input
            type={revealed || !isSecret ? 'text' : 'password'}
            value={value}
            autoFocus={autoFocus}
            placeholder={placeholder ?? getValuePlaceholder(type, isSecret)}
            aria-invalid={showError || undefined}
            aria-describedby={describedBy}
            onChange={(e) => onChange(e.target.value)}
            onBlur={() => setTouched(true)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault();
                onRequestSubmit?.();
              }
              if (e.key === 'Escape') onRequestCancel?.();
            }}
            className={cn(
              'w-full bg-surface border rounded-lg px-3 py-2 text-xs font-mono text-text-primary',
              'placeholder:text-text-muted focus:ring-1 focus:ring-primary/30 outline-none transition-colors',
              isSecret ? 'pr-10' : 'pr-3',
              showError ? 'border-status-error/50' : 'border-border focus:border-primary/50',
              compact && 'py-1.5',
              enableZoom && 'log-text',
            )}
          />
          {isSecret && (
            <button
              type="button"
              onClick={onToggleReveal}
              aria-label={revealed ? 'Hide value' : 'Reveal value'}
              aria-pressed={revealed}
              className="absolute right-2 top-1/2 -translate-y-1/2 p-1 rounded text-text-muted hover:text-text-primary transition-colors"
            >
              {revealed ? <EyeOff size={12} /> : <Eye size={12} />}
            </button>
          )}
        </div>
        {errorNode}
      </div>
    );
  }

  // Structured editors (json/list/number) always render their rich widget when
  // composing — the value is the user's own input, so masking it while typing
  // is pure friction and can't coexist with highlighting/chips. The Secret flag
  // still governs storage and masks the value in the saved variable row.
  return (
    <div>
      {type === 'number' && (
        <NumberValueInput
          value={value}
          onChange={onChange}
          onBlur={() => setTouched(true)}
          onSubmit={onRequestSubmit}
          onCancel={onRequestCancel}
          invalid={showError}
          describedBy={describedBy}
          compact={compact}
          enableZoom={enableZoom}
          autoFocus={autoFocus}
          placeholder={placeholder}
        />
      )}
      {type === 'list' && (
        <ListTagInput
          value={value}
          onChange={onChange}
          onCancel={onRequestCancel}
          compact={compact}
          enableZoom={enableZoom}
          autoFocus={autoFocus}
        />
      )}
      {type === 'json' && (
        <Suspense
          fallback={
            <div className="rounded-lg border border-border bg-surface px-3 py-2 text-[10px] text-text-muted">
              Loading editor…
            </div>
          }
        >
          <JsonValueEditor
            value={value}
            onChange={onChange}
            onBlur={() => setTouched(true)}
            onSubmit={onRequestSubmit}
            onCancel={onRequestCancel}
            compact={compact}
          />
        </Suspense>
      )}
      {errorNode}
    </div>
  );
}
