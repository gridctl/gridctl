import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { History } from 'lucide-react';
import { useSearchParams } from 'react-router';
import { emptyFilters, useRunsStore } from '../../stores/useRunsStore';
import { exportRuns, updateStackRuns, wipeRuns, type RunRecord, type RunStatusResponse } from '../../lib/api';
import { formatBytes } from '../../lib/format-bytes';
import { cn } from '../../lib/cn';
import { ConfirmDialog } from '../ui/ConfirmDialog';
import { Button } from '../ui/Button';
import { EmptyState, FailedState, FailureBanner } from '../ui/EmptyState';
import { refreshPhase } from '../../lib/refreshPhase';
import { parseRunsSearch, resolveRunsQuery, writeRunsSearch } from '../../lib/runsFilters';
import { RunsFilterBar } from './RunsFilterBar';
import { listNavProps, useListNav } from '../../hooks/useListNav';
import { showToast } from '../ui/Toast';
import { formatTotalDuration } from '../../lib/duration';
import { formatLocalDateTime } from '../../lib/time';
import { DispositionBadge } from './DispositionBadge';
import { RunDetail, runTarget } from './RunDetail';
import { RunsHeader } from './RunsHeader';

const MAX_RETENTION_DAYS = 3650;
const MAX_RETENTION_MIB = 1048576;
const DEFAULT_RETENTION_DAYS = 7;
const DEFAULT_RETENTION_MIB = 100;

const RECORDING_BLURB =
  'Records are saved after dispatch attempts return. Recording is best-effort. Attempts interrupted by a crash may leave no record, and older records may have been removed by retention or wipe.';

interface RunsViewProps {
  servers: string[];
  /** When set, a run with a trace ID gets a "View trace" link. */
  onViewTrace?: (traceId: string) => void;
  /** When set, a run with a trace ID gets a "View logs" link. */
  onViewLogs?: (traceId: string) => void;
}

export function RunsView({ servers, onViewTrace, onViewLogs }: RunsViewProps) {
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
  const clearFilters = useRunsStore((s) => s.clearFilters);
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
  const gridRef = useRef<HTMLTableElement>(null);

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
    onEscape: selectedId
      ? () => {
          // Closing unmounts the detail; keep focus in the list if it was inside.
          const inDetail = document.activeElement?.closest('[data-run-detail]') != null;
          select(null);
          if (inDetail) gridRef.current?.focus();
        }
      : undefined,
  });

  const selected = useMemo(
    () => records.find((r) => r.attemptId === selectedId) ?? null,
    [records, selectedId],
  );

  const retentionSettings =
    status && showRetention ? (
      <div className="flex flex-col items-start gap-2 text-xs">
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
    ) : null;

  const phase = refreshPhase({ error, hasLoaded });
  const firstLoadPending = phase === 'loading' && isLoading;
  const filtersActive = useMemo(() => {
    const defaults = emptyFilters();
    return (Object.keys(defaults) as (keyof typeof defaults)[]).some((k) => filters[k] !== defaults[k]);
  }, [filters]);
  const missingSelection =
    selectedId != null && hasLoaded && !isLoading && !records.some((r) => r.attemptId === selectedId);

  return (
    <div className="h-full flex flex-col min-h-0">
      <div className="flex-shrink-0 px-6 py-3 border-b border-border-subtle space-y-2">
        <RunsHeader
          status={status}
          statusError={statusError}
          partial={partial}
          recordingBlurb={RECORDING_BLURB}
          busy={busy}
          onRefresh={() => void load()}
          onExport={() => {
            void (async () => {
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
            })();
          }}
          onEnable={() => setConfirmEnable(true)}
          onDisable={() => setConfirmDisable(true)}
          settings={retentionSettings}
          showWipe={Boolean(status) && historyExists}
          onWipe={() => setConfirmWipe(true)}
        />
        <RunsFilterBar servers={servers} />
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
          ) : records.length === 0 ? (
            <RunsEmptyState
              status={status}
              filtersActive={filtersActive}
              onEnable={() => setConfirmEnable(true)}
              onClearFilters={clearFilters}
              onRefresh={() => void load()}
            />
          ) : (
            <table
              ref={gridRef}
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
                  <th className="px-2 py-2 text-right">Duration</th>
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
          <RunDetail
            record={selected}
            records={records}
            narrow={narrow}
            onSelect={(id) => {
              setNavId(id);
              select(id);
            }}
            onClose={() => {
              select(null);
              // Keep focus in the list instead of dropping it to <body>.
              gridRef.current?.focus();
            }}
            onViewTrace={onViewTrace}
            onViewLogs={onViewLogs}
          />
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
  const target = runTarget(record);
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
      <td className="px-6 py-2 font-mono text-xs whitespace-nowrap" title={record.returnedAt}>
        {formatLocalDateTime(record.returnedAt)}
      </td>
      <td
        className="px-2 py-2 font-mono text-xs"
        title={record.requestedName && record.requestedName !== target ? record.requestedName : undefined}
      >
        {target}
      </td>
      <td className="px-2 py-2 text-xs">
        <DispositionBadge value={record.disposition} />
      </td>
      <td className="px-2 py-2 font-mono text-xs text-right tabular-nums">{formatTotalDuration(record.durationMs)}</td>
    </tr>
  );
}

