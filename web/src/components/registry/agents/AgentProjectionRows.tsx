import { useCallback, useState } from 'react';
import { useNavigate } from 'react-router';
import { Cable } from 'lucide-react';
import { cn } from '../../../lib/cn';
import { ProjectedModelChip } from '../ModelChip';
import { Modal } from '../../ui/Modal';
import { StatePill } from '../../ui/StatePill';
import { showToast } from '../../ui/Toast';
import {
  adoptAgentProjection,
  syncAgentProjections,
  unsyncAgentProjections,
} from '../../../lib/api';
import { agentClientName, describeSyncResults } from './agentModel';
import type { AgentProjectionStatus } from '../../../types';

interface AgentProjectionRowsProps {
  agentName: string;
  /** This agent's rows; empty means never projected. */
  statuses: AgentProjectionStatus[];
  onRefresh: () => Promise<void> | void;
}

/**
 * Per-client projection rows for one agent, in the shipped ClientsStrip /
 * ClientRow grammar: state pill, render chip, client name with an inline
 * experimental suffix, truncated target path, and inline text-button
 * actions. Adopt is only ever offered on identity rows; lossy rows get an
 * always-visible explanation with the two real alternatives as buttons —
 * never a disabled button whose reason hides in a tooltip.
 */
export function AgentProjectionRows({ agentName, statuses, onRefresh }: AgentProjectionRowsProps) {
  const [busy, setBusy] = useState(false);
  const [driftReview, setDriftReview] = useState<AgentProjectionStatus | null>(null);

  const act = useCallback(
    async (fn: () => Promise<unknown>, okMessage: string) => {
      setBusy(true);
      try {
        await fn();
        showToast('success', okMessage);
        await onRefresh();
      } catch (err) {
        showToast('error', err instanceof Error ? err.message : 'Action failed');
      } finally {
        setBusy(false);
      }
    },
    [onRefresh],
  );

  // Sync toasts classify the engine's actual result rows: skips carry no
  // error field, so an error-keyed toast would announce success while
  // nothing was written (undetected client, drifted copy).
  const runSync = useCallback(
    async (body: { agents: string[]; clients?: string[]; force?: boolean }) => {
      setBusy(true);
      try {
        const results = await syncAgentProjections(body);
        const { kind, message } = describeSyncResults(results);
        showToast(kind, message);
        await onRefresh();
      } catch (err) {
        showToast('error', err instanceof Error ? err.message : 'Sync failed');
      } finally {
        setBusy(false);
      }
    },
    [onRefresh],
  );

  const syncOne = useCallback(
    (client: string) => runSync({ agents: [agentName], clients: [client] }),
    [runSync, agentName],
  );

  const unsyncOne = useCallback(
    (client: string) =>
      act(
        () => unsyncAgentProjections({ agents: [agentName], clients: [client] }),
        `${agentName} unsynced from ${agentClientName(client)}`,
      ),
    [act, agentName],
  );

  if (statuses.length === 0) {
    return (
      <div className="flex flex-col items-center gap-3 py-8 text-center" data-testid="agent-never-synced">
        <p className="text-sm text-text-secondary">Not projected to any client yet</p>
        <p className="text-[11px] text-text-muted max-w-xs">
          Syncing copies this agent into each detected client's agents directory
          (verbatim for Claude Code, rendered for other clients).
        </p>
        <button
          onClick={() => void runSync({ agents: [agentName] })}
          disabled={busy}
          className="px-3 py-1.5 text-xs font-medium text-primary bg-primary/10 border border-primary/25 rounded-lg hover:bg-primary/15 transition-colors disabled:opacity-50"
        >
          {busy ? 'Syncing…' : 'Sync now'}
        </button>
      </div>
    );
  }

  // The identity target comes from the status rows, never a hardcoded
  // slug: a second identity target (or a renamed one) must keep the
  // adopt-alternative button working without a code change here.
  const identityClient = statuses.find((s) => s.render === 'identity')?.client ?? null;

  return (
    <>
      <ul className="divide-y divide-border/20">
        {statuses.map((s) => (
          <ProjectionRow
            key={s.client}
            status={s}
            busy={busy}
            onSync={() => void syncOne(s.client)}
            onUnsync={() => void unsyncOne(s.client)}
            onReviewDrift={() => setDriftReview(s)}
          />
        ))}
      </ul>
      {driftReview && (
        <AgentDriftDialog
          agentName={agentName}
          status={driftReview}
          identityClient={identityClient}
          busy={busy}
          onClose={() => setDriftReview(null)}
          onAdopt={() =>
            void act(async () => {
              const res = await adoptAgentProjection(agentName, driftReview.client);
              setDriftReview(null);
              return res;
            }, `Adopted ${agentClientName(driftReview.client)}'s edit into the canonical AGENT.md`)
          }
          onAdoptIdentity={() => {
            if (!identityClient) return;
            void act(async () => {
              const res = await adoptAgentProjection(agentName, identityClient);
              setDriftReview(null);
              return res;
            }, `Adopted the ${agentClientName(identityClient)} copy into the canonical AGENT.md`);
          }}
          onOverwrite={() => {
            setDriftReview(null);
            void runSync({ agents: [agentName], clients: [driftReview.client], force: true });
          }}
          onUnsync={() =>
            void act(async () => {
              await unsyncAgentProjections({ agents: [agentName], clients: [driftReview.client] });
              setDriftReview(null);
            }, `${agentName} unsynced from ${agentClientName(driftReview.client)}`)
          }
        />
      )}
    </>
  );
}

