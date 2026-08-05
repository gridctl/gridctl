import { useCallback, useState, type ReactNode } from 'react';
import { useNavigate } from 'react-router';
import { Bot, Cable, ChevronDown, ChevronRight, Copy, Globe, Plug, Radio, Wrench } from 'lucide-react';
import { cn } from '../../lib/cn';
import { ConfirmDialog } from '../ui/ConfirmDialog';
import { StatePill } from '../ui/StatePill';
import { PackChip } from '../registry/PackChip';
import { showToast } from '../ui/Toast';
import {
  ClientLinkError,
  adoptWiringEntry,
  linkClient,
  syncGlobalContext,
  unsyncGlobalContext,
  type ContextClientStatus,
} from '../../lib/api';
import { AGENT_TARGET_SLUGS, agentClientName } from '../registry/agents/agentModel';
import type { AgentProjectionStatus, ClientStatus, SessionEntry, WiringRow } from '../../types';
import { ClientBrandIcon, Badges } from './ConnectionsRail';
import { sessionIdentity, type ClientHealth } from './connectionsModel';

interface ClientDetailPaneProps {
  client: ClientStatus | null;
  health: ClientHealth;
  /** null while wiring status has not loaded (or its fetch failed) — the
   *  section must say so rather than claiming "no entries recorded". */
  wiringRows: WiringRow[] | null;
  contextClient: ContextClientStatus | null;
  agentRows: AgentProjectionStatus[];
  /** null while the sessions fetch has not resolved; an empty array is a
   *  real "no attributed activity" fact. */
  sessions: SessionEntry[] | null;
  /** True when the sessions fetch failed: unavailable, not loading. */
  sessionsFailed?: boolean;
  onRefresh: () => Promise<void> | void;
  onReviewContext: () => void;
}

/**
 * The selected client's health pane: what did gridctl configure on this
 * client, and is it still true? A summary surface in the ClientsStrip
 * grammar — collapsible sections that auto-expand on attention — with
 * heavy content deep-linked to the surfaces that own it (Tools for
 * scope, the Global Context dialog for fragment review, the Library for
 * agent bodies). Ownership state and live activity are two separately
 * labeled axes, never one merged health light.
 */
