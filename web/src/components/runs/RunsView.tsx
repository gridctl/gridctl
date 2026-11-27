import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { useSearchParams } from 'react-router';
import { useRunsStore } from '../../stores/useRunsStore';
import { exportRuns, updateStackRuns, wipeRuns, type RunRecord } from '../../lib/api';
import { formatBytes } from '../../lib/format-bytes';
import { cn } from '../../lib/cn';
import { ConfirmDialog } from '../ui/ConfirmDialog';
import { FailedState, FailureBanner } from '../ui/EmptyState';
import { refreshPhase } from '../../lib/refreshPhase';
import { parseRunsSearch, resolveRunsQuery, writeRunsSearch } from '../../lib/runsFilters';
import { RunsFilterBar } from './RunsFilterBar';
import { listNavProps, useListNav } from '../../hooks/useListNav';
import { showToast } from '../ui/Toast';

const MAX_RETENTION_DAYS = 3650;
const MAX_RETENTION_MIB = 1048576;
const DEFAULT_RETENTION_DAYS = 7;
const DEFAULT_RETENTION_MIB = 100;

const RECORDING_BLURB =
  'Records are saved after dispatch attempts return. Recording is best-effort. Attempts interrupted by a crash may leave no record, and older records may have been removed by retention or wipe.';

interface RunsViewProps {
  servers: string[];
}

