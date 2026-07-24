import { useCallback, useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router';
import {
  CheckCircle2,
  Clock,
  Loader2,
  Lock,
  LockOpen,
  Minus,
  Pin,
  Plus,
  RefreshCw,
} from 'lucide-react';
import { cn } from '../../lib/cn';
import { usePinsStore } from '../../stores/usePinsStore';
import {
  approveServerPins,
  fetchPinsDiff,
  fetchServerPins,
  type PinsDiff,
  type ServerPins,
} from '../../lib/api';
import { escapeNonPrintable, shortPinHash } from '../../lib/nonPrintable';
import { formatRelativeTime } from '../../lib/time';
import { useListNav } from '../../hooks/useListNav';
import { useUIStore } from '../../stores/useUIStore';
import { WorkspaceShell } from '../layout/WorkspaceShell';
import { pinStatusMeta } from '../pins/pinStatus';
import { FindingsList, FindingsSummaryBadge } from '../pins/PinFindings';
import { showToast } from '../ui/Toast';

// PinsWorkspace is the schema-pinning surface, sibling to Stack, Library,
// Variables, Tools, and Metrics. The left rail lists pinned servers (drifted
// first); the center pane shows the selected server's drift diff (when any)
// with the Approve action beside it, followed by its pinned tool records.
// The diff is fetched from GET /api/pins/{server}/diff, which recomputes the
// delta against live tools and never mutates pin state - approval is the only
// mutating action, and it always sits next to the rendered diff.
export function PinsWorkspace() {
  const [searchParams, setSearchParams] = useSearchParams();
  const compact = useUIStore((s) => s.compactMode.pins);
  const pins = usePinsStore((s) => s.pins);

  // Drifted servers first, then alphabetical, for a stable rail order that
  // surfaces what needs attention.
  const entries = useMemo(() => {
    if (!pins) return [];
    return Object.entries(pins).sort(([aName, a], [bName, b]) => {
      const aDrift = a.status === 'drift' ? 0 : 1;
      const bDrift = b.status === 'drift' ? 0 : 1;
      if (aDrift !== bDrift) return aDrift - bDrift;
      return aName.localeCompare(bName);
    });
  }, [pins]);

  const serverParam = searchParams.get('server') ?? '';
  const activeServerName = useMemo(() => {
    if (entries.some(([name]) => name === serverParam)) return serverParam;
    return entries[0]?.[0] ?? '';
  }, [entries, serverParam]);

  const activePins = useMemo(
    () => entries.find(([name]) => name === activeServerName)?.[1] ?? null,
    [entries, activeServerName],
  );

  const applyServer = useCallback(
    (name: string) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          next.set('server', name);
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  const selectedIndex = entries.findIndex(([name]) => name === activeServerName);
  useListNav({
    itemCount: entries.length,
    selectedIndex: selectedIndex < 0 ? 0 : selectedIndex,
    setSelectedIndex: (i) => {
      const name = entries[i]?.[0];
      if (name) applyServer(name);
    },
  });

  // null means the first /api/pins poll has not landed yet; the endpoint
  // returns an empty object (not an error) when pinning is disabled.
  if (pins === null) {
    return (
      <PinsEmptyState
        icon={<Loader2 size={24} className="text-primary/70 animate-spin" />}
        title="Loading pins…"
        body="Fetching pin state from the gateway."
      />
    );
  }

  if (entries.length === 0) {
    return (
      <PinsEmptyState
        icon={<Pin size={24} className="text-primary/70" />}
        title="No servers pinned yet"
        body="Servers are pinned automatically on first verify after deploy. If schema pinning is disabled in your stack, nothing will appear here."
      />
    );
  }

  return (
    <div className="absolute inset-0 flex flex-col bg-background text-text-primary overflow-hidden">
      <WorkspaceShell
        workspace="pins"
        defaultLeftPct={20}
        left={
          <ServerRail
            compact={compact}
            entries={entries}
            activeServerName={activeServerName}
            onSelect={applyServer}
          />
        }
        minLeftPx={220}
      >
        <main className="flex flex-col h-full overflow-hidden">
          <header
            className={cn(
              'flex-shrink-0 bg-surface/30 backdrop-blur-sm border-b border-border-subtle px-6 flex items-center gap-3',
              compact ? 'py-2' : 'py-3',
            )}
          >
            <div className="font-sans text-text-muted/60 text-[10px] uppercase tracking-[0.4em]">
              pins
            </div>
            <div className="font-mono text-[10px] text-text-muted">
              {entries.length} {entries.length === 1 ? 'server' : 'servers'} pinned
            </div>
          </header>

          <div className="flex-1 min-h-0 overflow-y-auto scrollbar-dark">
            {activePins && (
              <ServerDetail key={activeServerName} name={activeServerName} pins={activePins} />
            )}
          </div>
        </main>
      </WorkspaceShell>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Left rail
// ---------------------------------------------------------------------------

interface ServerRailProps {
  compact: boolean;
  entries: Array<[string, ServerPins]>;
  activeServerName: string;
  onSelect: (name: string) => void;
}

function ServerRail({ compact, entries, activeServerName, onSelect }: ServerRailProps) {
  return (
    <aside className="h-full flex flex-col bg-surface border-r border-border-subtle">
      <div
        className={cn(
          'flex-shrink-0 px-3 border-b border-border-subtle/60',
          compact ? 'py-2' : 'py-3',
        )}
      >
        <div className="text-[10px] font-medium text-text-muted/60 uppercase tracking-[0.3em]">
          servers
        </div>
      </div>

      <div className="flex-1 min-h-0 overflow-y-auto scrollbar-dark px-2 py-2 space-y-0.5">
        {entries.map(([name, sp]) => {
          const active = name === activeServerName;
          const { label, colorClass } = pinStatusMeta(sp.status);
          return (
            <button
              key={name}
              onClick={() => onSelect(name)}
              aria-current={active}
              className={cn(
                'w-full flex items-center gap-2 px-3 py-1.5 rounded-md text-left transition-colors',
                active
                  ? 'bg-primary/10 text-primary'
                  : 'text-text-secondary hover:bg-surface-highlight/50 hover:text-text-primary',
              )}
            >
              <span className={cn('flex-shrink-0', colorClass)}>
                {sp.status === 'drift' ? <LockOpen size={11} /> : <Lock size={11} />}
              </span>
              <span
                className={cn('flex-1 min-w-0 text-xs font-mono truncate', active && 'text-primary')}
              >
                {name}
              </span>
              <span
                className={cn(
                  'flex-shrink-0 text-[10px] px-1.5 py-0.5 rounded',
                  sp.status === 'drift'
                    ? 'bg-status-pending/15 text-status-pending'
                    : 'bg-surface-elevated text-text-muted',
                )}
              >
                {label}
              </span>
            </button>
          );
        })}
      </div>
    </aside>
  );
}

// ---------------------------------------------------------------------------
// Server detail - drift diff (when drifted) + pinned tool records
// ---------------------------------------------------------------------------

function ServerDetail({ name, pins: sp }: { name: string; pins: ServerPins }) {
  const { label, colorClass } = pinStatusMeta(sp.status);
  const toolRecords = useMemo(
    () => Object.values(sp.tools ?? {}).sort((a, b) => a.name.localeCompare(b.name)),
    [sp.tools],
  );

  return (
    <div className="px-6 py-4 max-w-3xl space-y-4">
      <div className="flex items-center gap-3">
        <h2 className="text-sm font-mono text-text-primary">{name}</h2>
        <span className={cn('flex items-center gap-1.5 text-xs', colorClass)}>
          {sp.status === 'drift' ? <LockOpen size={11} /> : <Lock size={11} />}
          {label}
        </span>
        <span className="flex items-center gap-1 text-[11px] text-text-muted ml-auto">
          <Clock size={10} className="text-text-muted/60" />
          {sp.last_verified_at
            ? `verified ${formatRelativeTime(new Date(sp.last_verified_at))}`
            : 'never verified'}
        </span>
      </div>

      <div className="flex items-center gap-4 text-[11px] text-text-muted">
        <span>
          <span className="text-text-secondary font-medium">{sp.tool_count}</span> tools pinned
        </span>
        {sp.pinned_at && <span>first pinned {formatRelativeTime(new Date(sp.pinned_at))}</span>}
      </div>

      {sp.status === 'drift' && <DriftSection serverName={name} />}

      <section className="space-y-2">
        <h3 className="text-[10px] uppercase tracking-[0.18em] text-text-muted/70">
          Pinned tool records
        </h3>
        <div className="rounded-lg border border-border/40 bg-background/60 overflow-hidden">
          <table className="w-full text-xs border-collapse">
            <thead>
              <tr className="border-b border-border/30">
                <th className="text-left px-3 py-2 text-text-muted font-medium">Tool</th>
                <th className="text-left px-3 py-2 text-text-muted font-medium">Hash</th>
                <th className="text-left px-3 py-2 text-text-muted font-medium">Pinned</th>
              </tr>
            </thead>
            <tbody>
              {toolRecords.map((rec) => (
                <tr key={rec.name} className="border-b border-border/20 last:border-b-0">
                  <td className="px-3 py-2 align-top">
                    <div className="flex items-center gap-2">
                      <span className="font-mono text-text-primary">{escapeNonPrintable(rec.name)}</span>
                      <FindingsSummaryBadge findings={rec.findings} />
                    </div>
                    {rec.description && (
                      <div className="text-[10px] text-text-muted mt-0.5 whitespace-pre-wrap break-words">
                        {escapeNonPrintable(rec.description)}
                      </div>
                    )}
                    {rec.findings && rec.findings.length > 0 && (
                      <div className="mt-1.5">
                        <FindingsList findings={rec.findings} />
                      </div>
                    )}
                  </td>
                  <td className="px-3 py-2 align-top font-mono text-text-muted whitespace-nowrap">
                    {shortPinHash(rec.hash)}
                  </td>
                  <td className="px-3 py-2 align-top text-text-muted whitespace-nowrap">
                    {rec.pinned_at ? formatRelativeTime(new Date(rec.pinned_at)) : '—'}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Drift diff + informed approve
// ---------------------------------------------------------------------------

function DriftSection({ serverName }: { serverName: string }) {
  const [diff, setDiff] = useState<PinsDiff | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [isApproving, setIsApproving] = useState(false);
  // Bumped by Retry and by a failed approve so the effect refetches.
  const [attempt, setAttempt] = useState(0);

  // No reset needed on server change: ServerDetail is keyed by server name,
  // so this section remounts (with null state) whenever the selection moves.
  // Retry resets state in its click handler before bumping `attempt`.
  useEffect(() => {
    let cancelled = false;
    fetchPinsDiff(serverName)
      .then((d) => {
        if (!cancelled) setDiff(d);
      })
      .catch((err) => {
        if (!cancelled) setError(err instanceof Error ? err.message : 'Failed to load diff');
      });
    return () => {
      cancelled = true;
    };
  }, [serverName, attempt]);

  const reloadDiff = () => {
    setDiff(null);
    setError(null);
    setAttempt((n) => n + 1);
  };

  const handleApprove = async () => {
    if (!diff) return;
    setIsApproving(true);
    try {
      // Bind the approval to the reviewed snapshot: the gateway rejects with
      // 409 if the live definitions no longer hash to what was rendered here.
      await approveServerPins(serverName, diff.live_server_hash);
      const updated = await fetchServerPins();
      usePinsStore.getState().setPins(updated);
      showToast('success', `Pins approved for ${serverName}`);
    } catch (err) {
      showToast('error', `Failed to approve: ${err instanceof Error ? err.message : 'Unknown error'}`);
      // The definitions may have changed again since review; reload the diff
      // so the user re-reviews the current state instead of a stale one.
      reloadDiff();
    } finally {
      setIsApproving(false);
    }
  };

  const changeCount =
    (diff?.modified_tools.length ?? 0) +
    (diff?.new_tools.length ?? 0) +
    (diff?.removed_tools.length ?? 0);

  return (
    <section
      className="rounded-lg border border-status-pending/30 bg-status-pending/[0.04] px-4 py-3 space-y-3"
      aria-label={`Schema drift for ${serverName}`}
    >
      <div className="flex items-center gap-2">
        <LockOpen size={12} className="text-status-pending flex-shrink-0" />
        <h3 className="text-xs font-medium text-status-pending">Schema drift</h3>
        <span className="text-[10px] text-text-muted">
          {diff ? `${changeCount} ${changeCount === 1 ? 'change' : 'changes'}` : ''}
        </span>
        <button
          onClick={handleApprove}
          disabled={isApproving || diff === null}
          title={
            diff === null
              ? 'Review the changes below before approving'
              : 'Re-pin the live definitions shown below'
          }
          className={cn(
            'ml-auto flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-medium transition-all duration-200',
            isApproving || diff === null
              ? 'text-text-muted bg-surface-highlight/30 cursor-not-allowed'
              : 'text-status-running bg-status-running/10 border border-status-running/20 hover:bg-status-running/20',
          )}
        >
          {isApproving ? (
            <>
              <RefreshCw size={10} className="animate-spin" />
              Approving…
            </>
          ) : (
            <>
              <CheckCircle2 size={10} />
              Approve {diff ? `${changeCount} ${changeCount === 1 ? 'change' : 'changes'}` : ''}
            </>
          )}
        </button>
      </div>

      {error && (
        <div role="alert" className="flex items-center gap-2 text-[11px] text-status-error">
          <span className="min-w-0">{error}</span>
          <button
            onClick={reloadDiff}
            className="flex-shrink-0 inline-flex items-center gap-1 rounded-md border border-border/40 bg-background/40 px-2 py-0.5 text-[10px] text-text-secondary hover:text-text-primary hover:border-border transition-colors"
          >
            <RefreshCw size={10} />
            Retry
          </button>
        </div>
      )}

      {!diff && !error && (
        <p className="flex items-center gap-2 text-[11px] text-text-muted">
          <Loader2 size={11} className="animate-spin" />
          Comparing pinned definitions against live tools…
        </p>
      )}

      {diff && (
        <div className="space-y-3">
          {diff.modified_tools.map((d) => (
            <div
              key={d.name}
              className="rounded-md border border-border/40 bg-background/60 px-3 py-2 space-y-1.5"
            >
              <div className="text-xs font-mono text-text-primary">{escapeNonPrintable(d.name)}</div>
              <DiffRow kind="old" hash={d.old_hash} description={d.old_description} />
              <DiffRow kind="new" hash={d.new_hash} description={d.new_description} />
              <FindingsList findings={d.findings} />
            </div>
          ))}

          {diff.new_tools.length > 0 && (
            <div className="flex flex-wrap items-center gap-1.5 text-[11px] text-text-muted">
              <Plus size={11} className="text-status-running" />
              <span>New tools (pinned on approve):</span>
              {diff.new_tools.map((n) => (
                <span key={n} className="font-mono text-text-secondary">
                  {escapeNonPrintable(n)}
                </span>
              ))}
            </div>
          )}

          {diff.removed_tools.length > 0 && (
            <div className="flex flex-wrap items-center gap-1.5 text-[11px] text-text-muted">
              <Minus size={11} className="text-status-error" />
              <span>Removed from server:</span>
              {diff.removed_tools.map((n) => (
                <span key={n} className="font-mono text-text-secondary">
                  {escapeNonPrintable(n)}
                </span>
              ))}
            </div>
          )}

          {changeCount === 0 && (
            <p className="text-[11px] text-text-muted">
              The live definitions match the pins again; approving will simply re-verify.
            </p>
          )}
        </div>
      )}
    </section>
  );
}

// One side of a before/after pair. Descriptions render as plain text with
// control characters escaped - they can carry prompt-injection payloads and
// must never be interpreted as markup.
function DiffRow({
  kind,
  hash,
  description,
}: {
  kind: 'old' | 'new';
  hash: string;
  description: string;
}) {
  return (
    <div className="flex items-start gap-2 text-[11px]">
      <span
        className={cn(
          'flex-shrink-0 w-8 font-mono uppercase',
          kind === 'old' ? 'text-status-error/80' : 'text-status-running/80',
        )}
      >
        {kind}
      </span>
      <span className="flex-shrink-0 font-mono text-text-muted">{shortPinHash(hash)}</span>
      <span className="min-w-0 text-text-secondary whitespace-pre-wrap break-words">
        {description ? escapeNonPrintable(description) : <em className="text-text-muted/60">no description</em>}
      </span>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Empty states
// ---------------------------------------------------------------------------

function PinsEmptyState({
  icon,
  title,
  body,
}: {
  icon: React.ReactNode;
  title: string;
  body: string;
}) {
  return (
    <div className="absolute inset-0 flex items-center justify-center bg-background px-6 py-12">
      <div className="max-w-md w-full text-center space-y-4">
        <div className="mx-auto w-14 h-14 rounded-2xl bg-primary/10 border border-primary/20 flex items-center justify-center">
          {icon}
        </div>
        <div className="space-y-1.5">
          <h2 className="text-base font-semibold text-text-primary">{title}</h2>
          <p className="text-xs text-text-muted leading-relaxed">{body}</p>
        </div>
      </div>
    </div>
  );
}

export default PinsWorkspace;
