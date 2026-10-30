import { useId, type ReactNode } from 'react';
import { AlertCircle } from 'lucide-react';

export function FieldError({ error, id }: { error?: string; id?: string }) {
  if (!error) return null;
  return (
    <p id={id} className="text-[10px] text-status-error mt-1">
      <AlertCircle size={10} className="inline mr-1 -mt-0.5" />
      {error}
    </p>
  );
}

export interface FieldControl {
  id: string;
  describedBy: string | undefined;
  invalid: true | undefined;
}

export function Field({
  label,
  required,
  help,
  error,
  className,
  labelClassName = 'block text-xs text-text-secondary mb-1.5',
  children,
}: {
  label: ReactNode;
  required?: boolean;
  help?: ReactNode;
  error?: string;
  className?: string;
  labelClassName?: string;
  children: (field: FieldControl) => ReactNode;
}) {
  const id = useId();
  const helpId = `${id}-help`;
  const errorId = `${id}-error`;
  const describedIds = [error ? errorId : undefined, help ? helpId : undefined].filter(
    (part): part is string => Boolean(part),
  );
  const describedBy = describedIds.length > 0 ? describedIds.join(' ') : undefined;
  const invalid = error ? (true as const) : undefined;

  return (
    <div className={className}>
      <label htmlFor={id} className={labelClassName}>
        {label}
        {required && (
          <>
            {' '}
            <span className="text-status-error">*</span>
          </>
        )}
      </label>
      {children({ id, describedBy, invalid })}
      <FieldError id={errorId} error={error} />
      {help && (
        <p id={helpId} className="text-[10px] text-text-muted mt-1">
          {help}
        </p>
      )}
    </div>
  );
}