export function ClientDetailPane({
  client,
  health,
  wiringRows,
  contextClient,
  agentRows,
  sessions,
  sessionsFailed = false,
  onRefresh,
  onReviewContext,
}: ClientDetailPaneProps) {
  const navigate = useNavigate();
  const [busy, setBusy] = useState(false);
  const [confirmAdopt, setConfirmAdopt] = useState<WiringRow | null>(null);
  const [confirmOverwrite, setConfirmOverwrite] = useState<WiringRow | null>(null);

  const act = useCallback(
    async (fn: () => Promise<unknown>, okMessage: string) => {
      setBusy(true);
      try {
        await fn();
        showToast('success', okMessage);
        await onRefresh();
      } catch (err) {
        // Failures render the server's message verbatim. Link failures
        // arrive as structured ClientLinkError with a remediation hint
        // (append it); wiring adopt 409s carry the engine's reason in the
        // plain error message already.
        const detail =
          err instanceof ClientLinkError && err.hint
            ? `${err.message} ${err.hint}`
            : err instanceof Error
              ? err.message
              : 'Action failed';
        showToast('error', detail);
      } finally {
        setBusy(false);
      }
    },
    [onRefresh],
  );

  if (!client) {
    return (
      <div className="h-full flex flex-col items-center justify-center gap-2 text-text-muted p-6 text-center">
        <Plug size={24} className="text-text-muted/40" />
        <p className="text-sm">Select a client to inspect it</p>
      </div>
    );
  }

  const isAgentTarget = AGENT_TARGET_SLUGS.has(client.slug);
  const ownershipAttention = health.attention;
  const scope = client.effectiveScope;
  const attributedSessions = sessions ?? [];

  return (
    // Keyed by slug so section expand/collapse state never leaks from one
    // client to the next: auto-expand-on-attention must fire per client.
    <div key={client.slug} className="h-full overflow-y-auto scrollbar-dark">
      {/* Header: identity, then the two axes side by side, separately labeled. */}
      <div className="px-5 py-4 border-b border-border-subtle bg-surface-elevated/30">
        <div className="flex items-center gap-3">
          <span className="w-10 h-10 rounded-xl bg-surface-elevated border border-border-subtle flex items-center justify-center text-text-secondary flex-shrink-0">
            <ClientBrandIcon slug={client.slug} size={20} />
          </span>
          <span className="min-w-0 flex-1">
            <span className="flex items-center gap-2">
              <h2 className="font-semibold text-text-primary truncate tracking-tight">{client.name}</h2>
              <Badges client={client} />
            </span>
            {client.configPath && (
              <button
                onClick={() => {
                  void navigator.clipboard?.writeText(client.configPath ?? '');
                  showToast('success', 'Config path copied');
                }}
                title="Copy config path"
                className="mt-0.5 flex items-center gap-1 font-mono text-[10px] text-text-muted hover:text-text-secondary transition-colors max-w-full"
              >
                <span className="truncate">{client.configPath}</span>
                <Copy size={9} className="flex-shrink-0" />
              </button>
            )}
          </span>
        </div>
        <div className="mt-3 flex items-center gap-4 flex-wrap">
          <AxisBadge
            label="Ownership"
            tone={wiringRows === null ? 'muted' : ownershipAttention ? 'pending' : 'ok'}
            text={
              wiringRows === null
                ? 'loading'
                : ownershipAttention
                  ? health.reasons.join(' · ')
                  : wiringRows.length === 0
                    ? 'nothing placed yet'
                    : 'everything gridctl placed is in sync'
            }
          />
          <AxisBadge
            label="Activity"
            tone={attributedSessions.length > 0 ? 'live' : 'muted'}
            text={
              sessions === null
                ? sessionsFailed
                  ? 'unavailable'
                  : 'loading'
                : attributedSessions.length > 0
                  ? `${attributedSessions.length} active session${attributedSessions.length === 1 ? '' : 's'}`
                  : 'no attributed sessions'
            }
          />
        </div>
      </div>

      {/* Wiring: the gateway entry's ownership state. */}
      <Section
        icon={<Plug size={13} />}
        title="Wiring"
        summary={
          wiringRows === null
            ? 'loading'
            : wiringRows.length === 0
              ? 'no entries recorded'
              : wiringRows.map((r) => `${r.name}: ${r.state}`).join(' · ')
        }
        attention={(wiringRows ?? []).some((r) => r.state !== 'in-sync' && r.state !== 'missing')}
      >
        {wiringRows === null && (
          <p className="text-[11px] text-text-muted px-1">
            Wiring state has not loaded. It appears once the ownership status fetch resolves.
          </p>
        )}
        {wiringRows !== null && wiringRows.length === 0 && (
          <p className="text-[11px] text-text-muted px-1">
            No gateway entry recorded for this client. Use the connect toggle to link it.
          </p>
        )}
        {(wiringRows ?? []).map((row) => (
          <div key={row.name} className="flex flex-col gap-1 px-1 py-1.5">
            <div className="flex items-center gap-2 flex-wrap">
              <StatePill state={row.state} />
              <span className="text-xs text-text-primary font-mono">{row.name}</span>
              {row.pack && <PackChip pack={row.pack} />}
              <span className="ml-auto flex items-center gap-1.5">
                {/* Actions match what the API can actually do per state.
                    Adopt records the entry's CURRENT value, so it cannot
                    apply to target-missing (nothing exists to adopt).
                    Plain Re-link succeeds only on stale and
                    target-missing; drifted and foreign entries need the
                    explicit Overwrite (force) with its confirm — a plain
                    Re-link there would systematically 409. */}
                {(row.state === 'stale' || row.state === 'drifted' || row.state === 'foreign') && (
                  <InlineAction label="Adopt" disabled={busy} onClick={() => setConfirmAdopt(row)} />
                )}
                {(row.state === 'stale' || row.state === 'target-missing') && (
                  <InlineAction
                    label="Re-link"
                    subtle
                    disabled={busy}
                    onClick={() =>
                      void act(() => linkClient(client.slug), `${client.name} re-linked`)
                    }
                  />
                )}
                {(row.state === 'drifted' || row.state === 'foreign') && (
                  <InlineAction
                    label="Overwrite"
                    subtle
                    disabled={busy}
                    onClick={() => setConfirmOverwrite(row)}
                  />
                )}
              </span>
            </div>
            {row.detail && <span className="text-[11px] text-text-muted/80 px-1">{row.detail}</span>}
            {/* The engine's remediation is the user's map when a state
                needs a decision; hiding it until an action fails would
                make the failure the discovery mechanism. */}
            {row.remediation && row.state !== 'in-sync' && (
              <span className="text-[11px] text-text-muted/60 px-1">{row.remediation}</span>
            )}
            {row.target && (
              <span className="text-[11px] text-text-muted font-mono truncate px-1" title={row.target}>
                {row.target}
              </span>
            )}
          </div>
        ))}
      </Section>

      {/* Context: the global context file gridctl syncs to this client. */}
      <Section
        icon={<Globe size={13} />}
        title="Context"
        summary={contextClient ? contextSummary(contextClient) : 'unknown'}
        attention={
          !!contextClient && ['stale', 'drifted', 'target-missing'].includes(contextClient.state)
        }
      >
        {!contextClient && (
          <p className="text-[11px] text-text-muted px-1">Context state not loaded.</p>
        )}
        {contextClient && (
          <div className="flex items-center gap-2 flex-wrap px-1 py-1.5">
            <StatePill state={contextClient.state} />
            {contextClient.mode && (
              <span className="text-[10px] px-1.5 py-0.5 rounded border border-border/40 bg-background/40 text-text-muted font-mono whitespace-nowrap">
                {contextClient.mode}
              </span>
            )}
            <span
              className="text-[11px] text-text-muted font-mono truncate flex-1"
              title={contextClient.target_path ?? contextClient.detail}
            >
              {contextClient.supported ? contextClient.target_path : contextClient.detail}
            </span>
            <span className="flex items-center gap-1.5">
              {contextClient.state === 'drifted' && (
                <InlineAction label="Review" disabled={busy} onClick={onReviewContext} />
              )}
              {(contextClient.state === 'stale' ||
                contextClient.state === 'target-missing' ||
                (contextClient.state === 'never-synced' && contextClient.available)) && (
                <InlineAction
                  label={busy ? 'Syncing…' : 'Sync'}
                  disabled={busy}
                  onClick={() =>
                    void act(
                      () => syncGlobalContext({ clients: [client.slug] }),
                      `${client.name} context synced`,
                    )
                  }
                />
              )}
              {['in-sync', 'stale', 'drifted'].includes(contextClient.state) && (
                <InlineAction
                  label="Unsync"
                  subtle
                  disabled={busy}
                  onClick={() =>
                    void act(
                      () => unsyncGlobalContext(client.slug),
                      `${client.name} context unsynced`,
                    )
                  }
                />
              )}
            </span>
          </div>
        )}
        {contextClient && (contextClient.fragments ?? []).length > 0 && (
          <ul
            className="flex flex-col gap-1 px-1 pb-1.5"
            aria-label={`${client.name} out-of-sync context fragments`}
          >
            {(contextClient.fragments ?? []).map((f) => (
              <li key={f.name} className="flex items-center gap-2">
                <StatePill state={f.state} />
                <span className="text-[11px] text-text-secondary font-mono truncate flex-1">
                  {f.name}
                </span>
                {f.pack && <PackChip pack={f.pack} />}
              </li>
            ))}
          </ul>
        )}
      </Section>

      {/* Agents: projections this client receives. */}
      <Section
        icon={<Bot size={13} />}
        title="Agents"
        summary={
          !isAgentTarget
            ? 'not a projection target'
            : agentRows.length === 0
              ? 'none projected'
              : `${agentRows.length} projected`
        }
        attention={agentRows.some((r) => r.state !== 'in-sync')}
      >
        {!isAgentTarget && (
          <p className="text-[11px] text-text-muted px-1" data-testid="not-agent-target">
            Not an agent projection target: agent definitions project to{' '}
            {[...AGENT_TARGET_SLUGS].map(agentClientName).join(', ')}.
          </p>
        )}
        {isAgentTarget && agentRows.length === 0 && (
          <p className="text-[11px] text-text-muted px-1">
            No agents projected to this client yet. Manage projections from the Library's Agents
            segment.
          </p>
        )}
        {agentRows.map((row) => (
          <div key={row.agent} className="flex items-center gap-2 flex-wrap px-1 py-1.5">
            <StatePill state={row.state} />
            <span
              className="text-[10px] px-1.5 py-0.5 rounded border border-border/40 bg-background/40 text-text-muted font-mono whitespace-nowrap"
              title={row.render === 'lossy' ? 'Client-dialect render; unmappable keys dropped' : 'Canonical bytes copied verbatim'}
            >
              {row.render}
            </span>
            <button
              onClick={() => navigate(`/library?kind=agent&selected=${encodeURIComponent(row.agent)}`)}
              className="text-xs text-text-primary hover:text-primary transition-colors font-mono truncate"
              title={`Open ${row.agent} in the Library`}
            >
              {row.agent}
            </button>
            {row.pack && <PackChip pack={row.pack} />}
            {row.detail && (
              <span className="text-[10px] text-text-muted/70 truncate" title={row.detail}>
                {row.detail}
              </span>
            )}
          </div>
        ))}
      </Section>

      {/* Access scope: a one-liner deep-linking into Tools. */}
      <Section
        icon={<Wrench size={13} />}
        title="Access scope"
        summary={
          !scope || !scope.configured
            ? 'full surface (no clients: block)'
            : scope.unscoped
              ? 'full surface'
              : `${scope.servers.length} server${scope.servers.length === 1 ? '' : 's'}, ${scope.tools.length} tool${scope.tools.length === 1 ? '' : 's'}`
        }
        attention={false}
      >
        <div className="flex items-center gap-2 px-1 py-1.5">
          <span className="text-[11px] text-text-muted flex-1">
            {!scope || !scope.configured
              ? 'No per-client scoping is configured; this client reaches every exposed tool.'
              : scope.unscoped
                ? 'This client is unscoped and reaches every exposed tool.'
                : `Reaches ${scope.servers.length} server${scope.servers.length === 1 ? '' : 's'} (${scope.tools.length} tool${scope.tools.length === 1 ? '' : 's'}).`}
          </span>
          <InlineAction
            label="Edit in Tools"
            subtle
            onClick={() => navigate(`/tools?client=${encodeURIComponent(client.slug)}`)}
          />
        </div>
      </Section>

      {/* Sessions: live transport activity attributed to this client. */}
      <Section
        icon={<Radio size={13} />}
        title="Sessions"
        summary={
          sessions === null
            ? sessionsFailed
              ? 'unavailable'
              : 'loading'
            : attributedSessions.length === 0
              ? 'none attributed'
              : `${attributedSessions.length} active`
        }
        attention={false}
      >
        {sessions === null && (
          <p className="text-[11px] text-text-muted px-1">
            {sessionsFailed
              ? 'Session state is unavailable: the sessions endpoint failed (or the daemon predates it).'
              : 'Session state has not loaded yet.'}
          </p>
        )}
        {sessions !== null && attributedSessions.length === 0 && (
          <p className="text-[11px] text-text-muted px-1" data-testid="sessionless-copy">
            No attributed sessions. Stateless-generation clients (2026-07-28) are sessionless by
            design and never appear here.
          </p>
        )}
        {attributedSessions.map((s) => (
          <div key={s.id} className="flex justify-between items-center px-1 py-1">
            <span className="text-xs font-mono text-text-secondary truncate max-w-[220px]" title={s.id}>
              {sessionIdentity(s)}
            </span>
            <span className="flex items-center gap-2">
              {s.protocolVersion && (
                <span className="text-[10px] font-mono text-text-muted">{s.protocolVersion}</span>
              )}
              <span className="text-[10px] px-2 py-0.5 rounded-md font-mono font-medium uppercase tracking-wider bg-secondary/10 text-secondary">
                {s.generation}
              </span>
            </span>
          </div>
        ))}
        <p className="text-[10px] text-text-muted/60 px-1 pt-1 flex items-center gap-1">
          <Cable size={9} /> Transport state, not declared links.
        </p>
      </Section>

      <ConfirmDialog
        isOpen={confirmAdopt !== null}
        onClose={() => setConfirmAdopt(null)}
        onConfirm={() => {
          const row = confirmAdopt;
          setConfirmAdopt(null);
          if (!row) return;
          void act(
            () => adoptWiringEntry(client.slug, row.name),
            `Adopted ${client.name}'s '${row.name}' entry`,
          );
        }}
        title="Adopt entry"
        message={
          <>
            <p>
              Record ownership of <span className="font-mono text-primary">{confirmAdopt?.name}</span>{' '}
              in {client.name}'s config as it currently stands?
            </p>
            <p>
              Adopt keeps the file untouched and records its current value as gridctl-owned, so
              future unlinks and drift checks treat it as gridctl's entry.
            </p>
          </>
        }
        confirmLabel="Adopt"
      />

      <ConfirmDialog
        isOpen={confirmOverwrite !== null}
        onClose={() => setConfirmOverwrite(null)}
        onConfirm={() => {
          const row = confirmOverwrite;
          setConfirmOverwrite(null);
          if (!row) return;
          void act(
            () => linkClient(client.slug, { force: true }),
            `${client.name}'s '${row.name}' entry rewritten from gridctl`,
          );
        }}
        title="Overwrite entry"
        message={
          <>
            <p>
              Rewrite <span className="font-mono text-primary">{confirmOverwrite?.name}</span> in{' '}
              {client.name}'s config with gridctl's gateway entry?
            </p>
            <p>
              The current value was {confirmOverwrite?.state === 'foreign' ? 'not written by gridctl' : 'edited after gridctl wrote it'};
              the engine backs it up before overwriting. Adopt instead if the current value should
              become the recorded one.
            </p>
          </>
        }
        confirmLabel="Overwrite"
        variant="danger"
      />
    </div>
  );
}

