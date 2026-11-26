import { HeartPulse } from 'lucide-react';
import { cn } from '../../lib/cn';
import { formatRelativeTime } from '../../lib/time';
import { ServerCrashDetails } from './ServerCrashDetails';
import type { MCPServerNodeData, MCPServerStatus } from '../../types';

type HealthSource = Pick<
  MCPServerNodeData | MCPServerStatus,
  'healthy' | 'lastCheck' | 'healthError' | 'replicas' | 'stderrTail'
>;

// Health, last check, the health error, and crash details for one MCP server.
// Crash details render whether or not a health check has reported yet.
export function ServerHealth({ data, labelClassName = 'text-sm' }: { data: HealthSource; labelClassName?: string }) {
  const checked = data.healthy !== undefined && data.healthy !== null;
  return (
    <>
      {checked && (
        <>
          <div className="flex justify-between items-center">
            <span className={cn(labelClassName, 'text-text-muted')}>Health</span>
            <span className={cn(
              'text-xs px-2 py-0.5 rounded-md font-medium flex items-center gap-1',
              data.healthy
                ? 'bg-status-running/10 text-status-running'
                : 'bg-status-error/10 text-status-error'
            )}>
              <HeartPulse size={10} />
              {data.healthy ? 'Healthy' : 'Unhealthy'}
            </span>
          </div>

          {data.lastCheck && (
            <div className="flex justify-between items-center">
              <span className={cn(labelClassName, 'text-text-muted')}>Last Check</span>
              <span className="text-xs text-text-secondary font-mono">
                {formatRelativeTime(new Date(data.lastCheck))}
              </span>
            </div>
          )}

          {!data.healthy && data.healthError && (
            <div className="mt-1 p-2 rounded-md bg-status-error/5 border border-status-error/15">
              <span className="text-xs text-status-error font-mono break-words">
                {data.healthError}
              </span>
            </div>
          )}
        </>
      )}
      <ServerCrashDetails data={data} />
    </>
  );
}
