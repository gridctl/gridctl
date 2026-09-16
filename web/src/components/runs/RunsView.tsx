import { useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router';
import { useRunsStore } from '../../stores/useRunsStore';
import { exportRuns, updateStackRuns, wipeRuns, type RunRecord } from '../../lib/api';
import { cn } from '../../lib/cn';

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
  const setFilters = useRunsStore((s) => s.setFilters);
  const select = useRunsStore((s) => s.select);
  const load = useRunsStore((s) => s.load);
  const setMutationError = useRunsStore((s) => s.setMutationError);
  const isLoading = useRunsStore((s) => s.isLoading);
  const [confirmEnable, setConfirmEnable] = useState(false);
  const [confirmDisable, setConfirmDisable] = useState(false);
  const [busy, setBusy] = useState(false);
  const [params] = useSearchParams();
  const [narrow, setNarrow] = useState(false);
  const [ageDraft, setAgeDraft] = useState<string | null>(null);
  const [sizeDraft, setSizeDraft] = useState<string | null>(null);

  useEffect(() => {
    const server = params.get('server');
    if (server) setFilters({ server });
  }, [params, setFilters]);

  useEffect(() => {
    void load();
  }, [
    load,
    filters.server,
    filters.tool,
    filters.disposition,
    filters.requested,
    filters.client,
    filters.access,
    filters.attempt,
    filters.parent,
    filters.root,
    filters.previous,
    filters.trace,
    filters.since,
    filters.until,
  ]);

  useEffect(() => {
    const mq = window.matchMedia('(max-width: 768px)');
    const apply = () => setNarrow(mq.matches);
    apply();
    mq.addEventListener('change', apply);
    return () => mq.removeEventListener('change', apply);
  }, []);

  const ageDays = ageDraft ?? (status?.retention_max_age_days ? String(status.retention_max_age_days) : '');
  const sizeMb = sizeDraft ?? (status?.retention_max_bytes ? String(Math.round(status.retention_max_bytes / (1024 * 1024))) : '');

  const selected = useMemo(
    () => records.find((r) => r.attemptId === selectedId) ?? null,
    [records, selectedId],
  );
  const children = useMemo(
    () => records.filter((r) => selected && r.parentAttemptId === selected.attemptId),
    [records, selected],
  );

  const emptyReason = emptyState(status, error, records.length, isLoading);
  const dropSummary = dropText(status);

  return (
    <div className="h-full flex flex-col min-h-0">
      <div className="flex-shrink-0 px-6 py-3 border-b border-border-subtle space-y-2">
        <p className="text-xs text-text-muted max-w-3xl">{status?.recordingNote ?? RECORDING_BLURB}</p>
        <div className="flex flex-wrap items-center gap-3 text-xs">
          <span className="text-text-muted">
            Writer: <Health value={status?.writer_health} />
          </span>
          <span className="text-text-muted">
            History: {error ? 'unreadable' : partial ? 'partial' : 'readable'}
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
                const blob = await exportRuns({
                  server: filters.server || undefined,
                  tool: filters.tool || undefined,
                  disposition: filters.disposition || undefined,
                  requested: filters.requested || undefined,
                  client: filters.client || undefined,
                  access: filters.access || undefined,
                  attempt: filters.attempt || undefined,
                  parent: filters.parent || undefined,
                  root: filters.root || undefined,
                  previous: filters.previous || undefined,
                  trace: filters.trace || undefined,
                  since: filters.since || undefined,
                  until: filters.until || undefined,
                });
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
            <>
              <button
                type="button"
                className="px-2 py-1 rounded border border-border text-text-primary hover:bg-surface-highlight"
                onClick={() => setConfirmDisable(true)}
              >
                Disable recording
              </button>
              <button
                type="button"
                className="px-2 py-1 rounded border border-border text-text-primary hover:bg-surface-highlight"
                onClick={async () => {
                  setBusy(true);
                  setMutationError(null);
                  try {
                    const result = await wipeRuns();
                    if (!result.success) {
                      setMutationError(result.partial ? 'Wipe was partial; some files remain.' : 'Wipe failed.');
                    }
                    await load();
                  } catch (err) {
                    setMutationError(err instanceof Error ? err.message : 'Wipe failed');
                  } finally {
                    setBusy(false);
                  }
                }}
                disabled={busy}
              >
                Wipe history
              </button>
            </>
          )}
        </div>
        <div className="flex flex-wrap items-center gap-3 text-xs">
          <label className="flex items-center gap-1">
            <span className="text-text-muted">Server</span>
            <select
              className="bg-surface border border-border rounded px-1 py-0.5"
              value={filters.server}
              onChange={(e) => setFilters({ server: e.target.value })}
              aria-label="Filter by server"
            >
              <option value="">All</option>
              {servers.map((s) => (
                <option key={s} value={s}>
                  {s}
                </option>
              ))}
            </select>
          </label>
          <label className="flex items-center gap-1">
            <span className="text-text-muted">Disposition</span>
            <select
              className="bg-surface border border-border rounded px-1 py-0.5"
              value={filters.disposition}
              onChange={(e) => setFilters({ disposition: e.target.value })}
              aria-label="Filter by disposition"
            >
              <option value="">All</option>
              {['completed', 'denied', 'tool_error', 'routing_failed', 'transport_error', 'cancelled', 'timeout', 'input_required', 'retry_rejected'].map((d) => (
                <option key={d} value={d}>
                  {d}
                </option>
              ))}
            </select>
          </label>
          <FilterInput label="Tool" value={filters.tool} onChange={(v) => setFilters({ tool: v })} />
          <FilterInput label="Requested" value={filters.requested} onChange={(v) => setFilters({ requested: v })} />
          <FilterInput label="Client" value={filters.client} onChange={(v) => setFilters({ client: v })} />
          <FilterInput label="Access" value={filters.access} onChange={(v) => setFilters({ access: v })} />
          <FilterInput label="Attempt" value={filters.attempt} onChange={(v) => setFilters({ attempt: v })} />
          <FilterInput label="Parent" value={filters.parent} onChange={(v) => setFilters({ parent: v })} />
          <FilterInput label="Root" value={filters.root} onChange={(v) => setFilters({ root: v })} />
          <FilterInput label="Previous" value={filters.previous} onChange={(v) => setFilters({ previous: v })} />
          <FilterInput label="Trace" value={filters.trace} onChange={(v) => setFilters({ trace: v })} />
          <FilterInput label="Since" value={filters.since} onChange={(v) => setFilters({ since: v })} placeholder="RFC3339" />
          <FilterInput label="Until" value={filters.until} onChange={(v) => setFilters({ until: v })} placeholder="RFC3339" />
        </div>
        {status?.enabled && (
          <div className="flex flex-wrap items-center gap-3 text-xs">
            <label className="flex items-center gap-1">
              <span className="text-text-muted">Max age (days)</span>
              <input
                className="bg-surface border border-border rounded px-1 py-0.5 w-16"
                value={ageDays}
                onChange={(e) => setAgeDraft(e.target.value)}
                aria-label="Retention max age days"
              />
            </label>
            <label className="flex items-center gap-1">
              <span className="text-text-muted">Max size (MiB)</span>
              <input
                className="bg-surface border border-border rounded px-1 py-0.5 w-16"
                value={sizeMb}
                onChange={(e) => setSizeDraft(e.target.value)}
                aria-label="Retention max size mebibytes"
              />
            </label>
            <button
              type="button"
              className="px-2 py-1 rounded border border-border"
              disabled={busy}
              onClick={async () => {
                setBusy(true);
                setMutationError(null);
                try {
                  await updateStackRuns({
                    retention: {
                      max_age_days: Number(ageDays) || undefined,
                      max_size_mb: Number(sizeMb) || undefined,
                    },
                  });
                  await load();
                } catch (err) {
                  setMutationError(err instanceof Error ? err.message : 'Failed to update retention');
                } finally {
                  setBusy(false);
                }
              }}
            >
              Save retention
            </button>
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
          </div>
        )}
        {warnings.map((w) => (
          <p key={w.code} className="text-xs text-status-pending" role="status">
            {w.message} ({w.count})
          </p>
        ))}
        {mutationError && (
          <p className="text-xs text-status-error" role="alert">
            {mutationError}
          </p>
        )}
      </div>

      {confirmEnable && (
        <div role="dialog" aria-labelledby="enable-runs-title" className="px-6 py-3 border-b border-border-subtle bg-surface-elevated">
          <h2 id="enable-runs-title" className="text-sm font-medium">Enable run recording?</h2>
          <p className="text-xs text-text-muted mt-1 max-w-2xl">{status?.privacyNote}</p>
          <p className="text-xs text-text-muted mt-1 max-w-2xl">{status?.retentionNote}</p>
          <div className="mt-2 flex gap-2">
            <button
              type="button"
              className="px-2 py-1 rounded bg-primary/20 text-primary"
              onClick={async () => {
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
              }}
              disabled={busy}
            >
              Enable
            </button>
            <button type="button" className="px-2 py-1 rounded border border-border" onClick={() => setConfirmEnable(false)}>
              Cancel
            </button>
          </div>
        </div>
      )}

      {confirmDisable && (
        <div role="dialog" aria-labelledby="disable-runs-title" className="px-6 py-3 border-b border-border-subtle bg-surface-elevated">
          <h2 id="disable-runs-title" className="text-sm font-medium">Disable run recording?</h2>
          <p className="text-xs text-text-muted mt-1 max-w-2xl">Retained history is kept. New attempts will not be recorded.</p>
          <div className="mt-2 flex gap-2">
            <button
              type="button"
              className="px-2 py-1 rounded bg-primary/20 text-primary"
              onClick={async () => {
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
              }}
              disabled={busy}
            >
              Disable
            </button>
            <button type="button" className="px-2 py-1 rounded border border-border" onClick={() => setConfirmDisable(false)}>
              Cancel
            </button>
          </div>
        </div>
      )}

      <div className={cn('flex-1 min-h-0 flex', narrow && 'flex-col')}>
        <div className="flex-1 min-w-0 overflow-auto">
          {emptyReason ? (
            <p className="px-6 py-8 text-sm text-text-muted" role="status">
              {emptyReason}
            </p>
          ) : (
            <table className="w-full text-sm" role="grid" aria-label="Run records">
              <thead className="text-left text-[10px] uppercase tracking-wider text-text-muted">
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
                    onSelect={() => select(rec.attemptId)}
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

function FilterInput({
  label,
  value,
  onChange,
  placeholder,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
}) {
  return (
    <label className="flex items-center gap-1">
      <span className="text-text-muted">{label}</span>
      <input
        className="bg-surface border border-border rounded px-1 py-0.5 w-28"
        value={value}
        placeholder={placeholder}
        onChange={(e) => onChange(e.target.value)}
        aria-label={`Filter by ${label.toLowerCase()}`}
      />
    </label>
  );
}

function RunRow({
  record,
  selected,
  onSelect,
}: {
  record: RunRecord;
  selected: boolean;
  onSelect: () => void;
}) {
  const target = record.resolvedServer
    ? `${record.resolvedServer} › ${record.resolvedTool ?? ''}`
    : record.requestedName || 'unresolved';
  return (
    <tr
      tabIndex={0}
      aria-selected={selected}
      className={cn('cursor-pointer border-t border-border-subtle', selected && 'bg-primary/10')}
      onClick={onSelect}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          onSelect();
        }
      }}
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

function emptyState(
  status: { enabled?: boolean; logical_bytes?: number; inventory?: { fileCount: number; sizeBytes: number } } | null,
  error: string | null,
  count: number,
  loading: boolean,
): string | null {
  if (loading) return null;
  if (error) return 'Run history is unreadable. This is not an empty history.';
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