export function RunsView({ servers }: RunsViewProps) {
  const records = useRunsStore((s) => s.records);
  const warnings = useRunsStore((s) => s.warnings);
  const partial = useRunsStore((s) => s.partial);
  const nextCursor = useRunsStore((s) => s.nextCursor);
  const status = useRunsStore((s) => s.status);
  const error = useRunsStore((s) => s.error);
  const statusError = useRunsStore((s) => s.statusError);
  const mutationError = useRunsStore((s) => s.mutationError);
  const selectedId = useRunsStore((s) => s.selectedId);
  const filters = useRunsStore((s) => s.filters);
  const replaceFilters = useRunsStore((s) => s.replaceFilters);
  const select = useRunsStore((s) => s.select);
  const load = useRunsStore((s) => s.load);
  const retry = useRunsStore((s) => s.retry);
  const setMutationError = useRunsStore((s) => s.setMutationError);
  const isLoading = useRunsStore((s) => s.isLoading);
  const hasLoaded = useRunsStore((s) => s.hasLoaded);
  const [confirmEnable, setConfirmEnable] = useState(false);
  const [confirmDisable, setConfirmDisable] = useState(false);
  const [confirmWipe, setConfirmWipe] = useState(false);
  const [busy, setBusy] = useState(false);
  const [params, setSearchParams] = useSearchParams();
  const initialParams = useRef(params);
  const [urlApplied, setUrlApplied] = useState(false);
  const [narrow, setNarrow] = useState(false);
  const [ageDraft, setAgeDraft] = useState<string | null>(null);
  const [sizeDraft, setSizeDraft] = useState<string | null>(null);
  const [ageError, setAgeError] = useState<string | null>(null);
  const [sizeError, setSizeError] = useState<string | null>(null);
  const [navId, setNavId] = useState<string | null>(null);

  // URL to store, once on mount and before the first load: a deep link with
  // any filter key replaces the filters wholesale. The load and mirror
  // effects wait for urlApplied so mounting issues exactly one request.
  useLayoutEffect(() => {
    const { filters: urlFilters, run } = parseRunsSearch(initialParams.current);
    if (urlFilters) replaceFilters(urlFilters);
    if (run) select(run);
    // eslint-disable-next-line react-hooks/set-state-in-effect -- gates the first load until the URL is applied; the layout effect re-renders before paint
    setUrlApplied(true);
  }, [replaceFilters, select]);

  // Store to URL. Reads live store state so the first pass after the mount
  // sync never writes stale render-time values.
  useEffect(() => {
    if (!urlApplied) return;
    const s = useRunsStore.getState();
    setSearchParams((prev) => writeRunsSearch(prev, s.filters, s.selectedId), { replace: true });
  }, [urlApplied, filters, selectedId, setSearchParams]);

  useEffect(() => {
    if (!urlApplied) return;
    void load();
  }, [load, filters, urlApplied]);

  useEffect(() => {
    const mq = window.matchMedia('(max-width: 768px)');
    const apply = () => setNarrow(mq.matches);
    apply();
    mq.addEventListener('change', apply);
    return () => mq.removeEventListener('change', apply);
  }, []);

  // Zero values mean the server could not read the configured retention.
  const ageDays = ageDraft ?? String(status?.retention_max_age_days || DEFAULT_RETENTION_DAYS);
  const sizeMb =
    sizeDraft ??
    String(status?.retention_max_bytes ? Math.round(status.retention_max_bytes / (1024 * 1024)) : DEFAULT_RETENTION_MIB);
  const historyExists =
    (status?.inventory.sizeBytes ?? 0) > 0 || (status?.inventory.fileCount ?? 0) > 0 || records.length > 0;
  const showRetention = Boolean(status) && (Boolean(status?.enabled) || historyExists);

  const saveRetention = async () => {
    const age = parseWholeNumber(ageDays, 1, MAX_RETENTION_DAYS);
    const size = parseWholeNumber(sizeMb, 1, MAX_RETENTION_MIB);
    setAgeError(age === null ? `Max age must be a whole number of days from 1 to ${MAX_RETENTION_DAYS}.` : null);
    setSizeError(size === null ? `Max size must be a whole number of MiB from 1 to ${MAX_RETENTION_MIB}.` : null);
    if (age === null || size === null) return;
    setBusy(true);
    setMutationError(null);
    try {
      await updateStackRuns({ retention: { max_age_days: age, max_size_mb: size } });
      setAgeDraft(null);
      setSizeDraft(null);
      showToast('success', 'Retention saved');
      await load();
    } catch (err) {
      setMutationError(err instanceof Error ? err.message : 'Failed to update retention');
    } finally {
      setBusy(false);
    }
  };

  // Keyboard navigation copies the Traces list: the highlight derives from
  // navId, so a navId no longer in the list is inert.
  const navIndex = useMemo(
    () => (navId ? records.findIndex((r) => r.attemptId === navId) : -1),
    [records, navId],
  );
  const activeNavId = navIndex >= 0 ? navId : null;
  useListNav({
    itemCount: records.length,
    selectedIndex: navIndex < 0 ? 0 : navIndex,
    setSelectedIndex: (i) => {
      const rec = records[i];
      if (!rec) return;
      setNavId(rec.attemptId);
      document.getElementById(`run-row-${rec.attemptId}`)?.scrollIntoView({ block: 'nearest' });
    },
    onEnter: () => {
      if (activeNavId) select(activeNavId);
    },
    onEscape: selectedId ? () => select(null) : undefined,
  });

  const selected = useMemo(
    () => records.find((r) => r.attemptId === selectedId) ?? null,
    [records, selectedId],
  );
  const children = useMemo(
    () => records.filter((r) => selected && r.parentAttemptId === selected.attemptId),
    [records, selected],
  );

  const phase = refreshPhase({ error, hasLoaded });
  const firstLoadPending = phase === 'loading' && isLoading;
  const emptyReason = emptyState(status, records.length, firstLoadPending);
  const dropSummary = dropText(status);
  const missingSelection =
    selectedId != null && hasLoaded && !isLoading && !records.some((r) => r.attemptId === selectedId);

  return (
    <div className="h-full flex flex-col min-h-0">
      <div className="flex-shrink-0 px-6 py-3 border-b border-border-subtle space-y-2">
        <p className="text-xs text-text-muted max-w-3xl">{status?.recordingNote ?? RECORDING_BLURB}</p>
        <div className="flex flex-wrap items-center gap-3 text-xs">
          <span className="text-text-muted">
            Writer: <Health value={status?.writer_health} />
          </span>
          <span className="text-text-muted">
            History:{' '}
            {phase === 'failed'
              ? 'unreadable'
              : phase === 'stale'
                ? 'refresh failed'
                : partial
                  ? 'partial'
                  : 'readable'}
          </span>
          {statusError && (
            <span className="text-status-pending" role="status">
              Status unavailable
            </span>
          )}
          {dropSummary && (
            <span className="text-status-pending" role="status">
              {dropSummary}
            </span>
          )}
          <button
            type="button"
            className="px-2 py-1 rounded border border-border text-text-primary hover:bg-surface-highlight"
            onClick={() => void load()}
            aria-label="Refresh run records"
          >
            Refresh
          </button>
          <button
            type="button"
            className="px-2 py-1 rounded border border-border text-text-primary hover:bg-surface-highlight"
            onClick={async () => {
              setBusy(true);
              setMutationError(null);
              try {
                // Export the query behind the rows on screen, not one in flight.
                const blob = await exportRuns(useRunsStore.getState().lastQuery ?? resolveRunsQuery(filters, Date.now()));
                const url = URL.createObjectURL(blob);
                const a = document.createElement('a');
                a.href = url;
                a.download = 'runs.json';
                a.click();
                URL.revokeObjectURL(url);
              } catch (err) {
                setMutationError(err instanceof Error ? err.message : 'Export failed');
              } finally {
                setBusy(false);
              }
            }}
            disabled={busy}
          >
            Export
          </button>
          {status && !status.enabled && (
            <button
              type="button"
              className="px-2 py-1 rounded border border-border text-text-primary hover:bg-surface-highlight"
              onClick={() => setConfirmEnable(true)}
            >
              Enable recording
            </button>
          )}
          {status?.enabled && (
            <button
              type="button"
              className="px-2 py-1 rounded border border-border text-text-primary hover:bg-surface-highlight"
              onClick={() => setConfirmDisable(true)}
            >
              Disable recording
            </button>
          )}
          {status && historyExists && (
            <button
              type="button"
              className={cn(
                'px-2 py-1 rounded border',
                'border-status-error/40 text-status-error hover:bg-status-error/10',
              )}
              onClick={() => setConfirmWipe(true)}
              disabled={busy}
            >
              Wipe history
            </button>
          )}
        </div>
        <RunsFilterBar servers={servers} />
        {status && showRetention && (
          <div className="flex flex-wrap items-center gap-3 text-xs">
            <label className="flex items-center gap-1">
              <span className="text-text-muted">Max age (days)</span>
              <input
                className="bg-surface border border-border rounded px-1 py-0.5 w-16"
                value={ageDays}
                onChange={(e) => {
                  setAgeDraft(e.target.value);
                  setAgeError(null);
                }}
                aria-label="Retention max age days"
                aria-invalid={ageError ? true : undefined}
                aria-describedby={ageError ? 'runs-retention-age-error' : undefined}
              />
            </label>
            <label className="flex items-center gap-1">
              <span className="text-text-muted">Max size (MiB)</span>
              <input
                className="bg-surface border border-border rounded px-1 py-0.5 w-20"
                value={sizeMb}
                onChange={(e) => {
                  setSizeDraft(e.target.value);
                  setSizeError(null);
                }}
                aria-label="Retention max size mebibytes"
                aria-invalid={sizeError ? true : undefined}
                aria-describedby={sizeError ? 'runs-retention-size-error' : undefined}
              />
            </label>
            <button
              type="button"
              className="px-2 py-1 rounded border border-border"
              disabled={busy}
              onClick={() => void saveRetention()}
            >
              Save retention
            </button>
            {status.enabled ? (
              <label className="flex items-center gap-1">
                <input
                  type="checkbox"
                  checked={Boolean(status.omit_labels)}
                  onChange={async (e) => {
                    setBusy(true);
                    setMutationError(null);
                    try {
                      await updateStackRuns({ omit_labels: e.target.checked });
                      await load();
                    } catch (err) {
                      setMutationError(err instanceof Error ? err.message : 'Failed to update labels');
                    } finally {
                      setBusy(false);
                    }
                  }}
                  disabled={busy}
                />
                <span className="text-text-muted">Omit caller labels</span>
              </label>
            ) : (
              <span className="text-text-muted">
                Applies when recording is enabled. Retained records are not pruned while recording is off.
              </span>
            )}
            {ageError && (
              <p id="runs-retention-age-error" className="w-full text-status-error" role="alert">
                {ageError}
              </p>
            )}
            {sizeError && (
              <p id="runs-retention-size-error" className="w-full text-status-error" role="alert">
                {sizeError}
              </p>
            )}
          </div>
        )}
        {warnings.map((w) => (
          <p key={w.code} className="text-xs text-status-pending" role="status">
            {w.message} ({w.count})
          </p>
        ))}
        {/* Enable and disable failures render inside the open confirm. */}
        {mutationError && !confirmEnable && !confirmDisable && (
          <p className="text-xs text-status-error" role="alert">
            {mutationError}
          </p>
        )}
      </div>

      <ConfirmDialog
        isOpen={confirmEnable}
        onClose={() => {
          setMutationError(null);
          setConfirmEnable(false);
        }}
        onConfirm={() => {
          void (async () => {
            setBusy(true);
            setMutationError(null);
            try {
              await updateStackRuns({ enabled: true });
              setConfirmEnable(false);
              await load();
            } catch (err) {
              setMutationError(err instanceof Error ? err.message : 'Failed to enable recording');
            } finally {
              setBusy(false);
            }
          })();
        }}
        title="Enable run recording?"
        message={
          <>
            <p>{status?.privacyNote}</p>
            <p>{status?.retentionNote}</p>
            {mutationError && (
              <p className="text-status-error" role="alert">{mutationError}</p>
            )}
          </>
        }
        confirmLabel="Enable"
        busy={busy}
      />

      <ConfirmDialog
        isOpen={confirmDisable}
        onClose={() => {
          setMutationError(null);
          setConfirmDisable(false);
        }}
        onConfirm={() => {
          void (async () => {
            setBusy(true);
            setMutationError(null);
            try {
              await updateStackRuns({ enabled: false });
              setConfirmDisable(false);
              await load();
            } catch (err) {
              setMutationError(err instanceof Error ? err.message : 'Failed to disable recording');
            } finally {
              setBusy(false);
            }
          })();
        }}
        title="Disable run recording?"
        message={
          <>
            <p>Retained history is kept. New attempts will not be recorded.</p>
            {mutationError && (
              <p className="text-status-error" role="alert">{mutationError}</p>
            )}
          </>
        }
        confirmLabel="Disable"
        busy={busy}
      />

      <ConfirmDialog
        isOpen={confirmWipe}
        onClose={() => setConfirmWipe(false)}
        onConfirm={() => {
          setConfirmWipe(false);
          void (async () => {
            setBusy(true);
            setMutationError(null);
            try {
              const result = await wipeRuns();
              if (!result.success) {
                const base = result.partial ? 'Wipe was partial; some files remain' : 'Wipe failed';
                setMutationError(result.error ? `${base} (${result.error}).` : `${base}.`);
              }
              await load();
            } catch (err) {
              setMutationError(err instanceof Error ? err.message : 'Wipe failed');
            } finally {
              setBusy(false);
            }
          })();
        }}
        title="Wipe run history"
        message={
          <p>
            Wipe run history for stack <span className="font-mono text-primary">{status?.inventory.stack}</span>?
            This removes {status?.inventory.fileCount ?? 0} stored {(status?.inventory.fileCount ?? 0) === 1 ? 'file' : 'files'} ({formatBytes(status?.inventory.sizeBytes ?? 0)}).
            The wipe is stack-wide and ignores the current filters.
          </p>
        }
        confirmLabel="Wipe history"
        variant="danger"
      />

      <div className={cn('flex-1 min-h-0 flex', narrow && 'flex-col')}>
        <div className="flex-1 min-w-0 overflow-auto">
          {error && records.length > 0 && (
            <FailureBanner message={error} onRetry={() => void retry()} className="mt-3" />
          )}
          {missingSelection && (
            <p className="px-6 pt-3 text-xs text-text-muted flex items-center gap-2" role="status">
              The selected run is not in the current results.
              <button type="button" className="underline text-text-primary" onClick={() => select(null)}>
                Clear selection
              </button>
            </p>
          )}
          {firstLoadPending ? (
            <div className="p-3 space-y-2 animate-pulse" data-testid="runs-skeleton">
              {[1, 2, 3].map((i) => (
                <div key={i} className="h-7 rounded bg-surface-elevated/60 border border-border/20" />
              ))}
            </div>
          ) : error && records.length === 0 ? (
            <FailedState feature="Run history" message={error} onRetry={() => void retry()} inline />
          ) : emptyReason ? (
            <p className="px-6 py-8 text-sm text-text-muted" role="status">
              {emptyReason}
            </p>
          ) : (
            <table
              className="group/grid w-full text-sm focus-visible:-outline-offset-2"
              role="grid"
              aria-label="Run records"
              tabIndex={0}
              {...listNavProps}
              aria-activedescendant={activeNavId ? `run-row-${activeNavId}` : undefined}
            >
              <thead className="text-left text-3xs uppercase tracking-wider text-text-muted">
                <tr>
                  <th className="px-6 py-2">Returned</th>
                  <th className="px-2 py-2">Target</th>
                  <th className="px-2 py-2">Disposition</th>
                  <th className="px-2 py-2">Duration</th>
                </tr>
              </thead>
              <tbody>
                {records.map((rec) => (
                  <RunRow
                    key={rec.attemptId}
                    record={rec}
                    selected={rec.attemptId === selectedId}
                    highlighted={rec.attemptId === activeNavId}
                    onSelect={() => {
                      setNavId(rec.attemptId);
                      select(rec.attemptId);
                    }}
                  />
                ))}
              </tbody>
            </table>
          )}
          {nextCursor && (
            <div className="px-6 py-3">
              <button
                type="button"
                className="px-2 py-1 rounded border border-border text-xs"
                onClick={() => void load({ append: true })}
                disabled={isLoading}
              >
                Load older
              </button>
            </div>
          )}
        </div>
        {selected && (
          <aside
            className={cn(
              'border-border p-4 overflow-auto',
              narrow ? 'w-full border-t' : 'w-80 flex-shrink-0 border-l',
            )}
            aria-label="Run detail"
          >
            <h2 className="text-xs uppercase tracking-wider text-text-muted">Detail</h2>
            <dl className="mt-2 space-y-1 text-xs font-mono">
              <Detail label="Attempt" value={selected.attemptId} />
              <Detail label="Parent" value={selected.parentAttemptId || 'unavailable'} />
              <Detail label="Root" value={selected.rootAttemptId || selected.attemptId} />
              <Detail label="Previous" value={selected.previousAttemptId || 'none'} />
              <Detail label="Stage" value={selected.stage} />
              <Detail label="Reason" value={selected.reason} />
              <Detail label="Labels" value={[selected.clientLabel, selected.accessLabel].filter(Boolean).join(' / ') || 'omitted'} />
            </dl>
            {children.length > 0 && (
              <div className="mt-3">
                <h3 className="text-xs text-text-muted">Nested attempts</h3>
                <ul className="text-xs font-mono">
                  {children.map((c) => (
                    <li key={c.attemptId}>
                      <button type="button" className="underline" onClick={() => select(c.attemptId)}>
                        {c.requestedName || c.attemptId} ({c.disposition})
                      </button>
                    </li>
                  ))}
                </ul>
              </div>
            )}
            {selected.parentAttemptId && !records.some((r) => r.attemptId === selected.parentAttemptId) && (
              <p className="mt-2 text-xs text-text-muted">Parent record is unavailable, not still running.</p>
            )}
          </aside>
        )}
      </div>
    </div>
  );
}

