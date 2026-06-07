import { useCallback, useState } from 'react';
import { Users, Server, Globe } from 'lucide-react';
import { SlideOver } from '../ui/SlideOver';
import { showToast } from '../ui/Toast';
import { updateDefaultModel } from '../../lib/api';
import { ModelPicker } from './ModelPicker';
import { ClientModelCell } from './ClientModelCell';
import { ServerModelCell } from './ServerModelCell';
import { EffectiveModelTag } from './EffectiveModelTag';
import { MODEL_PRECEDENCE_HINT, SNAPSHOT_NOTE } from './constants';
import type { EffectiveModel } from '../../types';

export interface PricingManagerSlideOverProps {
  open: boolean;
  onClose: () => void;
  /** Gateway-level default_model; empty when not configured. */
  defaultModel: string;
  /** Every server in the stack with its DECLARED model (model: field only). */
  servers: Array<{ name: string; declaredModel?: string }>;
  /** Declared clients plus clients observed in cost/token data. */
  clients: Array<{ name: string; declaredModel?: string }>;
  costAttribution: boolean;
  /** Read-only effective model + provenance per client / server (optional). */
  effectiveClientModels?: Record<string, EffectiveModel>;
  effectiveServerModels?: Record<string, EffectiveModel>;
  onClientSaved: (client: string, model: string) => void;
  onServerSaved: (server: string, model: string) => void;
  onDefaultSaved: (model: string) => void;
}

/**
 * PricingManagerSlideOver is the canonical editor for all three pricing
 * attribution tiers, listed in precedence order: client models, server
 * models, gateway default. Each row is the same inline editor the metrics
 * tables use, so a save here behaves identically (optimistic update via the
 * host's onSaved, hot reload, 409 surfacing). Data flows in through props so
 * the main window can host it from the store while the detached metrics
 * window hosts it from local state.
 *
 * Non-modal by design (no focus trap, no backdrop): the canvas and the cost
 * chart stay live behind it so an edit's effect is visible on the next poll.
 */
export function PricingManagerSlideOver({
  open,
  onClose,
  defaultModel,
  servers,
  clients,
  costAttribution,
  effectiveClientModels,
  effectiveServerModels,
  onClientSaved,
  onServerSaved,
  onDefaultSaved,
}: PricingManagerSlideOverProps) {
  return (
    <SlideOver isOpen={open} onClose={onClose} title="Pricing models" widthClass="w-[400px]">
      <div className="flex flex-col gap-5 px-4 py-4">
        <p className="text-[11px] text-text-muted leading-relaxed">{MODEL_PRECEDENCE_HINT}</p>

        <TierSection
          icon={<Users size={12} className="text-text-muted" />}
          title="1 · Client models"
          note="Pricing only. Does not create access restrictions or require a clients: block."
        >
          {clients.length === 0 ? (
            <p className="text-[10px] text-text-muted/60 italic">
              No clients observed yet. Clients appear after their first tool call.
            </p>
          ) : (
            clients.map((c) => (
              <TierRow key={c.name} name={c.name} effective={c.declaredModel ? effectiveClientModels?.[c.name] : undefined}>
                {/* The manager's job is editing, so the cell keeps its
                    editable affordance for undeclared rows; mixed/none
                    provenance surfaces in the read-only Effective sub-line
                    (drift) rather than replacing the editor. */}
                <ClientModelCell
                  client={c.name}
                  declaredModel={c.declaredModel}
                  costAttribution={costAttribution}
                  onSaved={onClientSaved}
                />
              </TierRow>
            ))
          )}
        </TierSection>

        <TierSection
          icon={<Server size={12} className="text-text-muted" />}
          title="2 · Server models"
          note="A server without its own model inherits the gateway default."
        >
          {servers.length === 0 ? (
            <p className="text-[10px] text-text-muted/60 italic">No MCP servers in the stack.</p>
          ) : (
            servers.map((s) => (
              <TierRow key={s.name} name={s.name} effective={s.declaredModel ? effectiveServerModels?.[s.name] : undefined}>
                <ServerModelCell
                  server={s.name}
                  declaredModel={s.declaredModel}
                  defaultModel={defaultModel}
                  onSaved={onServerSaved}
                />
              </TierRow>
            ))
          )}
        </TierSection>

        <TierSection
          icon={<Globe size={12} className="text-text-muted" />}
          title="3 · Gateway default"
          note="Stack-wide floor: prices every server without its own model."
        >
          <DefaultModelRow defaultModel={defaultModel} onSaved={onDefaultSaved} />
        </TierSection>

        <div className="border-t border-border/30 pt-3 space-y-1.5">
          <p className="text-[10px] text-text-muted leading-relaxed">{SNAPSHOT_NOTE}</p>
          <p className="text-[10px] text-text-muted leading-relaxed">
            Unknown IDs record tokens but price as $0. A declared client model is a session
            default and cannot observe mid-session model switches.
          </p>
        </div>
      </div>
    </SlideOver>
  );
}