function AxisBadge({
  label,
  tone,
  text,
}: {
  label: string;
  tone: 'ok' | 'pending' | 'live' | 'muted';
  text: string;
}) {
  return (
    <span className="flex items-center gap-1.5 min-w-0">
      <span className="text-[9px] uppercase tracking-[0.2em] text-text-muted/70 flex-shrink-0">
        {label}
      </span>
      <span
        className={cn(
          'text-[10px] px-2 py-0.5 rounded-full border font-medium truncate',
          tone === 'ok' && 'text-emerald-400 border-emerald-400/25 bg-emerald-400/10',
          tone === 'pending' && 'text-status-pending border-status-pending/30 bg-status-pending/10',
          tone === 'live' && 'text-secondary border-secondary/25 bg-secondary/10',
          tone === 'muted' && 'text-text-muted border-border/40 bg-background/40',
        )}
      >
        {text}
      </span>
    </span>
  );
}

/**
 * One collapsible detail section in the ClientsStrip grammar: a one-line
 * summary when collapsed, auto-expanded when its content needs the
 * user's attention. Never force-closes a strip the user opened.
 */
function Section({
  icon,
  title,
  summary,
  attention,
  children,
}: {
  icon: ReactNode;
  title: string;
  summary: string;
  attention: boolean;
  children: ReactNode;
}) {
  const [expanded, setExpanded] = useState(attention);
  const [prevAttention, setPrevAttention] = useState(attention);
  if (attention !== prevAttention) {
    setPrevAttention(attention);
    if (attention) setExpanded(true);
  }

  return (
    <div className="border-b border-border/30">
      <button
        onClick={() => setExpanded((e) => !e)}
        aria-expanded={expanded}
        className="w-full flex items-center gap-2 px-5 py-2.5 text-left hover:bg-surface-highlight/40 transition-colors"
      >
        {expanded ? (
          <ChevronDown size={13} className="text-text-muted flex-shrink-0" />
        ) : (
          <ChevronRight size={13} className="text-text-muted flex-shrink-0" />
        )}
        <span className="text-text-muted/70 flex-shrink-0" aria-hidden="true">
          {icon}
        </span>
        <span className="text-xs text-text-muted uppercase tracking-wider">{title}</span>
        <span className="text-[11px] text-text-muted/80 truncate flex-1">{summary}</span>
        {attention && !expanded && (
          <span className="flex-shrink-0 text-[9px] font-medium uppercase tracking-wider px-1.5 py-0.5 rounded-full border border-status-pending/30 bg-status-pending/10 text-status-pending">
            Needs attention
          </span>
        )}
      </button>
      {expanded && <div className="px-5 pb-3 flex flex-col gap-0.5">{children}</div>}
    </div>
  );
}

function InlineAction({
  label,
  onClick,
  disabled,
  subtle,
}: {
  label: string;
  onClick: () => void;
  disabled?: boolean;
  subtle?: boolean;
}) {
  return (
    <button
      onClick={onClick}
      disabled={disabled}
      className={cn(
        'px-2 py-0.5 rounded-md text-[11px] font-medium border transition-colors disabled:opacity-50 whitespace-nowrap',
        subtle
          ? 'text-text-muted border-border/40 hover:bg-surface-highlight'
          : 'text-primary border-primary/25 hover:bg-primary/10',
      )}
    >
      {label}
    </button>
  );
}

/**
 * Section summary for the Context row: the state, plus how many fragments
 * drifted when the per-fragment status reports any.
 */
function contextSummary(c: ContextClientStatus): string {
  const drifted = (c.fragments ?? []).filter((f) => f.state === 'drifted').length;
  if (!drifted) return c.state;
  return `${c.state} (${drifted} fragment${drifted === 1 ? '' : 's'})`;
}
