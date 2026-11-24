import { useCallback, useId, useState, type RefObject } from 'react';
import { Plus } from 'lucide-react';
import { cn } from '../../lib/cn';
import { Button } from '../ui/Button';
import { VariableTypeSelector } from './VariableTypeSelector';
import { VariableSecretToggle } from './VariableSecretToggle';
import { SecretGenerator } from './SecretGenerator';
import { VariableValueInput } from './VariableValueInput';
import { validateVariableInput } from './variableTypeHelpers';
import { describeReplace } from './replaceSummary';
import { HTTPError, type Variable, type VariableType } from '../../lib/api';

export interface QuickAddInput {
  key: string;
  value: string;
  type: VariableType;
  isSecret: boolean;
  set?: string;
  // True when the user confirmed replacing an existing key. The host must
  // apply it as a partial update so metadata the form does not edit survives.
  replace?: boolean;
}

export interface VariableQuickAddFormProps {
  setNames: string[];
  // The current variable list. A key found here is treated as existing: Add
  // is refused and replacing takes an explicit confirmation.
  existingVariables: Variable[];
  onSubmit: (input: QuickAddInput) => Promise<void>;
  className?: string;
  keyInputRef?: RefObject<HTMLInputElement | null>;
  // Apply `.log-text` so the key and value inputs scale with the parent's
  // zoom controls (detached page).
  enableZoom?: boolean;
  // When provided, a Cancel button is shown that clears the form and invokes
  // this (e.g. to collapse the add panel). Omit for always-open hosts.
  onCancel?: () => void;
}

