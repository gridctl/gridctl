import { useCallback, useState, type RefObject } from 'react';
import { Plus } from 'lucide-react';
import { cn } from '../../lib/cn';
import { Button } from '../ui/Button';
import { VariableTypeSelector } from './VariableTypeSelector';
import { VariableSecretToggle } from './VariableSecretToggle';
import { SecretGenerator } from './SecretGenerator';
import { VariableValueInput } from './VariableValueInput';
import { validateVariableInput } from './variableTypeHelpers';
import type { VariableType } from '../../lib/api';

export interface QuickAddInput {
  key: string;
  value: string;
  type: VariableType;
  isSecret: boolean;
  set?: string;
}

export interface VariableQuickAddFormProps {
  setNames: string[];
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

  const doSubmit = useCallback(async () => {
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
      });
      setNewKey('');
      setNewValue('');
      setNewSet('');
      setShowValue(false);
      setType('string');
      setIsSecret(true);
    } catch (err) {
      setError(
        err instanceof Error ? err.message : 'Failed to create variable',
      );
    } finally {
      setSubmitting(false);
    }
  }, [newKey, newValue, newSet, type, isSecret, onSubmit]);

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
    onCancel?.();
  }, [onCancel]);

  return (
    <form onSubmit={handleSubmit} className={className}>
      <div className="space-y-2">
        <input
          ref={keyInputRef}
          type="text"
          value={newKey}
          onChange={(e) => {
            setNewKey(e.target.value.toUpperCase().replace(/[^A-Z0-9_]/g, ''));
            setError(null);
          }}
          placeholder="KEY_NAME"
          className={cn(
            'w-full bg-surface border rounded-lg px-3 py-2 text-xs font-mono text-text-primary',
            'placeholder:text-text-muted focus:border-primary/50 focus:ring-1 focus:ring-primary/30 outline-none transition-colors',
            error ? 'border-status-error/50' : 'border-border',
            enableZoom && 'log-text',
          )}
        />
        <VariableValueInput
          type={type}
          value={newValue}
          onChange={(v) => {
            setNewValue(v);
            setError(null);
          }}
          isSecret={isSecret}
          revealed={showValue}
          onToggleReveal={() => setShowValue((s) => !s)}
          onRequestSubmit={doSubmit}
          enableZoom={enableZoom}
        />
        <div className="flex flex-wrap items-center gap-2">
          <VariableTypeSelector value={type} onChange={handleTypeChange} />
          <VariableSecretToggle isSecret={isSecret} onChange={setIsSecret} />
          {type === 'string' && (
            <SecretGenerator
              onGenerate={setNewValue}
              onReveal={() => setShowValue(true)}
            />
          )}
        </div>
        <div className="flex gap-2">
          <select
            value={newSet}
            onChange={(e) => setNewSet(e.target.value)}
            className="flex-1 bg-surface border border-border rounded-lg px-3 py-2 text-xs text-text-secondary focus:border-primary/50 outline-none transition-colors"
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
          <Button
            type="submit"
            variant="primary"
            size="sm"
            disabled={!newKey.trim() || !newValue || submitting}
          >
            <Plus size={12} />
            Add
          </Button>
        </div>
      </div>
      {error && <p className="mt-1.5 text-[10px] text-status-error">{error}</p>}
    </form>
  );
}
