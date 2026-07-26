import { useMemo, useState } from 'react';
import { Clock, Lock, LockOpen, RefreshCw, ShieldAlert, Trash2 } from 'lucide-react';
import { cn } from '../../lib/cn';
import { fetchPinsDiff, fetchServerPins, type ServerPins } from '../../lib/api';
import { usePinsStore } from '../../stores/usePinsStore';
import { escapeNonPrintable, shortPinHash } from '../../lib/nonPrintable';
import { formatRelativeTime } from '../../lib/time';
import { pinStatusMeta } from './pinStatus';
import { FindingsList, FindingsSummaryBadge } from './PinFindings';
import { DriftSection, REVERIFY_BUTTON_ID } from './PinsDriftSection';
import { ResetPinsDialog } from './ResetPinsDialog';
import { showToast } from '../ui/Toast';

// Scroll target for ?view=tools and ?view=findings (the findings live inside
// the pinned-records table).
export const TOOLS_SECTION_ID = 'pins-tools-section';

// Palette dispatch target for the destructive reset affordance.
export const RESET_BUTTON_ID = 'pins-reset-button';

// ---------------------------------------------------------------------------
// Server detail - drift diff (when drifted) + pinned tool records
// ---------------------------------------------------------------------------

interface PinsServerDetailProps {
  name: string;
  pins: ServerPins;
  findingsOnly: boolean;
  onToggleFindingsOnly: () => void;
  onReset: (serverName: string) => Promise<void>;
  /** A ?view=findings landing intent: start with finding bodies expanded. */
  expandFindingsOnMount: boolean;
}

