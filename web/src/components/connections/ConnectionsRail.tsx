import { createElement } from 'react';
import { CheckCircle2, ShieldAlert } from 'lucide-react';
import { cn } from '../../lib/cn';
import { getClientIcon } from '../../lib/clientIcons';
import type { ClientStatus } from '../../types';
import { isConnected, type ClientHealth } from './connectionsModel';

// ClientBrandIcon resolves the brand mark per render via createElement so no
// component value is created during render (react-hooks/static-components).
export function ClientBrandIcon({ slug, size }: { slug: string; size?: number }) {
  return createElement(getClientIcon(slug), { size });
}

export function Badges({ client }: { client: ClientStatus }) {
  return (
    <span className="flex items-center gap-1">
      {client.linked && (
        <span className="text-[9px] uppercase tracking-wide px-1.5 py-0.5 rounded-full bg-status-running/15 text-status-running">
          Linked
        </span>
      )}
      {client.declared && (
        <span className="text-[9px] uppercase tracking-wide px-1.5 py-0.5 rounded-full bg-primary/15 text-primary">
          Declared
        </span>
      )}
      {client.drifted && (
        <span className="text-[9px] uppercase tracking-wide px-1.5 py-0.5 rounded-full bg-status-warning/15 text-status-warning">
          Drifted
        </span>
      )}
      {client.detected && !client.linked && (
        <span className="text-[9px] uppercase tracking-wide px-1.5 py-0.5 rounded-full bg-surface-elevated text-text-muted">
          Detected
        </span>
      )}
    </span>
  );
}

/** The inline connect toggle, unchanged from the single-column page:
 *  toggling connectivity is the one action users take without opening
 *  detail, and the switch is muscle memory. */
export function ConnectToggle({
  client,
  desired,
  onToggle,
}: {
  client: ClientStatus;
  desired: boolean;
  onToggle: () => void;
}) {
  const canLink = client.detected;
  const staged = desired !== isConnected(client);
  return (
    <button
      role="switch"
      aria-checked={desired}
      aria-label={`Link ${client.name}`}
      disabled={!canLink && !desired && !staged}
      onClick={(e) => {
        e.stopPropagation();
        onToggle();
      }}
      title={!canLink && !desired ? 'Client not detected on this machine' : undefined}
      className={cn(
        'relative w-9 h-5 rounded-full transition-colors flex-shrink-0 border',
        desired
          ? 'bg-primary border-primary'
          : 'bg-text-muted/20 border-text-muted/30 opacity-70',
        !canLink && !desired && !staged && 'opacity-40 cursor-not-allowed',
      )}
    >
      <span
        className={cn(
          'absolute top-0.5 left-0 w-4 h-4 rounded-full bg-white shadow-sm transition-transform',
          desired ? 'translate-x-[18px]' : 'translate-x-0.5',
        )}
      />
    </button>
  );
}

interface ConnectionsRailProps {
  /** Pre-sorted (attention-first) and attention-filtered client rows. */
  clients: ClientStatus[];
  totalCount: number;
  activeSlug: string | null;
  onSelect: (slug: string) => void;
  healthOf: (slug: string) => ClientHealth;
  hasLiveActivity: (slug: string) => boolean;
  desiredOf: (client: ClientStatus) => boolean;
  onToggle: (client: ClientStatus) => void;
  attentionOnly: boolean;
  onToggleAttention: () => void;
}

/**
 * Left rail of the Connections hub: one row per client with brand icon,
 * link badges, an ownership attention glyph, a quiet live-activity dot,
 * and the inline connect toggle. Attention filter mirrors PinsRail.
 */