function ProjectionRow({
  status: s,
  busy,
  onSync,
  onUnsync,
  onReviewDrift,
}: {
  status: AgentProjectionStatus;
  busy: boolean;
  onSync: () => void;
  onUnsync: () => void;
  onReviewDrift: () => void;
}) {
  const navigate = useNavigate();
  return (
    <li className="flex flex-col gap-1 px-1 py-2">
      {/* flex-wrap: at the 300px right-rail minimum the chips stack onto the
          next line instead of squeezing the client name out. */}
      <div className="flex items-center gap-2 flex-wrap">
        <StatePill state={s.state} />
        <span
          title={s.render === 'lossy' ? 'Client-dialect render; unmappable frontmatter keys are dropped' : 'Canonical bytes copied verbatim'}
          className="text-[10px] px-1.5 py-0.5 rounded border border-border/40 bg-background/40 text-text-muted font-mono whitespace-nowrap"
        >
          {s.render}
        </span>
        <ProjectedModelChip value={s.model_value} />
        <span className="text-xs text-text-primary whitespace-nowrap">
          {agentClientName(s.client)}
          {s.experimental && <span className="ml-1 text-[10px] text-text-secondary">(experimental)</span>}
        </span>
        <button
          onClick={() => navigate(`/connections?client=${encodeURIComponent(s.client)}`)}
          title={`Open ${agentClientName(s.client)} in Connections`}
          aria-label={`Open ${agentClientName(s.client)} in Connections`}
          className="p-0.5 rounded text-text-muted/60 hover:text-primary transition-colors flex-shrink-0"
        >
          <Cable size={12} />
        </button>
        <span className="ml-auto flex items-center gap-1.5">
          {(s.state === 'stale' || s.state === 'target-missing') && (
            <RowAction label={busy ? 'Syncing…' : 'Sync'} disabled={busy} onClick={onSync} />
          )}
          {s.state === 'drifted' && (
            <RowAction label="Review" disabled={busy} onClick={onReviewDrift} />
          )}
          <RowAction label="Unsync" subtle disabled={busy} onClick={onUnsync} />
        </span>
      </div>
      <span className="text-[11px] text-text-muted font-mono truncate" title={s.target}>
        {s.target}
      </span>
      {s.detail && (
        <span className="text-[11px] text-text-muted/80" data-testid="projection-detail">
          {s.detail}
        </span>
      )}
    </li>
  );
}

