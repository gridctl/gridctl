import { useCallback, useEffect, useMemo, useRef, useState } from 'react';

import { useNavigate, useSearchParams } from 'react-router';
import { Bot, Plug, Radio } from 'lucide-react';
import {
  ClientLinkError,
  fetchClients,
  fetchSessions,
  fetchWiringStatus,
  fetchAgentProjectionStatus,
  linkClient,
  unlinkClient,
} from '../../lib/api';
import { POLLING } from '../../lib/constants';
import { useContextStore } from '../../stores/useContextStore';
import { useRegistryStore } from '../../stores/useRegistryStore';
import { useStackStore } from '../../stores/useStackStore';
import { useListNav } from '../../hooks/useListNav';
import type { AgentProjectionStatus, ClientStatus, SessionEntry, WiringRow } from '../../types';
import { showToast } from '../ui/Toast';
import { GlobalContextDialog } from '../context/GlobalContextDialog';
import { ResetDialog } from '../system/ResetDialog';
import { useUIStore } from '../../stores/useUIStore';
import { WorkspaceShell } from '../layout/WorkspaceShell';
import { ClientDetailPane } from '../connections/ClientDetailPane';
import { ConnectionsRail } from '../connections/ConnectionsRail';
import { ReviewDialog } from '../connections/ReviewDialog';
import {
  attributeSessions,
  clientHealth,
  isConnected,
  sessionIdentity,
  sortClients,
  unjoinedAgentSlugs,
  type StagedChanges,
} from '../connections/connectionsModel';
import { agentClientName, statusesByClient } from '../registry/agents/agentModel';

/**
 * Connections workspace: the per-client health hub. A resizable client
 * rail (attention-first) beside a selected-client detail pane answering
 * "what did gridctl configure on this client, and is it still true?" —
 * wiring ownership, context sync, agent projections, access scope, and
 * attributed live activity. Deliberately labeled Connections, not
 * Clients: per-client access scoping (the clients: block) lives in the
 * Tools workspace.
 *
 * Toggles stage changes locally; Apply opens a review dialog with a
 * per-client config diff (nothing is written until confirmed). The
 * staged bar spans the workspace because it batches changes across
 * clients, including unselected ones.
 */
