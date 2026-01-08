import { memo } from 'react';
import { Handle, Position } from '@xyflow/react';
import { Users, Globe, Server } from 'lucide-react';
import { cn } from '../../lib/cn';
import { StatusDot } from '../ui/StatusDot';
import type { A2AAgentNodeData } from '../../types';

interface A2AAgentNodeProps {
  data: A2AAgentNodeData;
  selected?: boolean;
}

const A2AAgentNode = memo(({ data, selected }: A2AAgentNodeProps) => {
  const isRemote = data.role === 'remote';
  const RoleIcon = isRemote ? Globe : Server;

  return (
    <div
      className={cn(
        'w-36 h-36 rounded-lg',
        'backdrop-blur-xl border-2 transition-all duration-300 ease-out',
        'bg-gradient-to-br from-surface/95 to-secondary/[0.08]',
        'flex flex-col items-center justify-center text-center',
        selected && 'border-secondary shadow-glow-secondary ring-2 ring-secondary/30',
        !selected && 'border-secondary/30 hover:shadow-node-hover hover:border-secondary/50'
      )}
    >
      {/* Pulse ring for available agents */}
      {data.status === 'running' && (
        <div
          className="absolute inset-0 rounded-lg border-2 border-secondary/30 animate-ping"
          style={{ animationDuration: '2.5s' }}
        />
      )}

      {/* Role badge */}
      <div className="absolute -top-2 -right-2 px-2 py-0.5 rounded-full bg-secondary/20 border border-secondary/30">
        <span className="text-[9px] text-secondary font-medium uppercase">
          {data.role}
        </span>
      </div>

      {/* Icon */}
      <div className={cn(
        'p-3 rounded-lg border mb-1',
        'bg-secondary/10 border-secondary/30'
      )}>
        <Users size={20} className="text-secondary" />
      </div>

      {/* Name */}
      <span className="font-semibold text-xs text-text-primary truncate max-w-[100px] px-1">
        {data.name}
      </span>

      {/* Skills count */}
      <span className="text-[10px] text-text-muted mt-0.5">
        {data.skillCount} skill{data.skillCount !== 1 ? 's' : ''}
      </span>

      {/* Status */}
      <div className="flex items-center gap-1 mt-1">
        <StatusDot status={data.status} size="sm" />
        <span className="text-[10px] text-text-muted capitalize">
          {data.status === 'running' ? 'available' : data.status}
        </span>
      </div>

      {/* Role indicator icon */}
      <div className="absolute bottom-2 right-2 opacity-40">
        <RoleIcon size={12} className="text-secondary" />
      </div>

      {/* Connection Handles */}
      <Handle
        type="target"
        position={Position.Left}
        className={cn(
          '!w-2.5 !h-2.5 !border-2 !border-background !rounded-full',
          '!bg-secondary',
          'transition-all duration-200 hover:!scale-125'
        )}
        id="input"
      />
      <Handle
        type="source"
        position={Position.Right}
        className={cn(
          '!w-2.5 !h-2.5 !border-2 !border-background !rounded-full',
          '!bg-secondary',
          'transition-all duration-200 hover:!scale-125'
        )}
        id="output"
      />
    </div>
  );
});

A2AAgentNode.displayName = 'A2AAgentNode';

export default A2AAgentNode;