export function PinsServerDetail({
  name,
  pins: sp,
  findingsOnly,
  onToggleFindingsOnly,
  onReset,
  expandFindingsOnMount,
}: PinsServerDetailProps) {
  const { label, colorClass } = pinStatusMeta(sp.status);
  const [resetOpen, setResetOpen] = useState(false);
  const [isResetting, setIsResetting] = useState(false);
  const [isRefreshing, setIsRefreshing] = useState(false);
  // Findings are advisory context on the audit table, collapsed behind their
  // badges; the drift diff is the approval subject and stays expanded. A
  // findings deep link is an explicit "show me the findings", so that landing
  // starts expanded instead of making the user re-open every row.
  const [expandedFindings, setExpandedFindings] = useState<Set<string>>(() =>
    expandFindingsOnMount
      ? new Set(
          Object.values(sp.tools ?? {})
            .filter((rec) => (rec.findings?.length ?? 0) > 0)
            .map((rec) => rec.name),
        )
      : new Set(),
  );

  const toolRecords = useMemo(
    () => Object.values(sp.tools ?? {}).sort((a, b) => a.name.localeCompare(b.name)),
    [sp.tools],
  );
  const flaggedRecords = useMemo(
    () => toolRecords.filter((rec) => (rec.findings?.length ?? 0) > 0),
    [toolRecords],
  );
  const visibleRecords = findingsOnly ? flaggedRecords : toolRecords;
  const hasDrift = sp.status === 'drift';

  const toggleRowFindings = (toolName: string) => {
    setExpandedFindings((prev) => {
      const next = new Set(prev);
      if (next.has(toolName)) {
        next.delete(toolName);
      } else {
        next.add(toolName);
      }
      return next;
    });
  };

  // A real re-verify, not just a store refresh: the diff endpoint recomputes
  // against the server's live tools (read-only), so drift that appeared since
  // the gateway's last verify cycle is reported honestly instead of a false
  // "still pinned" assurance.
  const handleRefresh = async () => {
    setIsRefreshing(true);
    try {
      const [diff, updated] = await Promise.all([fetchPinsDiff(name), fetchServerPins()]);
      usePinsStore.getState().setPins(updated);
      const changes =
        diff.modified_tools.length + diff.new_tools.length + diff.removed_tools.length;
      if (changes > 0) {
        showToast(
          'warning',
          `Live definitions differ from the pins (${changes} ${changes === 1 ? 'change' : 'changes'}); the gateway surfaces drift on its next verify`,
        );
      } else {
        showToast('success', 'Verified: live definitions match the pins');
      }
    } catch {
      showToast('error', 'Failed to verify against live tools');
    } finally {
      setIsRefreshing(false);
    }
  };

  const handleResetConfirm = async () => {
    setResetOpen(false);
    setIsResetting(true);
    try {
      await onReset(name);
    } finally {
      setIsResetting(false);
    }
  };

  return (
    // A drift review earns the wide layout: the per-tool prose|schema split
    // needs line length for both columns. The clean inventory view keeps the
    // narrow cap for readability.
    <div className={cn('px-6 py-4 space-y-4', hasDrift ? 'max-w-7xl' : 'max-w-3xl')}>
      <div className="flex items-center gap-3">
        <h2 className="text-sm font-mono text-text-primary">{name}</h2>
        <span className={cn('flex items-center gap-1.5 text-xs', colorClass)}>
          {hasDrift ? <LockOpen size={11} /> : <Lock size={11} />}
          {label}
        </span>
        <span
          className="flex items-center gap-1 text-[11px] text-text-muted ml-auto"
          title={sp.last_verified_at || undefined}
        >
          <Clock size={10} className="text-text-muted/60" />
          {sp.last_verified_at
            ? `verified ${formatRelativeTime(new Date(sp.last_verified_at))}`
            : 'never verified'}
        </span>
        {!hasDrift && (
          <button
            id={REVERIFY_BUTTON_ID}
            onClick={handleRefresh}
            disabled={isRefreshing}
            title="Refresh pin state from the gateway"
            aria-label="Re-verify"
            className="p-1 rounded text-text-muted hover:text-primary hover:bg-surface-highlight transition-colors disabled:opacity-50"
          >
            <RefreshCw size={11} className={cn(isRefreshing && 'animate-spin')} />
          </button>
        )}
      </div>

      <div className="flex items-center gap-4 text-[11px] text-text-muted">
        <span>
          <span className="text-text-secondary font-medium">{sp.tool_count}</span> tools pinned
        </span>
        {sp.pinned_at && (
          <span title={sp.pinned_at}>first pinned {formatRelativeTime(new Date(sp.pinned_at))}</span>
        )}
      </div>

      {hasDrift && <DriftSection serverName={name} />}

      <section id={TOOLS_SECTION_ID} className="space-y-2 max-w-5xl">
        <div className="flex items-center gap-2">
          <h3 className="text-[10px] uppercase tracking-[0.18em] text-text-muted/70">
            Pinned tool records
          </h3>
          <button
            onClick={onToggleFindingsOnly}
            aria-pressed={findingsOnly}
            title={
              findingsOnly ? 'Show all pinned tools' : 'Show only tools with scan findings'
            }
            className={cn(
              'h-6 px-2 text-[10px] font-medium rounded border transition-colors flex items-center gap-1 flex-shrink-0',
              findingsOnly
                ? 'bg-status-pending/15 text-status-pending border-status-pending/30'
                : 'bg-background/60 text-text-muted border-border/40 hover:text-text-secondary hover:border-border/60',
            )}
          >
            <ShieldAlert size={9} />
            Findings only
          </button>
          {findingsOnly && (
            <span className="text-[10px] text-text-muted">
              {flaggedRecords.length} of {toolRecords.length} tools
            </span>
          )}
        </div>
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
              {visibleRecords.map((rec) => {
                const findingsShown = expandedFindings.has(rec.name);
                const hasFindings = (rec.findings?.length ?? 0) > 0;
                return (
                  <tr key={rec.name} className="border-b border-border/20 last:border-b-0">
                    <td className="px-3 py-2 align-top">
                      <div className="flex items-center gap-2">
                        <span className="font-mono text-text-primary">
                          {escapeNonPrintable(rec.name)}
                        </span>
                        {hasFindings && (
                          <button
                            onClick={() => toggleRowFindings(rec.name)}
                            aria-expanded={findingsShown}
                            title={findingsShown ? 'Hide findings' : 'Show findings'}
                            className="flex-shrink-0"
                          >
                            <FindingsSummaryBadge findings={rec.findings} />
                          </button>
                        )}
                      </div>
                      {rec.description && (
                        <div className="text-[10px] text-text-muted mt-0.5 whitespace-pre-wrap break-words">
                          {escapeNonPrintable(rec.description)}
                        </div>
                      )}
                      {hasFindings && findingsShown && (
                        <div className="mt-1.5">
                          <FindingsList findings={rec.findings} />
                        </div>
                      )}
                    </td>
                    <td
                      className="px-3 py-2 align-top font-mono text-text-muted whitespace-nowrap"
                      title={rec.hash}
                    >
                      {shortPinHash(rec.hash)}
                    </td>
                    <td
                      className="px-3 py-2 align-top text-text-muted whitespace-nowrap"
                      title={rec.pinned_at || undefined}
                    >
                      {rec.pinned_at ? formatRelativeTime(new Date(rec.pinned_at)) : '—'}
                    </td>
                  </tr>
                );
              })}
              {visibleRecords.length === 0 && (
                <tr>
                  <td colSpan={3} className="px-3 py-4 text-center text-text-muted italic">
                    {findingsOnly ? 'No tools with scan findings.' : 'No pinned tools.'}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </section>

      {/* Destructive affordance, deliberately far from Approve: resetting
          discards the trust record, and the next verify blindly re-pins. */}
      <section className="pt-4 mt-2 border-t border-border/30 space-y-2 max-w-3xl">
        <h3 className="text-[10px] uppercase tracking-[0.18em] text-status-error/70">Danger</h3>
        <div className="flex items-center gap-3">
          <button
            id={RESET_BUTTON_ID}
            // Guarded rather than disabled while the DELETE is in flight:
            // the confirm dialog restores focus here on close, and focusing
            // a disabled button is a no-op that drops keyboard users at the
            // top of the document.
            onClick={() => {
              if (!isResetting) setResetOpen(true);
            }}
            aria-busy={isResetting}
            className={cn(
              'flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-medium text-status-error bg-status-error/10 border border-status-error/20 hover:bg-status-error/20 transition-colors',
              isResetting && 'opacity-50',
            )}
          >
            <Trash2 size={11} />
            {isResetting ? 'Resetting…' : `Reset pins for ${name}`}
          </button>
          <p className="text-[11px] text-text-muted">
            Deletes the pin record; the server re-pins on the next verify.
          </p>
        </div>
      </section>

      {/* display:contents so the parent's space-y margin cannot offset the
          fixed-position dialog backdrop. */}
      <div className="contents">
        <ResetPinsDialog
          isOpen={resetOpen}
          onClose={() => setResetOpen(false)}
          onConfirm={() => void handleResetConfirm()}
          serverName={name}
          toolCount={sp.tool_count}
        />
      </div>
    </div>
  );
}