export default function ConnectionsWorkspace() {
  const [searchParams, setSearchParams] = useSearchParams();
  const clients = useStackStore((s) => s.clients);
  const sessionEntries = useStackStore((s) => s.sessionEntries);
  const contextDoc = useContextStore((s) => s.doc);
  const agentStatuses = useRegistryStore((s) => s.agentStatuses);

  const [wiringRows, setWiringRows] = useState<WiringRow[] | null>(null);
  const [sessionsFailed, setSessionsFailed] = useState(false);
  const [staged, setStaged] = useState<StagedChanges>({});
  const [reviewing, setReviewing] = useState(false);
  const [applying, setApplying] = useState(false);
  const [showContextDialog, setShowContextDialog] = useState(false);
  // The client whose drift review the dialog should open on (the pane's
  // Review action); null for a plain open.
  const [contextReviewSlug, setContextReviewSlug] = useState<string | null>(null);

  // ---- Data: wiring + agent projections + context, refreshed together. ----
  const resetDialogOpen = useUIStore((s) => s.resetDialogOpen);

  const refreshHealth = useCallback(async () => {
    const results = await Promise.allSettled([
      fetchWiringStatus(),
      fetchAgentProjectionStatus(),
      useContextStore.getState().refresh(),
      fetchClients(),
    ]);
    if (results[0].status === 'fulfilled') setWiringRows(results[0].value);
    if (results[1].status === 'fulfilled') {
      useRegistryStore.getState().setAgentStatuses(results[1].value);
    }
    if (results[3].status === 'fulfilled') {
      useStackStore.getState().setClients(results[3].value);
    }
  }, []);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- all state writes happen after awaited fetches resolve; nothing sets state synchronously in this effect
    void refreshHealth();
  }, [refreshHealth]);

  // Sessions poll into the shared store slice; StatusBar reads the same
  // array while it is loaded, so the two counts cannot diverge. Cleared
  // on unmount so the status bar falls back to the status-poll count
  // (the backend keeps the two equal by construction).
  useEffect(() => {
    let cancelled = false;
    const load = async () => {
      try {
        const res = await fetchSessions();
        if (!cancelled) {
          setSessionsFailed(false);
          useStackStore
            .getState()
            .setSessionEntries(
              res.entries ?? res.sessions?.map((id) => ({ id, generation: 'handshake' })) ?? [],
            );
        }
      } catch {
        // A failed fetch is its own fact, distinct from "still loading":
        // the pane says unavailable, and the store slice stays null so
        // the status bar keeps its honest status-poll fallback.
        if (!cancelled) setSessionsFailed(true);
      }
    };
    void load();
    const timer = window.setInterval(load, POLLING.SESSIONS);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
      useStackStore.getState().setSessionEntries(null);
    };
  }, []);

  // ---- Health join + ordering. ----
  const contextClients = contextDoc?.clients ?? null;
  const healthOf = useCallback(
    (slug: string) => clientHealth(slug, wiringRows, contextClients, agentStatuses),
    [wiringRows, contextClients, agentStatuses],
  );
  const sorted = useMemo(() => sortClients(clients, healthOf), [clients, healthOf]);

  const attentionOnly = searchParams.get('attention') === '1';
  const setAttentionOnly = useCallback(
    (on: boolean) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (on) next.set('attention', '1');
          else next.delete('attention');
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  // ---- Selection URL state (?client=), with the shipped deep-link
  // contract: wait for the load, toast-and-clear on a miss. ----
  const selectedParam = searchParams.get('client');
  const setSelectedSlug = useCallback(
    (slug: string | null) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (slug) next.set('client', slug);
          else next.delete('client');
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  const lastResolvedRef = useRef<string | null>(null);
  useEffect(() => {
    if (!selectedParam) {
      lastResolvedRef.current = null;
      return;
    }
    if (lastResolvedRef.current === selectedParam || clients.length === 0) return;
    lastResolvedRef.current = selectedParam;
    if (!clients.some((c) => c.slug === selectedParam)) {
      showToast('error', `Client "${selectedParam}" not found`);
      setSelectedSlug(null);
    }
  }, [selectedParam, clients, setSelectedSlug]);

  // ?spotlight=unlinked (the wizard's Client Link route): select the
  // first detected-but-unlinked client, then drop the param.
  useEffect(() => {
    if (searchParams.get('spotlight') !== 'unlinked' || clients.length === 0) return;
    const target = sorted.find((c) => c.detected && !isConnected(c));
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        next.delete('spotlight');
        if (target) next.set('client', target.slug);
        return next;
      },
      { replace: true },
    );
  }, [searchParams, clients, sorted, setSearchParams]);

  // Default selection when no param: first attention client, else first
  // connected, else first detected-unlinked.
  const selectedSlug = useMemo(() => {
    if (selectedParam && clients.some((c) => c.slug === selectedParam)) return selectedParam;
    const fallback =
      sorted.find((c) => healthOf(c.slug).attention) ??
      sorted.find((c) => isConnected(c)) ??
      sorted.find((c) => c.detected);
    return fallback?.slug ?? null;
  }, [selectedParam, clients, sorted, healthOf]);

  // Pin the resolved default into ?client= once per mount: the fallback
  // recomputes from health, so without pinning, an inline action that
  // clears a client's drift would silently switch the pane to a
  // different client while the user reads the result.
  const promotedDefaultRef = useRef(false);
  useEffect(() => {
    if (promotedDefaultRef.current || selectedParam || clients.length === 0) return;
    if (searchParams.get('spotlight')) return; // the spotlight effect owns selection
    if (!selectedSlug) return;
    promotedDefaultRef.current = true;
    setSelectedSlug(selectedSlug);
  }, [selectedParam, clients.length, searchParams, selectedSlug, setSelectedSlug]);

  const visibleClients = useMemo(
    () =>
      attentionOnly
        ? sorted.filter((c) => healthOf(c.slug).attention || c.slug === selectedSlug)
        : sorted,
    [attentionOnly, sorted, healthOf, selectedSlug],
  );

  // ---- Keyboard: arrows/j/k through the rail, Esc clears selection.
  // useListNav already ignores keypresses inside dialogs and inputs. ----
  const selectedIndex = visibleClients.findIndex((c) => c.slug === selectedSlug);
  useListNav({
    itemCount: visibleClients.length,
    selectedIndex: selectedIndex < 0 ? 0 : selectedIndex,
    setSelectedIndex: (i) => {
      const c = visibleClients[i];
      if (c) setSelectedSlug(c.slug);
    },
    onEscape: selectedParam ? () => setSelectedSlug(null) : undefined,
  });

  // ---- Staged connection changes (unchanged model). ----
  const changes = useMemo(
    () =>
      clients
        .filter((c) => c.slug in staged && staged[c.slug] !== isConnected(c))
        .map((c) => ({ client: c, enable: staged[c.slug] })),
    [clients, staged],
  );

  const toggle = useCallback((c: ClientStatus) => {
    setStaged((prev) => {
      const current = isConnected(c);
      const desired = !(c.slug in prev ? prev[c.slug] : current);
      const next = { ...prev };
      if (desired === current) {
        delete next[c.slug];
      } else {
        next[c.slug] = desired;
      }
      return next;
    });
  }, []);

  const apply = useCallback(async () => {
    setApplying(true);
    const failed = new Set<string>();
    for (const { client, enable } of changes) {
      try {
        if (enable) {
          await linkClient(client.slug);
        } else {
          await unlinkClient(client.slug);
        }
      } catch (err) {
        failed.add(client.slug);
        const detail =
          err instanceof ClientLinkError && err.hint
            ? `${err.message} ${err.hint}`
            : err instanceof Error
              ? err.message
              : String(err);
        showToast('error', `${client.name}: ${detail}`);
      }
    }
    if (failed.size === 0) {
      showToast('success', `Applied ${changes.length} connection change${changes.length === 1 ? '' : 's'}`);
    }
    // Failed changes stay staged so they remain visible for retry; applied
    // ones clear (the refresh below picks up their new server state).
    setStaged((prev) =>
      Object.fromEntries(Object.entries(prev).filter(([slug]) => failed.has(slug))),
    );
    setReviewing(false);
    setApplying(false);
    await refreshHealth();
  }, [changes, refreshHealth]);

  // ---- Session attribution + agent-slug join accounting. ----
  const slugSet = useMemo(() => new Set(clients.map((c) => c.slug)), [clients]);
  const attributed = useMemo(
    () => attributeSessions(sessionEntries, slugSet),
    [sessionEntries, slugSet],
  );
  const agentsByClient = useMemo(() => statusesByClient(agentStatuses), [agentStatuses]);
  // Agent targets with no client row (copilot's global agents dir is the
  // documented case): surfaced as their own strip, never silently dropped.
  const unjoinedAgents = useMemo(
    () => unjoinedAgentSlugs(agentStatuses, slugSet),
    [agentStatuses, slugSet],
  );

  const selectedClient = useMemo(
    () => clients.find((c) => c.slug === selectedSlug) ?? null,
    [clients, selectedSlug],
  );

  if (clients.length === 0) {
    return (
      <div className="absolute inset-0 flex flex-col bg-background text-text-primary overflow-hidden">
        <ConnectionsHeader subtitle="No clients reported" />
        <div className="flex-1 flex items-center justify-center">
          <div className="text-center max-w-xs">
            <div className="w-14 h-14 rounded-2xl bg-primary/10 border border-primary/20 flex items-center justify-center mx-auto mb-4 text-primary">
              <Plug size={22} />
            </div>
            <div className="text-sm font-medium text-text-primary mb-1">No client registry available</div>
            <div className="text-xs text-text-muted">
              Start a stack with 'gridctl apply' to detect and link LLM clients.
            </div>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="absolute inset-0 flex flex-col bg-background text-text-primary overflow-hidden">
      <ConnectionsHeader
        subtitle={`${clients.filter((c) => c.linked).length} linked · ${clients.filter((c) => c.detected).length} detected · access scoping lives in Tools`}
      />

      <div className="flex-1 min-h-0 relative">
        <WorkspaceShell
          workspace="connections"
          defaultLeftPct={26}
          minLeftPx={240}
          left={
            <ConnectionsRail
              clients={visibleClients}
              totalCount={clients.length}
              activeSlug={selectedSlug}
              onSelect={setSelectedSlug}
              healthOf={healthOf}
              hasLiveActivity={(slug) => (attributed.bySlug.get(slug)?.length ?? 0) > 0}
              desiredOf={(c) => (c.slug in staged ? staged[c.slug] : isConnected(c))}
              onToggle={toggle}
              attentionOnly={attentionOnly}
              onToggleAttention={() => setAttentionOnly(!attentionOnly)}
            />
          }
        >
          <div className="h-full flex flex-col overflow-hidden">
            <div className="flex-1 min-h-0 overflow-hidden">
              <ClientDetailPane
                client={selectedClient}
                health={selectedSlug ? healthOf(selectedSlug) : { attention: false, reasons: [] }}
                wiringRows={
                  wiringRows === null
                    ? null
                    : wiringRows.filter((r) => r.client === selectedSlug)
                }
                contextClient={
                  (contextClients ?? []).find((c) => c.slug === selectedSlug) ?? null
                }
                agentRows={selectedSlug ? agentsByClient.get(selectedSlug) ?? [] : []}
                sessions={
                  sessionEntries === null
                    ? null
                    : selectedSlug
                      ? attributed.bySlug.get(selectedSlug) ?? []
                      : []
                }
                sessionsFailed={sessionsFailed}
                onRefresh={refreshHealth}
                onReviewContext={() => {
                  setContextReviewSlug(selectedSlug);
                  setShowContextDialog(true);
                }}
              />
            </div>
            {unjoinedAgents.length > 0 && (
              <UnjoinedAgentsStrip
                slugs={unjoinedAgents}
                agentsByClient={agentsByClient}
              />
            )}
            {attributed.unattributed.length > 0 && (
              <UnattributedSessionsStrip sessions={attributed.unattributed} />
            )}
          </div>
        </WorkspaceShell>
      </div>

      <DangerZoneStrip onOpen={() => useUIStore.getState().setResetDialogOpen(true)} />

      {changes.length > 0 && (
        <div className="flex-shrink-0 border-t border-border-subtle bg-surface px-6 py-3">
          <div className="flex items-center justify-between">
            <span className="text-xs text-text-secondary">
              {changes.length} pending change{changes.length === 1 ? '' : 's'}
            </span>
            <div className="flex items-center gap-2">
              <button
                onClick={() => setStaged({})}
                className="px-3 py-1.5 text-xs rounded-lg text-text-secondary hover:bg-surface-highlight/50"
              >
                Discard
              </button>
              <button
                onClick={() => setReviewing(true)}
                className="px-3 py-1.5 text-xs rounded-lg bg-primary/10 text-primary border border-primary/30 hover:bg-primary/20"
              >
                Review &amp; Apply
              </button>
            </div>
          </div>
        </div>
      )}

      {reviewing && (
        <ReviewDialog
          changes={changes}
          applying={applying}
          onApply={apply}
          onClose={() => setReviewing(false)}
        />
      )}

      <ResetDialog
        isOpen={resetDialogOpen}
        onClose={() => useUIStore.getState().setResetDialogOpen(false)}
      />

      <GlobalContextDialog
        isOpen={showContextDialog}
        initialDriftSlug={contextReviewSlug}
        onClose={() => {
          setShowContextDialog(false);
          setContextReviewSlug(null);
          void refreshHealth();
        }}
      />
    </div>
  );
}

