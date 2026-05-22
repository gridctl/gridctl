import { useCallback, useState, type RefObject } from 'react';
import { Eye, EyeOff, Plus } from 'lucide-react';
import { cn } from '../../lib/cn';
import { Button } from '../ui/Button';
import { VariableTypeSelector } from './VariableTypeSelector';
import { VariableSecretToggle } from './VariableSecretToggle';
import {
  validateVariableInput,
  getValuePlaceholder,
} from './variableTypeHelpers';
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
}: VariableQuickAddFormProps) {
  const [newKey, setNewKey] = useState('');
  const [newValue, setNewValue] = useState('');
  const [newSet, setNewSet] = useState('');
  const [showValue, setShowValue] = useState(false);
  const [type, setType] = useState<VariableType>('string');
  const [isSecret, setIsSecret] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const handleSubmit = useCallback(
    async (e: React.FormEvent) => {
      e.preventDefault();
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
    },
    [newKey, newValue, newSet, type, isSecret, onSubmit],
  );

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
        <div className="relative">
          <input
            type={showValue ? 'text' : 'password'}
            value={newValue}
            onChange={(e) => setNewValue(e.target.value)}
            placeholder={getValuePlaceholder(type, isSecret)}
            className={cn(
              'w-full bg-surface border border-border rounded-lg px-3 py-2 pr-10 text-xs font-mono text-text-primary placeholder:text-text-muted focus:border-primary/50 focus:ring-1 focus:ring-primary/30 outline-none transition-colors',
              enableZoom && 'log-text',
            )}
          />
          <button
            type="button"
            onClick={() => setShowValue(!showValue)}
            className="absolute right-2.5 top-1/2 -translate-y-1/2 p-1 rounded text-text-muted hover:text-text-primary transition-colors"
          >
            {showValue ? <EyeOff size={12} /> : <Eye size={12} />}
          </button>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <VariableTypeSelector value={type} onChange={setType} />
          <VariableSecretToggle isSecret={isSecret} onChange={setIsSecret} />
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