function RunRow({
  record,
  selected,
  highlighted,
  onSelect,
}: {
  record: RunRecord;
  selected: boolean;
  highlighted: boolean;
  onSelect: () => void;
}) {
  const target = record.resolvedServer
    ? `${record.resolvedServer} › ${record.resolvedTool ?? ''}`
    : record.requestedName || 'unresolved';
  return (
    <tr
      id={`run-row-${record.attemptId}`}
      aria-selected={selected}
      className={cn(
        'cursor-pointer border-t border-border-subtle',
        selected ? 'bg-primary/10' : highlighted && 'bg-surface-highlight/30',
        highlighted &&
          'group-focus-within/grid:outline-2 group-focus-within/grid:-outline-offset-2 group-focus-within/grid:outline-primary',
      )}
      onClick={onSelect}
    >
      <td className="px-6 py-2 font-mono text-xs">{record.returnedAt}</td>
      <td className="px-2 py-2 font-mono text-xs">{target}</td>
      <td className="px-2 py-2 text-xs">{record.disposition}</td>
      <td className="px-2 py-2 font-mono text-xs">{record.durationMs}ms</td>
    </tr>
  );
}

function Detail({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="text-text-muted">{label}</dt>
      <dd className="text-text-primary break-all">{value}</dd>
    </div>
  );
}