/**
 * The quiet entry point for the machine-wide reset: a collapsed one-line
 * strip, never a header affordance — the most destructive verb in the
 * product must be sought out, not stumbled into from ever-present chrome.
 */
function DangerZoneStrip({ onOpen }: { onOpen: () => void }) {
  return (
    <div className="flex-shrink-0 border-t border-border-subtle bg-surface px-6 py-2">
      <div className="flex items-center justify-between">
        <span className="text-[11px] text-text-muted">
          Danger zone: remove everything gridctl placed on this machine
        </span>
        <button
          type="button"
          onClick={onOpen}
          className="rounded-md px-2.5 py-1 text-[11px] font-medium border border-status-error/30 text-status-error/90 hover:bg-status-error/10 hover:text-status-error transition-colors"
        >
          Reset gridctl…
        </button>
      </div>
    </div>
  );
}

function ConnectionsHeader({ subtitle }: { subtitle: string }) {
  return (
    <div className="flex-shrink-0 bg-surface/30 backdrop-blur-sm border-b border-border-subtle px-6 py-3">
      <div className="flex items-baseline gap-3">
        <h1 className="text-xs font-medium uppercase tracking-[0.4em] text-text-primary">
          Connections
        </h1>
        <span className="font-mono text-[10px] text-text-muted">{subtitle}</span>
      </div>
    </div>
  );
}

