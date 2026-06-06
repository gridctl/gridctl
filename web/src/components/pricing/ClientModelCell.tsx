import { useCallback, useState } from 'react';
import { cn } from '../../lib/cn';
import { updateClientModel } from '../../lib/api';
import { showToast } from '../ui/Toast';
import { ModelPicker } from './ModelPicker';
import { MODEL_PRECEDENCE_HINT } from './constants';

// ClientModelCell shows which model a client's calls are priced as and lets
// the operator set it inline. A pill with "· client" provenance renders only
// for clients explicitly declared in client_models; non-declaring clients
// aggregate heterogeneous per-server/default rates, so they get a muted
// "per-server" (when any attribution is configured) rather than an invented
// single model. Clicking opens the shared ModelPicker combobox; free text is
// allowed (unknown IDs price as zero, best-effort).
//
// State stays with the host: `declaredModel` is the saved value and
// `onSaved` reports a successful write so the main window can update its
// store optimistically while the detached window updates local state.
export function ClientModelCell({
  client,
  declaredModel,
  costAttribution,
  onSaved,
}: {
  client: string;
  declaredModel?: string;
  costAttribution: boolean;
  onSaved: (client: string, model: string) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  const save = useCallback(
    async (model: string) => {
      if (model === (declaredModel ?? '')) {
        setEditing(false);
        return;
      }
      setSaving(true);
      setSaveError(null);
      try {
        const resp = await updateClientModel(client, model);
        onSaved(client, model);
        setEditing(false);
        showToast('success', model === '' ? `Pricing model cleared for ${client}` : `Pricing model saved for ${client}`);
        if (resp.reloaded === false) {
          showToast('warning', 'Stack updated. Run "gridctl reload" or restart with --watch to apply.');
        }
      } catch (err) {
        const msg = err instanceof Error ? err.message : 'Save failed';
        setSaveError(msg);
        showToast('error', msg);
      } finally {
        setSaving(false);
      }
    },
    [client, declaredModel, onSaved],
  );

  if (editing) {
    return (
      <span className="inline-flex items-center gap-1">
        <ModelPicker
          value={declaredModel ?? ''}
          onCommit={(model) => void save(model)}
          onCancel={() => setEditing(false)}
          disabled={saving}
          autoFocus
          widthClass="w-52"
          error={saveError}
        />
        <button
          type="button"
          disabled={saving}
          onClick={() => setEditing(false)}
          className="text-[10px] text-text-muted hover:text-text-secondary disabled:opacity-50"
        >
          cancel
        </button>
      </span>
    );
  }

  if (declaredModel) {
    return (
      <button
        type="button"
        onClick={() => {
          setSaveError(null);
          setEditing(true);
        }}
        title={MODEL_PRECEDENCE_HINT}
        className="inline-flex items-center gap-1 rounded-full bg-surface-highlight/60 border border-border/40 px-2 py-0.5 hover:border-primary/40 transition-colors"
      >
        <span className="text-[10px] font-mono text-text-primary">{declaredModel}</span>
        <span className="text-[9px] text-text-muted/70">· client</span>
      </button>
    );
  }

  return (
    <button
      type="button"
      onClick={() => {
        setSaveError(null);
        setEditing(true);
      }}
      title={MODEL_PRECEDENCE_HINT}
      className={cn(
        'text-[10px] transition-colors',
        costAttribution
          ? // Informational state that happens to be clickable: stays quiet
            // in dense tables, brightens to the accent on hover.
            'text-text-muted/70 hover:text-secondary'
          : // Empty-state CTA: the house interactive-text pattern.
            'text-secondary hover:text-secondary-light',
      )}
    >
      {costAttribution ? 'per-server' : 'set model'}
    </button>
  );
}

export default ClientModelCell;