function Health({ value }: { value?: string }) {
  const label = value || 'unknown';
  return <span className="text-text-primary">{label}</span>;
}

function dropText(status: { drops?: Record<string, number>; failures?: Record<string, number>; writer_health?: string } | null): string | null {
  if (!status) return null;
  const parts: string[] = [];
  for (const [key, n] of Object.entries(status.drops ?? {})) {
    if (n > 0) parts.push(`${key}: ${n}`);
  }
  for (const [key, n] of Object.entries(status.failures ?? {})) {
    if (n > 0 && !(status.drops && key in status.drops)) parts.push(`${key}: ${n}`);
  }
  if (parts.length === 0) return null;
  return `Loss ${parts.join(', ')}`;
}

/** A whole number within [min, max], or null for anything else. */
function parseWholeNumber(raw: string, min: number, max: number): number | null {
  const text = raw.trim();
  if (!/^\d+$/.test(text)) return null;
  const n = Number(text);
  return n >= min && n <= max ? n : null;
}

function emptyState(
  status: { enabled?: boolean; logical_bytes?: number; inventory?: { fileCount: number; sizeBytes: number } } | null,
  count: number,
  loading: boolean,
): string | null {
  if (loading) return null;
  if (count > 0) return null;
  const retained = (status?.logical_bytes ?? status?.inventory?.sizeBytes ?? 0) > 0;
  if (status && status.enabled === false && retained) {
    return 'Recording is disabled. Retained records may still exist; none match the current filters.';
  }
  if (status && status.enabled && !retained) {
    return 'Recording is enabled. No records have been retained yet.';
  }
  if (status && !status.enabled) {
    return 'Recording is disabled. Metrics remain aggregate usage; traces remain timing detail.';
  }
  return 'No matching run records.';
}