/**
 * Agent projections targeting surfaces that are not linkable clients
 * (copilot's global agents directory is the documented case). They have
 * no rail row to live under, so they get their own strip — never
 * silently dropped from the hub.
 */
function UnjoinedAgentsStrip({
  slugs,
  agentsByClient,
}: {
  slugs: string[];
  agentsByClient: Map<string, AgentProjectionStatus[]>;
}) {
  const navigate = useNavigate();
  const [expanded, setExpanded] = useState(false);
  const total = slugs.reduce((n, s) => n + (agentsByClient.get(s)?.length ?? 0), 0);
  return (
    <div className="flex-shrink-0 border-t border-border-subtle bg-surface/40">
      <button
        onClick={() => setExpanded((e) => !e)}
        aria-expanded={expanded}
        className="w-full flex items-center gap-2 px-5 py-2 text-left hover:bg-surface-highlight/40 transition-colors"
      >
        <Bot size={12} className="text-text-muted/70 flex-shrink-0" aria-hidden="true" />
        <span className="text-[11px] text-text-muted">
          {total} agent projection{total === 1 ? '' : 's'} on non-client targets (
          {slugs.map(agentClientName).join(', ')})
        </span>
      </button>
      {expanded && (
        <ul className="px-5 pb-2 max-h-40 overflow-y-auto scrollbar-dark">
          {slugs.flatMap((slug) =>
            (agentsByClient.get(slug) ?? []).map((row) => (
              <li key={`${slug}:${row.agent}`} className="flex items-center gap-2 py-0.5">
                <span className="text-[10px] text-text-muted w-28 truncate flex-shrink-0">
                  {agentClientName(slug)}
                </span>
                <button
                  onClick={() =>
                    navigate(`/library?kind=agent&selected=${encodeURIComponent(row.agent)}`)
                  }
                  title={`Open ${row.agent} in the Library`}
                  className="text-xs font-mono text-text-secondary hover:text-primary transition-colors truncate"
                >
                  {row.agent}
                </button>
                <span className="text-[10px] text-text-muted/70 ml-auto">{row.state}</span>
              </li>
            )),
          )}
        </ul>
      )}
    </div>
  );
}

