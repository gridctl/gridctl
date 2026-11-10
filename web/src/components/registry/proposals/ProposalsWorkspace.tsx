import { useCallback, useEffect, useId, useMemo, useState, createElement, type ReactNode } from 'react';
import { useSearchParams } from 'react-router';
import { Bot, List, Package, RefreshCw, Webhook } from 'lucide-react';
import { cn } from '../../../lib/cn';
import { WORKSPACE_LABELS } from '../../../types/workspace';
import { IconButton } from '../../ui/IconButton';
import { showToast } from '../../ui/Toast';
import { WorkspaceShell } from '../../layout/WorkspaceShell';
import { useCommandRegistry } from '../../../hooks/useCommandRegistry';
import { LibraryKindSwitch } from '../LibraryKindSwitch';
import type { RegistryKind } from '../../../lib/registryKind';
import { fetchProposals } from '../../../lib/api';
import { describeError, isCancellation } from '../../../lib/describeError';
import { escapeNonPrintable } from '../../../lib/nonPrintable';
import { formatRelativeTime } from '../../../lib/time';
import type { PaletteCommand } from '../../../types/palette';
import type { Proposal, ProposalSkipped } from '../../../types';
import { ProposalDetailPanel } from './ProposalDetailPanel';
import {
  countPending,
  filterProposals,
  isProposalStatusFilter,
  PROPOSAL_STATUS_FILTERS,
  sortProposals,
  type ProposalStatusFilter,
} from './proposalModel';

interface ProposalsWorkspaceProps {
  onKindChange: (kind: RegistryKind) => void;
}

export function ProposalsWorkspace({ onKindChange }: ProposalsWorkspaceProps) {
  const headingId = useId();
  const [searchParams, setSearchParams] = useSearchParams();
  const [proposals, setProposals] = useState<Proposal[] | null>(null);
  const [skipped, setSkipped] = useState<ProposalSkipped[]>([]);
  const [truncated, setTruncated] = useState(false);
  const statusParam = searchParams.get('status');
  const status: ProposalStatusFilter = isProposalStatusFilter(statusParam) ? statusParam : 'pending';
  const selectedId = searchParams.get('selected');

  const setStatus = useCallback((next: ProposalStatusFilter) => {
    setSearchParams((prev) => {
      const params = new URLSearchParams(prev);
      if (next === 'pending') params.delete('status');
      else params.set('status', next);
      return params;
    }, { replace: true });
  }, [setSearchParams]);

  const setSelectedId = useCallback((id: string | null) => {
    setSearchParams((prev) => {
      const params = new URLSearchParams(prev);
      if (id) params.set('selected', id);
      else params.delete('selected');
      return params;
    }, { replace: true });
  }, [setSearchParams]);

  // The list loads every status once and filters locally, so the pending
  // badge and the status filter agree without a second request.
  const refresh = useCallback(async () => {
    try {
      const doc = await fetchProposals();
      setProposals(doc.proposals ?? []);
      setSkipped(doc.skipped ?? []);
      setTruncated(Boolean(doc.truncated));
    } catch (err) {
      if (!isCancellation(err)) {
        const described = describeError(err, 'Load proposals');
        showToast('error', described.text, { detail: described.fullDetail });
      }
    }
  }, []);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- async callback; state is set only after await, not synchronously
    void refresh();
  }, [refresh]);

  const { registerCommands, unregisterCommands } = useCommandRegistry();
  useEffect(() => {
    const commands: PaletteCommand[] = [
      {
        id: 'library:proposals-refresh',
        label: 'Library: Refresh Proposals',
        section: 'registry',
        workspaces: ['library'],
        icon: createElement(RefreshCw, { size: 14 }),
        keywords: ['reload', 'refresh', 'proposals', 'proposal', 'review'],
        onSelect: () => void refresh(),
      },
      {
        id: 'library:view-skills',
        label: 'Library: View Skills',
        section: 'registry',
        workspaces: ['library'],
        icon: createElement(List, { size: 14 }),
        keywords: ['skills', 'segment', 'switch', 'kind'],
        onSelect: () => onKindChange('skill'),
      },
      {
        id: 'library:view-agents',
        label: 'Library: View Agents',
        section: 'registry',
        workspaces: ['library'],
        icon: createElement(Bot, { size: 14 }),
        keywords: ['agents', 'segment', 'switch', 'kind'],
        onSelect: () => onKindChange('agent'),
      },
      {
        id: 'library:view-hooks',
        label: 'Library: View Hooks',
        section: 'registry',
        workspaces: ['library'],
        icon: createElement(Webhook, { size: 14 }),
        keywords: ['hooks', 'segment', 'switch', 'kind'],
        onSelect: () => onKindChange('hook'),
      },
      {
        id: 'library:view-packs',
        label: 'Library: View Packs',
        section: 'registry',
        workspaces: ['library'],
        icon: createElement(Package, { size: 14 }),
        keywords: ['packs', 'segment', 'switch', 'kind'],
        onSelect: () => onKindChange('pack'),
      },
    ];
    registerCommands('library-proposals', commands);
    return () => unregisterCommands('library-proposals');
  }, [registerCommands, unregisterCommands, refresh, onKindChange]);

  const sorted = useMemo(() => sortProposals(proposals ?? []), [proposals]);
  const visible = useMemo(() => filterProposals(sorted, status), [sorted, status]);
  const pending = useMemo(() => countPending(proposals), [proposals]);
  const selected = useMemo(
    () => (selectedId ? sorted.find((p) => p.id === selectedId) ?? null : null),
    [selectedId, sorted],
  );

  return (
    <div className="absolute inset-0 flex flex-col bg-background text-text-primary overflow-hidden">
      <WorkspaceShell
        workspace="library"
        defaultRightPct={40}
        minRightPx={340}
        right={(
          <ProposalDetailPanel
            key={selected?.id ?? 'none'}
            proposal={selected}
            onClose={() => setSelectedId(null)}
            onChanged={refresh}
          />
        )}
      >
        <section aria-labelledby={headingId} className="flex flex-col h-full overflow-hidden">
          <h1 id={headingId} className="sr-only">{WORKSPACE_LABELS.library}</h1>
          <header className="flex-shrink-0 border-b border-border-subtle px-6 py-3 flex items-center justify-between gap-3">
            <LibraryKindSwitch
              kind="proposal"
              onChange={onKindChange}
              pendingProposals={proposals === null ? undefined : pending}
            />
            <IconButton icon={RefreshCw} onClick={() => void refresh()} tooltip="Refresh" size="sm" variant="ghost" />
          </header>
          <div className="px-6 py-2 flex items-center gap-1" role="group" aria-label="Proposal status filter">
            {PROPOSAL_STATUS_FILTERS.map((opt) => (
              <button
                key={opt.key}
                type="button"
                onClick={() => setStatus(opt.key)}
                aria-pressed={status === opt.key}
                className={cn(
                  'px-2 py-1 rounded-md text-2xs font-medium transition-colors border',
                  status === opt.key
                    ? 'bg-primary/10 text-primary border-primary/25'
                    : 'text-text-muted hover:text-text-secondary hover:bg-surface-highlight border-transparent',
                )}
              >
                {opt.label}
              </button>
            ))}
          </div>
          <div className="flex-1 overflow-auto px-6 pb-6 flex flex-col gap-2">
            {proposals === null && <p className="text-xs text-text-muted">Loading proposals…</p>}
            {proposals !== null && proposals.length === 0 && (
              <p className="text-xs text-text-muted">
                No proposals. Stage one with <span className="font-mono">gridctl propose</span>.
              </p>
            )}
            {proposals !== null && proposals.length > 0 && visible.length === 0 && (
              <p className="text-xs text-text-muted">No {status} proposals.</p>
            )}
            {(skipped.length > 0 || truncated) && (
              <p role="note" className="text-2xs text-status-pending">
                {skipped.length > 0 && `${skipped.length} proposal ${skipped.length === 1 ? 'file' : 'files'} could not be loaded (${[...new Set(skipped.map((s) => s.category))].join(', ')}). `}
                {truncated && 'The list stopped at the file limit.'}
              </p>
            )}
            <ul className="flex flex-col gap-2" aria-label="Proposals">
              {visible.map((p) => (
                <li key={p.id}>
                  <ProposalRow proposal={p} isActive={p.id === selectedId} onSelect={() => setSelectedId(p.id)} />
                </li>
              ))}
            </ul>
          </div>
        </section>
      </WorkspaceShell>
    </div>
  );
}

