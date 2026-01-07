import { memo } from 'react';
import { Handle, Position } from '@xyflow/react';
import { Activity, Server, Wrench } from 'lucide-react';
import { cn } from '../../lib/cn';
import { StatusDot } from '../ui/StatusDot';
import type { GatewayNodeData } from '../../types';

interface GatewayNodeProps {
  data: GatewayNodeData;
  selected?: boolean;
}

const GatewayNode = memo(({ data, selected }: GatewayNodeProps) => {
  return (
    <div
      className={cn(
        'w-56 node-glass-cyan shadow-node overflow-hidden node-rim-light',
        'transition-all duration-200 ease-out',
        'shadow-glow-primary',
        selected && 'node-active-glow ring-2 ring-primary/50 ring-offset-2 ring-offset-background'
      )}
    >
      {/* Header */}
      <div className="bg-primary/10 px-4 py-3 flex items-center gap-3 border-b border-primary/20">
        <div className="p-2 bg-primary/20 rounded-lg">
          <Activity size={20} className="text-primary drop-shadow-[0_0_6px_rgba(0,202,255,0.6)]" />
        </div>
        <div>
          <h3 className="font-bold text-sm text-text-primary">{data.name}</h3>
          <p className="text-[10px] text-text-muted font-mono">v{data.version}</p>
        </div>
      </div>

      {/* Stats */}
      <div className="p-3 space-y-2">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2 text-xs text-text-muted">
            <Server size={12} />
            <span>MCP Servers</span>
          </div>
          <span className="text-sm font-semibold text-text-primary">
            {data.serverCount}
          </span>
        </div>

        {data.resourceCount > 0 && (
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2 text-xs text-text-muted">
              <Server size={12} className="text-secondary" />
              <span>Resources</span>
            </div>
            <span className="text-sm font-semibold text-text-primary">
              {data.resourceCount}
            </span>
          </div>
        )}

        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2 text-xs text-text-muted">
            <Wrench size={12} />
            <span>Total Tools</span>
          </div>
          <span className="text-sm font-semibold text-text-primary">
            {data.totalToolCount}
          </span>
        </div>

        {/* Status indicator */}
        <div className="flex items-center gap-2 pt-2 border-t border-white/10">
          <StatusDot status="running" />
          <span className="text-xs text-status-running font-medium neon-text">Gateway Active</span>
        </div>
      </div>

      {/* Connection Handles - Neon style */}
      <Handle
        type="target"
        position={Position.Left}
        className={cn(
          '!w-4 !h-4 !bg-primary !border-2 !border-background',
          '!shadow-[0_0_8px_rgba(0,202,255,0.5)]',
          'transition-all duration-150'
        )}
        id="input"
      />
      <Handle
        type="source"
        position={Position.Right}
        className={cn(
          '!w-4 !h-4 !bg-primary !border-2 !border-background',
          '!shadow-[0_0_8px_rgba(0,202,255,0.5)]',
          'transition-all duration-150'
        )}
        id="output"
      />
    </div>
  );
});

GatewayNode.displayName = 'GatewayNode';

export default GatewayNode;