/**
 * Sessions the UI cannot attribute to a linked client, in their own
 * honest bucket with synthesized identities — never force-matched to a
 * guess, never silently dropped.
 */
function UnattributedSessionsStrip({ sessions }: { sessions: SessionEntry[] }) {
  const [expanded, setExpanded] = useState(false);
  return (
    <div className="flex-shrink-0 border-t border-border-subtle bg-surface/40">
      <button
        onClick={() => setExpanded((e) => !e)}
        aria-expanded={expanded}
        className="w-full flex items-center gap-2 px-5 py-2 text-left hover:bg-surface-highlight/40 transition-colors"
      >
        <Radio size={12} className="text-text-muted/70 flex-shrink-0" aria-hidden="true" />
        <span className="text-[11px] text-text-muted">
          {sessions.length} session{sessions.length === 1 ? '' : 's'} not matched to a linked client
        </span>
      </button>
      {expanded && (
        <ul className="px-5 pb-2 max-h-40 overflow-y-auto scrollbar-dark">
          {sessions.map((s) => (
            <li key={s.id} className="flex justify-between items-center py-0.5">
              <span className="text-xs font-mono text-text-secondary truncate max-w-[260px]" title={s.id}>
                {sessionIdentity(s)}
              </span>
              <span className="text-[10px] px-2 py-0.5 rounded-md font-mono font-medium uppercase tracking-wider bg-secondary/10 text-secondary">
                {s.generation}
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
