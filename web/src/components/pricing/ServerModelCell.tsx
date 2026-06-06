import { useCallback, useState } from 'react';
import { updateServerModel } from '../../lib/api';
import { showToast } from '../ui/Toast';
import { ModelPicker } from './ModelPicker';
import { MODEL_PRECEDENCE_HINT } from './constants';

// ServerModelCell mirrors ClientModelCell for the server tier: a pill with
// "· server" provenance when the server declares its own model:, a muted
// "default: <id>" when it inherits gateway.default_model, and "set model"
// when no attribution applies. Clicking edits inline through the shared
// ModelPicker; an empty save clears the server's model: key so it falls
// back to the default.
export function ServerModelCell({
  server,
  declaredModel,
  defaultModel,
  onSaved,
}: {
  server: string;
  declaredModel?: string;
  defaultModel: string;
  onSaved: (server: string, model: string) => void;
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
        const resp = await updateServerModel(server, model);
        onSaved(server, model);
        setEditing(false);
        showToast('success', model === '' ? `Pricing model cleared for ${server}` : `Pricing model saved for ${server}`);
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
    [server, declaredModel, onSaved],
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

  const openEditor = () => {
    setSaveError(null);
    setEditing(true);
  };

  if (declaredModel) {
    return (
      <button
        type="button"
        onClick={openEditor}
        title={MODEL_PRECEDENCE_HINT}
        className="inline-flex items-center gap-1 rounded-full bg-surface-highlight/60 border border-border/40 px-2 py-0.5 hover:border-primary/40 transition-colors"
      >
        <span className="text-[10px] font-mono text-text-primary">{declaredModel}</span>
        <span className="text-[9px] text-text-muted/70">· server</span>
      </button>
    );
  }

  if (defaultModel) {
    return (
      <button
        type="button"
        onClick={openEditor}
        title={MODEL_PRECEDENCE_HINT}
        className="text-[10px] text-text-muted/60 hover:text-text-secondary transition-colors font-mono"
      >
        default: {defaultModel}
      </button>
    );
  }

  return (
    <button
      type="button"
      onClick={openEditor}
      title={MODEL_PRECEDENCE_HINT}
      className="text-[10px] text-text-muted/60 hover:text-text-secondary transition-colors"
    >
      set model
    </button>
  );
}

export default ServerModelCell;
