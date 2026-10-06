import { formatRelativeTime } from '../../lib/time';
import type { MCPServerNodeData, ReplicaStatus } from '../../types';

function formatRetryIn(iso: string | undefined): string {
  if (!iso) return '—';
  const at = Date.parse(iso);
  if (Number.isNaN(at)) return '—';
  const seconds = Math.round((at - Date.now()) / 1000);
  if (seconds <= 0) return 'due';
  if (seconds < 60) return `in ${seconds}s`;
  const minutes = Math.round(seconds / 60);
  return `in ${minutes}m`;
}

function formatFinishedAt(iso: string | undefined): string {
  if (!iso) return '—';
  const at = new Date(iso);
  if (Number.isNaN(at.getTime())) return '—';
  return formatRelativeTime(at);
}

function ReplicaRow({ replica }: { replica: ReplicaStatus }) {
  return (
    <div className="space-y-1 rounded-md bg-background/40 px-2 py-1.5">
      <div className="flex justify-between items-center gap-4">
        <span className="text-sm text-text-muted">Replica {replica.replicaId}</span>
        <span className="text-xs text-text-secondary font-mono">{replica.state}</span>
      </div>
      <div className="flex justify-between items-center gap-4">
        <span className="text-sm text-text-muted">Attempts</span>
        <span className="text-xs text-text-secondary font-mono">{replica.restartAttempts ?? 0}</span>
      </div>
      <div className="flex justify-between items-center gap-4">
        <span className="text-sm text-text-muted">Next retry</span>
        <span className="text-xs text-text-secondary font-mono">{formatRetryIn(replica.nextRetryAt)}</span>
      </div>
      {replica.exit && (
        <>
          <div className="flex justify-between items-center gap-4">
            <span className="text-sm text-text-muted">Exit code</span>
            <span className="text-xs text-text-secondary font-mono">{replica.exit.code}</span>
          </div>
          <div className="flex justify-between items-center gap-4">
            <span className="text-sm text-text-muted">OOM</span>
            <span className="text-xs text-text-secondary font-mono">{replica.exit.oomKilled ? 'yes' : 'no'}</span>
          </div>
          <div className="flex justify-between items-center gap-4">
            <span className="text-sm text-text-muted">Finished</span>
            <span className="text-xs text-text-secondary font-mono">{formatFinishedAt(replica.exit.finishedAt)}</span>
          </div>
          <div className="flex justify-between items-center gap-4">
            <span className="text-sm text-text-muted">Runtime</span>
            <span className="text-xs text-text-secondary font-mono">{replica.exit.status || '—'}</span>
          </div>
        </>
      )}
    </div>
  );
}

export function ServerCrashDetails({ data }: { data?: MCPServerNodeData }) {
  const replicas = data?.replicas ?? [];
  const stderr = data?.stderrTail ?? [];
  if (replicas.length === 0 && stderr.length === 0) return null;
  return (
    <div className="space-y-2">
      {replicas.map((replica) => (
        <ReplicaRow key={replica.replicaId} replica={replica} />
      ))}
      {stderr.length > 0 && (
        <div className="space-y-1">
          <span className="text-sm text-text-muted">Recent stderr</span>
          <ul className="space-y-0.5 rounded-md bg-background/50 p-2">
            {stderr.map((line, index) => (
              <li key={`${index}-${line}`} className="font-mono text-xs text-text-secondary break-all">{line}</li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}