// Quick-add form for a single variable. Internal state for every field;
// validation is run before delegating to `onSubmit`, which should throw on
// error. On success the form clears itself.
export function VariableQuickAddForm({
  setNames,
  existingVariables,
  onSubmit,
  className,
  keyInputRef,
  enableZoom,
  onCancel,
}: VariableQuickAddFormProps) {
  const [newKey, setNewKey] = useState('');
  const [newValue, setNewValue] = useState('');
  const [newSet, setNewSet] = useState('');
  const [showValue, setShowValue] = useState(false);
  const [type, setType] = useState<VariableType>('string');
  const [isSecret, setIsSecret] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [replaceArmed, setReplaceArmed] = useState(false);
  // Key the server refused as existing although the list lacked it (stale
  // list). Kept until the key is edited.
  const [serverConflictKey, setServerConflictKey] = useState<string | null>(null);
  const conflictNoticeId = useId();

  const trimmedKey = newKey.trim();
  const conflict = trimmedKey
    ? existingVariables.find((v) => v.key === trimmedKey)
    : undefined;
  const keyExists =
    conflict !== undefined ||
    (trimmedKey !== '' && trimmedKey === serverConflictKey);
  const canSubmit = trimmedKey !== '' && newValue !== '' && !submitting;

  const submit = useCallback(
    async (replace: boolean) => {
      if (!newKey.trim() || !newValue) return;
      const key = newKey.trim();
      const validation = validateVariableInput(type, newValue);
      if (!validation.ok) {
        setError(validation.error);
        return;
      }
      setSubmitting(true);
      setError(null);
      try {
        await onSubmit({
          key,
          value: validation.normalized,
          type,
          isSecret,
          set: newSet || undefined,
          ...(replace ? { replace: true } : {}),
        });
        setNewKey('');
        setNewValue('');
        setNewSet('');
        setShowValue(false);
        setType('string');
        setIsSecret(true);
        setReplaceArmed(false);
        setServerConflictKey(null);
      } catch (err) {
        if (!replace && err instanceof HTTPError && err.status === 409) {
          setServerConflictKey(key);
        } else {
          setError(
            err instanceof Error ? err.message : 'Failed to create variable',
          );
        }
      } finally {
        setSubmitting(false);
      }
    },
    [newKey, newValue, newSet, type, isSecret, onSubmit],
  );

  // A plain add never replaces: an existing key returns before onSubmit.
  // Enter in the value widget reaches here too, so the guard lives here
  // rather than on the button.
  const doSubmit = useCallback(async () => {
    if (keyExists) return;
    await submit(false);
  }, [keyExists, submit]);

  const confirmReplace = useCallback(async () => {
    if (!conflict) return;
    await submit(true);
  }, [conflict, submit]);

  // Enter inside the value widget routes here via onRequestSubmit; the form's
  // own onSubmit covers the Add button. Both funnel into doSubmit.
  const handleSubmit = useCallback(
    (e: React.FormEvent) => {
      e.preventDefault();
      void doSubmit();
    },
    [doSubmit],
  );

  // Switching into/out of bool clears the value: bool's "true"/"false" is
  // widget-managed (auto-seeded), not user-typed, so it shouldn't bleed into a
  // text/json/list field. Text-type switches keep the value for reinterpreting.
  const handleTypeChange = useCallback(
    (next: VariableType) => {
      if (type === 'bool' || next === 'bool') {
        setNewValue('');
        setError(null);
      }
      setType(next);
      setReplaceArmed(false);
    },
    [type],
  );

  const handleCancel = useCallback(() => {
    setNewKey('');
    setNewValue('');
    setNewSet('');
    setShowValue(false);
    setType('string');
    setIsSecret(true);
    setError(null);
    setReplaceArmed(false);
    setServerConflictKey(null);
    onCancel?.();
  }, [onCancel]);

  return (
    <form onSubmit={handleSubmit} className={className}>
      <div className="space-y-2">
        <input
          ref={keyInputRef}
          type="text"
          value={newKey}
          aria-label="Variable key"
          aria-describedby={keyExists ? conflictNoticeId : undefined}
          onChange={(e) => {
            setNewKey(e.target.value.toUpperCase().replace(/[^A-Z0-9_]/g, ''));
            setError(null);
            setReplaceArmed(false);
            setServerConflictKey(null);
          }}
          placeholder="KEY_NAME"
          className={cn(
            'w-full bg-surface border rounded-lg px-3 py-2 text-xs font-mono text-text-primary',
            'placeholder:text-text-muted transition-colors',
            error ? 'border-status-error/50' : keyExists ? 'border-status-pending/50' : 'border-border',
            enableZoom && 'log-text',
          )}
        />
        {keyExists && (
          <p id={conflictNoticeId} role="status" className="text-3xs text-status-pending">
            {conflict
              ? `${trimmedKey} already exists.`
              : `${trimmedKey} already exists. Refresh the list to replace it.`}
          </p>
        )}
        <VariableValueInput
          type={type}
          value={newValue}
          ariaLabel="Variable value"
          onChange={(v) => {
            setNewValue(v);
            setError(null);
            setReplaceArmed(false);
          }}
          isSecret={isSecret}
          revealed={showValue}
          onToggleReveal={() => setShowValue((s) => !s)}
          onRequestSubmit={doSubmit}
          enableZoom={enableZoom}
        />
        <div className="flex flex-wrap items-center gap-2">
          <VariableTypeSelector value={type} onChange={handleTypeChange} />
          <VariableSecretToggle
            isSecret={isSecret}
            onChange={(next) => {
              setIsSecret(next);
              setReplaceArmed(false);
            }}
          />
          {type === 'string' && (
            <SecretGenerator
              onGenerate={(v) => {
                setNewValue(v);
                setReplaceArmed(false);
              }}
              onReveal={() => setShowValue(true)}
            />
          )}
        </div>
        <div className="flex gap-2">
          <select
            value={newSet}
            onChange={(e) => {
              setNewSet(e.target.value);
              setReplaceArmed(false);
            }}
            aria-label="Variable set"
            className="flex-1 bg-surface border border-border rounded-lg px-3 py-2 text-xs text-text-secondary transition-colors"
          >
            <option value="">No set</option>
            {setNames.map((name) => (
              <option key={name} value={name}>
                {name}
              </option>
            ))}
          </select>
          {onCancel && (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={handleCancel}
              disabled={submitting}
            >
              Cancel
            </Button>
          )}
          {conflict && (
            <Button
              type="button"
              variant="outline-danger"
              size="sm"
              onClick={() => setReplaceArmed(true)}
              disabled={!canSubmit || replaceArmed}
            >
              Replace…
            </Button>
          )}
          <Button
            type="submit"
            variant="primary"
            size="sm"
            disabled={!canSubmit || keyExists}
          >
            <Plus size={12} />
            Add
          </Button>
        </div>
        {conflict && replaceArmed && (
          <div
            role="group"
            aria-label={`Confirm replacing ${conflict.key}`}
            className="rounded-lg border border-status-error/25 bg-status-error/5 px-3 py-2 space-y-2"
          >
            <ul className="text-3xs text-text-secondary space-y-0.5">
              {describeReplace(conflict, {
                type,
                isSecret,
                set: newSet || undefined,
              }).map((line) => (
                <li key={line}>{line}</li>
              ))}
            </ul>
            <div className="flex justify-end gap-2">
              <Button
                type="button"
                variant="ghost"
                size="sm"
                onClick={() => setReplaceArmed(false)}
                disabled={submitting}
                aria-label="Cancel replace"
              >
                Cancel
              </Button>
              <Button
                type="button"
                variant="danger"
                size="sm"
                onClick={() => void confirmReplace()}
                disabled={!canSubmit}
              >
                Replace {conflict.key}
              </Button>
            </div>
          </div>
        )}
      </div>
      {error && <p className="mt-1.5 text-3xs text-status-error">{error}</p>}
    </form>
  );
}