function TierSection({
  icon,
  title,
  note,
  children,
}: {
  icon: React.ReactNode;
  title: string;
  note: string;
  children: React.ReactNode;
}) {
  return (
    <section>
      <div className="flex items-center gap-1.5 mb-1">
        {icon}
        <h3 className="text-xs font-medium text-text-secondary">{title}</h3>
      </div>
      <p className="text-[11px] text-text-muted mb-2 leading-relaxed">{note}</p>
      <div className="space-y-1.5">{children}</div>
    </section>
  );
}

function TierRow({
  name,
  effective,
  children,
}: {
  name: string;
  effective?: EffectiveModel;
  children: React.ReactNode;
}) {
  // A declared row whose traffic actually priced under a blend of models is
  // the drift signal worth surfacing read-only beside the editable cell.
  const showDrift = effective?.provenance === 'mixed';
  return (
    <div className="flex flex-col gap-0.5 min-h-[26px] justify-center">
      <div className="flex items-center justify-between gap-3">
        <span className="text-xs font-mono text-text-primary truncate" title={name}>
          {name}
        </span>
        <div className="flex-shrink-0">{children}</div>
      </div>
      {showDrift && effective && (
        <div className="flex items-center justify-between gap-3">
          <span className="text-[9px] text-text-muted/60 uppercase tracking-wider">Effective</span>
          <div className="flex-shrink-0">
            <EffectiveModelTag effective={effective} />
          </div>
        </div>
      )}
    </div>
  );
}

// DefaultModelRow is the gateway-tier twin of the client/server cells: pill
// with "· default" provenance, inline ModelPicker editing, empty save clears
// gateway.default_model (and the gateway: block when the clear empties it).
function DefaultModelRow({
  defaultModel,
  onSaved,
}: {
  defaultModel: string;
  onSaved: (model: string) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  const save = useCallback(
    async (model: string) => {
      if (model === defaultModel) {
        setEditing(false);
        return;
      }
      setSaving(true);
      setSaveError(null);
      try {
        const resp = await updateDefaultModel(model);
        onSaved(model);
        setEditing(false);
        showToast('success', model === '' ? 'Gateway default model cleared' : 'Gateway default model saved');
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
    [defaultModel, onSaved],
  );

  if (editing) {
    return (
      <span className="inline-flex items-center gap-1">
        <ModelPicker
          value={defaultModel}
          onCommit={(model) => void save(model)}
          onCancel={() => setEditing(false)}
          disabled={saving}
          autoFocus
          widthClass="w-56"
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

  if (defaultModel) {
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
        <span className="text-[10px] font-mono text-text-primary">{defaultModel}</span>
        <span className="text-[9px] text-text-muted/70">· default</span>
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
      className="text-[10px] text-secondary hover:text-secondary-light transition-colors"
    >
      set default model
    </button>
  );
}

export default PricingManagerSlideOver;