/** A whole number within [min, max], or null for anything else. */
function parseWholeNumber(raw: string, min: number, max: number): number | null {
  const text = raw.trim();
  if (!/^\d+$/.test(text)) return null;
  const n = Number(text);
  return n >= min && n <= max ? n : null;
}

function RunsEmptyState({
  status,
  filtersActive,
  onEnable,
  onClearFilters,
  onRefresh,
}: {
  status: RunStatusResponse | null;
  filtersActive: boolean;
  onEnable: () => void;
  onClearFilters: () => void;
  onRefresh: () => void;
}) {
  const retained = status
    ? (status.logical_bytes ?? status.inventory?.sizeBytes ?? 0) > 0 || (status.inventory?.fileCount ?? 0) > 0
    : false;
  const enable = (
    <Button variant="primary" size="sm" onClick={onEnable}>
      Enable recording
    </Button>
  );
  if (filtersActive) {
    return (
      <EmptyState
        inline
        role="status"
        icon={History}
        title="No run records match your filters"
        action={
          <button type="button" onClick={onClearFilters} className="text-3xs text-primary hover:underline">
            Clear filters
          </button>
        }
      />
    );
  }
  if (status && !status.enabled && !retained) {
    return (
      <EmptyState
        inline
        role="status"
        icon={History}
        title="Run recording is off"
        description="A run record is one saved line for each tool call that returns through the gateway: what was called, when, how long it took, and how it ended. Arguments and results are never stored. Metrics are aggregate counts and traces are sampled timing held in memory; run records stay on disk so you can look up a specific call later."
        action={
          <div className="flex flex-col items-center gap-2">
            {enable}
            <span className="text-xs text-text-muted">
              Or set <code className="font-mono">runs.enabled: true</code> in the stack file.
            </span>
          </div>
        }
      />
    );
  }
  if (status && !status.enabled) {
    return (
      <EmptyState
        inline
        role="status"
        icon={History}
        title="No run records to show"
        description="Recording is off. Earlier records are still on disk."
        action={enable}
      />
    );
  }
  if (status?.enabled && !retained) {
    return (
      <EmptyState
        inline
        role="status"
        icon={History}
        title="No run records yet"
        description="Recording is on. A record appears here after the next tool call returns through the gateway."
        action={
          <Button variant="secondary" size="sm" onClick={onRefresh}>
            Refresh
          </Button>
        }
      />
    );
  }
  return <EmptyState inline role="status" icon={History} title="No run records to show" />;
}
