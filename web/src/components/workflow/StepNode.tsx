import { memo } from 'react';
import { Handle, Position, type NodeProps } from '@xyflow/react';
import { Filter, RefreshCw, SkipForward, Play, Layers, Clock } from 'lucide-react';
import { cn } from '../../lib/cn';

interface StepNodeData {
  stepId: string;
  tool: string;
  status: 'pending' | 'running' | 'success' | 'failed' | 'skipped';
  durationMs?: number;
  error?: string;
  hasCondition: boolean;
  hasRetry: boolean;
  onError?: string;
  isSkillCall: boolean;
  selected?: boolean;
  [key: string]: unknown;
}

const statusBorder: Record<string, string> = {
  pending: 'border-border/40',
  running: 'border-primary/60 shadow-[0_0_12px_rgba(245,158,11,0.2)]',
  success: 'border-status-running/60 shadow-[0_0_8px_rgba(16,185,129,0.15)]',
  failed: 'border-status-error/60 shadow-[0_0_8px_rgba(244,63,94,0.15)]',
  skipped: 'border-border/20 opacity-50',
};

const statusDot: Record<string, string> = {
  pending: 'bg-text-muted',
  running: 'bg-primary animate-pulse',
  success: 'bg-status-running',
  failed: 'bg-status-error',
  skipped: 'bg-text-muted/40',
};

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}

function StepNodeComponent({ data }: NodeProps) {
  const d = data as StepNodeData;
  const status = d.status ?? 'pending';

  return (
    <div
      className={cn(
        'w-[220px] rounded-xl border bg-surface-elevated/90 backdrop-blur-md',
        'transition-all duration-200',
        statusBorder[status],
        d.selected && 'shadow-glow-primary border-primary/70',
        status === 'running' && 'step-node-running',
        status === 'success' && 'step-node-just-completed',
      )}
    >
      <Handle
        type="target"
        position={Position.Top}
        className="!w-2 !h-2 !bg-border !border-background !border-2 !-top-1"
      />

      <div className="px-3 py-2.5">
        {/* Header: step ID + status dot */}
        <div className="flex items-center justify-between mb-1">
          <span className="font-mono text-xs text-text-primary truncate pr-2">
            {d.stepId}
          </span>
          <div className={cn('w-2 h-2 rounded-full flex-shrink-0', statusDot[status])} />
        </div>

        {/* Tool name */}
        <div className="font-mono text-xs text-text-muted truncate mb-2">
          {d.tool}
        </div>

        {/* Badges row */}
        {(d.hasCondition || d.hasRetry || d.onError || d.isSkillCall) && (
          <div className="flex items-center gap-1.5 mb-1.5">
            {d.hasCondition && (
              <span title="Has condition" className="p-0.5 rounded bg-secondary/10">
                <Filter size={10} className="text-secondary" strokeWidth={2} />
              </span>
            )}
            {d.hasRetry && (
              <span title="Has retry policy" className="p-0.5 rounded bg-primary/10">
                <RefreshCw size={10} className="text-primary" strokeWidth={2} />
              </span>
            )}
            {d.onError === 'skip' && (
              <span title="on_error: skip" className="p-0.5 rounded bg-status-pending/10">
                <SkipForward size={10} className="text-status-pending" strokeWidth={2} />
              </span>
            )}
            {d.onError === 'continue' && (
              <span title="on_error: continue" className="p-0.5 rounded bg-status-running/10">
                <Play size={10} className="text-status-running" strokeWidth={2} />
              </span>
            )}
            {d.isSkillCall && (
              <span title={`Calls skill: ${d.tool.replace('registry__', '')}`} className="p-0.5 rounded bg-tertiary/10">
                <Layers size={10} className="text-tertiary" strokeWidth={2} />
              </span>
            )}
          </div>
        )}

        {/* Duration (after execution) */}
        {d.durationMs != null && status !== 'pending' && (
          <div className="flex items-center gap-1 text-[10px] text-text-muted">
            <Clock size={9} strokeWidth={2} />
            <span className="font-mono">{formatDuration(d.durationMs)}</span>
          </div>
        )}
      </div>

      <Handle
        type="source"
        position={Position.Bottom}
        className="!w-2 !h-2 !bg-border !border-background !border-2 !-bottom-1"
      />
    </div>
  );
}

// Custom comparator: only re-render when status, selection, or data changes
export const StepNode = memo(StepNodeComponent, (prev, next) => {
  const pd = prev.data as StepNodeData;
  const nd = next.data as StepNodeData;
  return (
    pd.status === nd.status &&
    pd.selected === nd.selected &&
    pd.durationMs === nd.durationMs &&
    pd.error === nd.error &&
    pd.stepId === nd.stepId &&
    pd.tool === nd.tool
  );
});
