import { useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router';
import { useRunsStore } from '../../stores/useRunsStore';
import { updateStackRuns, wipeRuns, type RunRecord } from '../../lib/api';
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
  const status = useRunsStore((s) => s.status);
  const error = useRunsStore((s) => s.error);
  const selectedId = useRunsStore((s) => s.selectedId);
  const filters = useRunsStore((s) => s.filters);
  const setFilters = useRunsStore((s) => s.setFilters);
  const select = useRunsStore((s) => s.select);
  const load = useRunsStore((s) => s.load);
  const isLoading = useRunsStore((s) => s.isLoading);
  const [confirmEnable, setConfirmEnable] = useState(false);
  const [busy, setBusy] = useState(false);
  const [params] = useSearchParams();

  useEffect(() => {
    const server = params.get('server');
    if (server) setFilters({ server });
  }, [params, setFilters]);

  useEffect(() => {
    void load();
  }, [load, filters.server, filters.disposition]);

  const selected = useMemo(
    () => records.find((r) => r.attemptId === selectedId) ?? null,
    [records, selectedId],
  );
  const children = useMemo(
    () => records.filter((r) => selected && r.parentAttemptId === selected.attemptId),
    [records, selected],
  );

  const emptyReason = emptyState(status, error, records.length, isLoading);

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
              onClick={async () => {
                setBusy(true);
                try {
                  await wipeRuns();
                  await load();
                } finally {
                  setBusy(false);
                }
              }}
              disabled={busy}
            >
              Wipe history
            </button>
          )}
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
        </div>
        {warnings.map((w) => (
          <p key={w.code} className="text-xs text-status-pending" role="status">
            {w.message} ({w.count})
          </p>
        ))}
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
                try {
                  await updateStackRuns({ enabled: true });
                  setConfirmEnable(false);
                  await load();
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

      <div className="flex-1 min-h-0 flex">
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
        </div>
        {selected && (
          <aside className="w-80 flex-shrink-0 border-l border-border p-4 overflow-auto" aria-label="Run detail">
            <h2 className="text-xs uppercase tracking-wider text-text-muted">Detail</h2>
            <dl className="mt-2 space-y-1 text-xs font-mono">
              <Detail label="Attempt" value={selected.attemptId} />
              <Detail label="Parent" value={selected.parentAttemptId || 'unavailable'} />
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

function emptyState(
  status: { enabled?: boolean; inventory?: { fileCount: number } } | null,
  error: string | null,
  count: number,
  loading: boolean,
): string | null {
  if (loading) return null;
  if (error) return 'Run history is unreadable. This is not an empty history.';
  if (count > 0) return null;
  if (status && status.enabled === false && (status.inventory?.fileCount ?? 0) > 0) {
    return 'Recording is disabled. Retained records may still exist; none match the current filters.';
  }
  if (status && status.enabled && (status.inventory?.fileCount ?? 0) === 0) {
    return 'Recording is enabled. No records have been retained yet.';
  }
  if (status && !status.enabled) {
    return 'Recording is disabled. Metrics remain aggregate usage; traces remain timing detail.';
  }
  return 'No matching run records.';
}