function ProposalRow({ proposal, isActive, onSelect }: { proposal: Proposal; isActive: boolean; onSelect: () => void }) {
  const findings = proposal.findings?.length ?? 0;
  return (
    <button
      type="button"
      aria-current={isActive ? 'true' : undefined}
      aria-label={`Review ${proposal.id}: ${proposal.kind} ${proposal.target}`}
      onClick={onSelect}
      className={cn(
        'w-full text-left rounded-xl border p-3 flex flex-col gap-1.5 transition-colors bg-surface/80 border-border-subtle hover:border-primary/40',
        isActive && 'border-primary/50 bg-primary/[0.06]',
      )}
    >
      <div className="flex items-center gap-2 min-w-0">
        <Chip>{proposal.kind}</Chip>
        <span className="font-semibold text-sm truncate flex-1">{escapeNonPrintable(proposal.target)}</span>
        <span className="text-3xs text-text-muted" title={proposal.created_at}>
          {formatRelativeTime(new Date(proposal.created_at))}
        </span>
      </div>
      <div className="flex items-center gap-1 flex-wrap">
        <Chip>{proposal.classification}</Chip>
        <Chip>{proposal.path}</Chip>
        <Chip tone={proposal.status === 'pending' ? 'pending' : proposal.status === 'rejected' ? 'error' : 'running'}>
          {proposal.status}
        </Chip>
        {findings > 0 && (
          <Chip tone="pending">{findings} {findings === 1 ? 'finding' : 'findings'}</Chip>
        )}
        {!proposal.base_current && proposal.status === 'pending' && <Chip tone="pending">stale base</Chip>}
        <span className="text-3xs text-text-muted ml-auto">
          {proposal.prevalence.sessions} sessions · {proposal.prevalence.runs} runs
        </span>
      </div>
      <span className="text-3xs font-mono text-text-muted">{proposal.id}</span>
    </button>
  );
}

function Chip({ children, tone }: { children: ReactNode; tone?: 'pending' | 'error' | 'running' }) {
  return (
    <span
      className={cn(
        'text-3xs px-1.5 py-0.5 rounded border',
        !tone && 'border-border/40 text-text-secondary',
        tone === 'pending' && 'border-status-pending/30 text-status-pending',
        tone === 'error' && 'border-status-error/30 text-status-error',
        tone === 'running' && 'border-status-running/30 text-status-running',
      )}
    >
      {children}
    </span>
  );
}