export function ConnectionsRail({
  clients,
  totalCount,
  activeSlug,
  onSelect,
  healthOf,
  hasLiveActivity,
  desiredOf,
  onToggle,
  attentionOnly,
  onToggleAttention,
}: ConnectionsRailProps) {
  return (
    <aside className="h-full flex flex-col bg-surface border-r border-border-subtle">
      <div className="flex-shrink-0 px-3 py-3 border-b border-border-subtle/60 flex items-center justify-between gap-2">
        <span className="text-[10px] font-medium uppercase tracking-[0.3em] text-text-muted">
          Clients
        </span>
        <button
          onClick={onToggleAttention}
          aria-pressed={attentionOnly}
          title={
            attentionOnly
              ? 'Show all clients'
              : 'Show only clients with wiring, context, or agent drift'
          }
          className={cn(
            'h-6 px-2 text-[10px] font-medium rounded border transition-colors flex items-center gap-1 flex-shrink-0',
            attentionOnly
              ? 'bg-status-pending/15 text-status-pending border-status-pending/30'
              : 'bg-background/60 text-text-muted border-border/40 hover:text-text-secondary hover:border-border/60',
          )}
        >
          <ShieldAlert size={9} />
          Attention
        </button>
      </div>

      <div className="flex-1 min-h-0 overflow-y-auto scrollbar-dark px-2 py-2 space-y-0.5">
        {clients.map((c) => {
          const health = healthOf(c.slug);
          return (
            // A div with button semantics rather than a <button>: the row
            // contains the connect toggle, and nested interactive controls
            // inside a native button are invalid HTML with broken keyboard
            // and AT behavior (same pattern as the Library's AgentCard).
            <div
              key={c.slug}
              role="button"
              tabIndex={0}
              onClick={() => onSelect(c.slug)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault();
                  onSelect(c.slug);
                }
              }}
              aria-label={`Inspect ${c.name}`}
              aria-current={c.slug === activeSlug}
              className={cn(
                'w-full flex items-center gap-2.5 px-2.5 py-2 rounded-md text-left transition-colors cursor-pointer',
                c.slug === activeSlug
                  ? 'bg-primary/10'
                  : 'hover:bg-surface-highlight/50',
              )}
            >
              <span className="relative w-7 h-7 rounded-lg bg-surface-elevated border border-border-subtle flex items-center justify-center text-text-secondary flex-shrink-0">
                <ClientBrandIcon slug={c.slug} size={14} />
                {hasLiveActivity(c.slug) && (
                  <span
                    title="Live session activity"
                    className="absolute -top-0.5 -right-0.5 w-1.5 h-1.5 rounded-full bg-secondary"
                  >
                    <span className="sr-only">Live session activity</span>
                  </span>
                )}
              </span>
              <span className="flex-1 min-w-0">
                <span className="flex items-center gap-1.5">
                  <span
                    className={cn(
                      'text-xs font-medium truncate',
                      c.slug === activeSlug ? 'text-primary' : 'text-text-primary',
                      !c.detected && 'text-text-muted',
                    )}
                  >
                    {c.name}
                  </span>
                  {health.attention && (
                    <span
                      className="flex-shrink-0 text-status-pending"
                      title={health.reasons.join(' · ')}
                      aria-label={`Needs attention: ${health.reasons.join(', ')}`}
                    >
                      <ShieldAlert size={10} />
                    </span>
                  )}
                </span>
                <span className="block mt-0.5">
                  <Badges client={c} />
                </span>
              </span>
              <ConnectToggle client={c} desired={desiredOf(c)} onToggle={() => onToggle(c)} />
            </div>
          );
        })}

        {attentionOnly && clients.length === 0 && (
          <div className="px-3 py-6 text-center space-y-2">
            <CheckCircle2 size={16} className="mx-auto text-status-running/70" />
            <p className="text-[11px] text-text-muted">All clear: no wiring, context, or agent drift.</p>
            <button onClick={onToggleAttention} className="text-[11px] text-primary hover:underline">
              Show all {totalCount} {totalCount === 1 ? 'client' : 'clients'}
            </button>
          </div>
        )}
      </div>
    </aside>
  );
}
