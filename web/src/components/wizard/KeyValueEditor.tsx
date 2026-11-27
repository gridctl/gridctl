import { useId } from 'react';
import { Plus, X } from 'lucide-react';
import { cn } from '../../lib/cn';
import { duplicateKeys, mapFromRows, newRow, rowsForMap, type KeyValueRow } from '../../lib/keyValueRows';
import { FieldError } from '../ui/Field';
import { VariablesPopover } from './VariablesPopover';
import { SecretInput } from './SecretInput';
import { isSensitiveEnvName } from '../../lib/wizard-secrets';

const inputClass = 'w-full bg-background/60 border border-border/40 rounded-lg px-3 py-2 text-xs text-text-primary placeholder:text-text-muted transition-colors';

export function KeyValueEditor({
  label,
  value,
  onChange,
  placeholder,
  showSecrets = false,
  sensitiveValues = false,
}: {
  label: string;
  value: Record<string, string>;
  onChange: (val: Record<string, string>) => void;
  placeholder?: { key: string; value: string };
  showSecrets?: boolean;
  // Treat every value as a credential. Otherwise only rows whose key names a
  // credential (TOKEN, PASSWORD, and the rest of the export list) are masked.
  sensitiveValues?: boolean;
}) {
  const uid = useId();
  const labelId = `${uid}-label`;
  const rows = rowsForMap(value);
  const duplicates = [...duplicateKeys(rows)];
  const duplicateMessageId = (key: string) => `${uid}-duplicate-${duplicates.indexOf(key)}`;

  const emit = (next: KeyValueRow[]) => onChange(mapFromRows(next));

  const addEntry = () => emit([...rows, newRow()]);

  const updateRow = (id: string, patch: Partial<Omit<KeyValueRow, 'id'>>) => {
    emit(rows.map((row) => (row.id === id ? { ...row, ...patch } : row)));
  };

  const removeEntry = (id: string) => emit(rows.filter((row) => row.id !== id));

  return (
    <div>
      <div className="flex items-center justify-between mb-1.5">
        <label id={labelId} className="text-xs text-text-secondary">{label}</label>
        <button
          type="button"
          onClick={addEntry}
          className="flex items-center gap-1 text-3xs text-secondary hover:text-secondary-light transition-colors"
        >
          <Plus size={10} />
          Add
        </button>
      </div>
      {rows.length === 0 && (
        <p className="text-3xs text-text-muted italic py-2">No entries</p>
      )}
      <div role="group" aria-labelledby={labelId} className="space-y-1.5">
        {rows.map((row, i) => {
          const duplicate = duplicates.includes(row.key);
          return (
            <div key={row.id} className="flex items-center gap-1.5">
              <input
                type="text"
                value={row.key}
                onChange={(e) => updateRow(row.id, { key: e.target.value })}
                placeholder={placeholder?.key ?? 'KEY'}
                aria-label={`${label} key ${i + 1}`}
                aria-invalid={duplicate ? true : undefined}
                aria-describedby={duplicate ? duplicateMessageId(row.key) : undefined}
                className={cn(inputClass, 'w-[40%] font-mono', duplicate && 'border-status-error/50')}
              />
              <span className="text-text-muted text-xs">=</span>
              <div className="flex-1 flex items-center gap-0.5">
                <SecretInput
                  value={row.value}
                  onChange={(next) => updateRow(row.id, { value: next })}
                  placeholder={placeholder?.value ?? 'value'}
                  ariaLabel={`${label} value ${i + 1}`}
                  suggestedKey={row.key}
                  sensitive={sensitiveValues || isSensitiveEnvName(row.key)}
                />
                {showSecrets && (
                  <VariablesPopover onSelect={(ref) => updateRow(row.id, { value: ref })} />
                )}
              </div>
              <button
                type="button"
                onClick={() => removeEntry(row.id)}
                aria-label={`Remove ${row.key || 'entry'}`}
                className="p-1 text-text-muted hover:text-status-error transition-colors flex-shrink-0"
              >
                <X size={12} />
              </button>
            </div>
          );
        })}
      </div>
      {duplicates.map((key) => (
        <FieldError
          key={key}
          id={duplicateMessageId(key)}
          error={`Duplicate key "${key}". Only the first row is used.`}
        />
      ))}
    </div>
  );
}
