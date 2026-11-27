import { useId, useState, type InputHTMLAttributes } from 'react';
import { Eye, EyeOff, KeyRound, Loader2 } from 'lucide-react';
import { cn } from '../../lib/cn';
import { createVariable, fetchVariables, HTTPError } from '../../lib/api';
import { isLiteralSecret } from '../../lib/wizard-secrets';
import { useVaultStore } from '../../stores/useVaultStore';

const inputClass = 'w-full bg-background/60 border border-border/40 rounded-lg px-3 py-2 text-xs text-text-primary placeholder:text-text-muted transition-colors';

const KEY_PATTERN = /^[A-Z][A-Z0-9_]*$/;

type PassThrough = Omit<InputHTMLAttributes<HTMLInputElement>, 'id' | 'value' | 'onChange' | 'placeholder' | 'className' | 'type'>;

interface SecretInputProps extends PassThrough {
  id?: string;
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  ariaLabel?: string;
  className?: string;
  // Prefills the "Store as variable" key, normalized to an uppercase name.
  suggestedKey?: string;
  // False renders a plain text input with no mask or warning. Lets a row
  // switch between the two as its key is edited without remounting.
  sensitive?: boolean;
}

function normalizeVariableKey(value: string | undefined): string {
  return (value ?? '').toUpperCase().replace(/[^A-Z0-9_]/g, '_');
}

// SecretInput is a credential field: a literal value is masked with a reveal
// toggle and a nudge to move it into the variable store; references and
// empty values render as ordinary text.
export function SecretInput({
  id,
  value,
  onChange,
  placeholder,
  ariaLabel,
  className,
  suggestedKey,
  sensitive = true,
  ...rest
}: SecretInputProps) {
  const uid = useId();
  const [revealed, setRevealed] = useState(false);
  const [storing, setStoring] = useState(false);
  const [key, setKey] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const setVariables = useVaultStore((s) => s.setVariables);

  const literal = sensitive && isLiteralSecret(value);
  const masked = literal && !revealed;
  const keyId = `${uid}-key`;
  const warningId = `${uid}-warning`;

  const openStore = () => {
    setKey(normalizeVariableKey(suggestedKey));
    setError(null);
    setStoring(true);
  };

  const store = async () => {
    if (!KEY_PATTERN.test(key)) {
      setError('Use an uppercase name: letters, digits, and underscores, starting with a letter.');
      return;
    }
    const exists = `A variable named ${key} already exists. Pick another name or insert it with the key icon.`;
    setSaving(true);
    setError(null);
    try {
      const current = await fetchVariables();
      if (current.some((v) => v.key === key)) {
        setVariables(current);
        setError(exists);
        return;
      }
      await createVariable({ key, value, type: 'string', isSecret: true, createOnly: true });
      setVariables(await fetchVariables());
      setStoring(false);
      setRevealed(false);
      onChange(`\${var:${key}}`);
    } catch (e) {
      if (e instanceof HTTPError && e.status === 409) setError(exists);
      else if (e instanceof HTTPError && e.status === 423) setError('Variables are locked. Unlock them in the Variables workspace, then retry.');
      else setError(e instanceof Error && e.message ? e.message : 'Failed to store the variable');
    } finally {
      setSaving(false);
    }
  };

  const userDescribedBy = rest['aria-describedby'];
  const describedBy = [userDescribedBy, literal ? warningId : undefined].filter(Boolean).join(' ') || undefined;

  return (
    <div className="flex-1 min-w-0">
      <div className="relative">
        <input
          {...rest}
          id={id}
          type={masked ? 'password' : 'text'}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder={placeholder}
          aria-label={ariaLabel}
          aria-describedby={describedBy}
          autoComplete="off"
          spellCheck={false}
          data-1p-ignore
          data-lpignore="true"
          data-bwignore
          className={cn(
            inputClass,
            className,
            literal && 'pr-8',
            value.startsWith('${var:') && 'text-tertiary font-medium',
          )}
        />
        {literal && (
          <button
            type="button"
            onClick={() => setRevealed((v) => !v)}
            aria-pressed={revealed}
            aria-label={revealed ? 'Hide value' : 'Show value'}
            className="absolute right-1.5 top-1/2 -translate-y-1/2 p-1 text-text-muted hover:text-text-secondary transition-colors"
          >
            {revealed ? <EyeOff size={11} /> : <Eye size={11} />}
          </button>
        )}
      </div>
      {literal && (
        <div className="mt-1 space-y-1">
          <p id={warningId} className="flex items-center gap-1.5 text-3xs text-status-pending">
            <span>This literal will be written to the stack file in clear text.</span>
            {!storing && (
              <button
                type="button"
                onClick={openStore}
                className="inline-flex items-center gap-1 text-secondary hover:text-secondary-light transition-colors"
              >
                <KeyRound size={10} />
                Store as variable
              </button>
            )}
          </p>
          {storing && (
            <div className="flex items-center gap-1.5">
              <label htmlFor={keyId} className="text-3xs text-text-muted flex-shrink-0">Variable name</label>
              <input
                id={keyId}
                type="text"
                value={key}
                onChange={(e) => setKey(normalizeVariableKey(e.target.value))}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    e.preventDefault();
                    void store();
                  }
                }}
                placeholder="MY_TOKEN"
                spellCheck={false}
                autoComplete="off"
                className={cn(inputClass, 'py-1 font-mono flex-1')}
              />
              <button
                type="button"
                onClick={() => void store()}
                disabled={saving || !key}
                className="px-2 py-1 rounded-md text-3xs font-medium bg-primary/15 text-primary hover:bg-primary/25 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
              >
                {saving ? <Loader2 size={10} className="animate-spin" /> : 'Save variable'}
              </button>
              <button
                type="button"
                onClick={() => {
                  setStoring(false);
                  setError(null);
                }}
                className="px-2 py-1 rounded-md text-3xs text-text-muted hover:text-text-secondary transition-colors"
              >
                Cancel
              </button>
            </div>
          )}
          {error && (
            <p role="alert" className="text-3xs text-status-error">{error}</p>
          )}
        </div>
      )}
    </div>
  );
}