function RowAction({
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
        'px-2 py-0.5 rounded-md text-[11px] font-medium border transition-colors disabled:opacity-50',
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
 * Drift resolution for one (agent, client) projection, mirroring the
 * context DriftResolveDialog's three-way model. On an identity target the
 * hand edit can be adopted into the canonical store (the previous
 * AGENT.md is backed up as AGENT.md.pre-<sha> first). On a lossy target
 * adopt is impossible by design — the dialect dropped canonical keys at
 * render time — so the dialog states that plainly and offers the two real
 * alternatives as buttons.
 */
function AgentDriftDialog({
  agentName,
  status,
  identityClient,
  busy,
  onClose,
  onAdopt,
  onAdoptIdentity,
  onOverwrite,
  onUnsync,
}: {
  agentName: string;
  status: AgentProjectionStatus;
  /** Slug of the identity-render row, or null when none is projected. */
  identityClient: string | null;
  busy: boolean;
  onClose: () => void;
  onAdopt: () => void;
  onAdoptIdentity: () => void;
  onOverwrite: () => void;
  onUnsync: () => void;
}) {
  const clientName = agentClientName(status.client);
  const lossy = status.render === 'lossy';
  return (
    <Modal isOpen onClose={onClose} title={`${clientName}'s copy of ${agentName} was edited`}>
      <div className="flex flex-col gap-3">
        {lossy ? (
          <p className="text-xs text-text-muted" data-testid="adopt-refusal">
            {clientName}'s projection is a lossy render; edits there can't sync back into the
            canonical AGENT.md without corrupting it with client-dialect content.
            {identityClient
              ? ` Adopt the ${agentClientName(identityClient)} copy instead, or unsync to hand-maintain this file.`
              : ' Unsync to hand-maintain this file, or overwrite it from the canonical store.'}
          </p>
        ) : (
          <p className="text-xs text-text-muted">
            The projected file at <span className="font-mono">{status.target}</span> differs from
            the canonical AGENT.md. Adopting overwrites the canonical file with the edit; the
            previous version is backed up beside it as{' '}
            <span className="font-mono">AGENT.md.pre-&lt;sha&gt;</span> first. Overwriting
            restores the client's copy from the canonical store instead.
          </p>
        )}
        <div className="flex items-center justify-end gap-2 flex-wrap">
          <button
            onClick={onClose}
            disabled={busy}
            className="px-3 py-1.5 text-xs text-text-muted border border-border/40 rounded-lg hover:bg-surface-highlight transition-colors"
          >
            Cancel
          </button>
          <button
            onClick={onUnsync}
            disabled={busy}
            className="px-3 py-1.5 text-xs text-text-muted border border-border/40 rounded-lg hover:bg-surface-highlight transition-colors"
          >
            Unsync
          </button>
          {lossy ? (
            identityClient !== null && (
              <button
                onClick={onAdoptIdentity}
                disabled={busy}
                className="px-3 py-1.5 text-xs font-medium text-primary bg-primary/10 border border-primary/25 rounded-lg hover:bg-primary/15 transition-colors disabled:opacity-50"
              >
                Adopt {agentClientName(identityClient)} copy
              </button>
            )
          ) : (
            <button
              onClick={onAdopt}
              disabled={busy}
              className="px-3 py-1.5 text-xs font-medium text-primary bg-primary/10 border border-primary/25 rounded-lg hover:bg-primary/15 transition-colors disabled:opacity-50"
            >
              Adopt into canon
            </button>
          )}
          <button
            onClick={onOverwrite}
            disabled={busy}
            className="px-3 py-1.5 text-xs font-medium text-red-400 border border-red-400/25 rounded-lg hover:bg-red-400/10 transition-colors disabled:opacity-50"
          >
            Overwrite client
          </button>
        </div>
      </div>
    </Modal>
  );
}
