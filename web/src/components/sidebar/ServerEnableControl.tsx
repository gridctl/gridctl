import { useState } from 'react';
import { patchServerEnabled, SetServerToolsError, RestartRequiredError, fetchStatus } from '../../lib/api';
import { useStackStore } from '../../stores/useStackStore';

interface ServerEnableControlProps {
  name: string;
  enabled: boolean;
  overrides?: string[];
}

export function ServerEnableControl({ name, enabled, overrides }: ServerEnableControlProps) {
  const [hint, setHint] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const toggle = async () => {
    setBusy(true);
    setHint(null);
    try {
      await patchServerEnabled(name, !enabled);
      const status = await fetchStatus();
      useStackStore.getState().setGatewayStatus(status);
    } catch (err) {
      if (err instanceof SetServerToolsError) {
        if (err.code === 'override_required') {
          setHint(err.hint || 'Use gridctl enable or gridctl disable. The canvas toggle does not edit an override.');
          return;
        }
        if (err.code === 'stack_modified') {
          setHint(err.hint || 'The stack file was modified outside the canvas. Reload the file and try again.');
          return;
        }
        setHint(err.hint || err.message);
        return;
      }
      if (err instanceof RestartRequiredError) {
        setHint(err.message);
        return;
      }
      setHint(err instanceof Error ? err.message : 'Toggle failed');
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="space-y-2">
      <button
        type="button"
        onClick={toggle}
        disabled={busy}
        className="inline-flex items-center justify-center px-3 py-1.5 rounded-md text-xs border border-border bg-surface-elevated text-text-primary hover:bg-surface-highlight disabled:opacity-50"
      >
        {enabled ? 'Disable' : 'Enable'}
      </button>
      {hint && (
        <p role="status" className="text-xs text-status-pending">
          {hint}
        </p>
      )}
      {overrides && overrides.length > 0 && (
        <ul className="space-y-1">
          {overrides.map((field) => (
            <li key={field} className="flex items-center gap-2 text-3xs text-text-muted">
              <span className="font-mono">{field}</span>
              <span className="rounded border border-border px-1.5 py-0.5 uppercase tracking-wide">overridden locally</span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
