import { memo } from 'react';
import { Handle, Position } from '@xyflow/react';
import { Bot, Hash } from 'lucide-react';
import { cn } from '../../lib/cn';
import { StatusDot } from '../ui/StatusDot';
import type { AgentNodeData } from '../../types';

interface AgentNodeProps {
  data: AgentNodeData;
  selected?: boolean;
}

const AgentNode = memo(({ data, selected }: AgentNodeProps) => {
  return (
    <div
      className={cn(
        'w-32 h-32 rounded-full',
        'backdrop-blur-xl border-2 transition-all duration-300 ease-out',
        'bg-gradient-to-br from-surface/95 to-tertiary/[0.05]',
        'flex flex-col items-center justify-center text-center',
        selected && 'border-tertiary shadow-glow-tertiary ring-2 ring-tertiary/30',
        !selected && 'border-tertiary/30 hover:shadow-node-hover hover:border-tertiary/50'
      )}
    >
      {/* Pulse ring for running agents */}
      {data.status === 'running' && (
        <div
          className="absolute inset-0 rounded-full border-2 border-tertiary/30 animate-ping"
          style={{ animationDuration: '2s' }}
        />
      )}

      {/* Icon */}
      <div className={cn(
        'p-3 rounded-full border mb-1',
        'bg-tertiary/10 border-tertiary/30'
      )}>
        <Bot size={24} className="text-tertiary" />
      </div>

      {/* Name */}
      <span className="font-semibold text-xs text-text-primary truncate max-w-[90px] px-1">
        {data.name}
      </span>

      {/* Status */}
      <div className="flex items-center gap-1 mt-1">
        <StatusDot status={data.status} size="sm" />
        <span className="text-[10px] text-text-muted capitalize">
          {data.status}
        </span>
      </div>

      {/* Container ID hint */}
      {data.containerId && (
        <div className="flex items-center gap-0.5 mt-1 opacity-60">
          <Hash size={8} className="text-tertiary" />
          <span className="text-[8px] text-text-muted font-mono">
            {data.containerId.slice(0, 8)}
          </span>
        </div>
      )}

      {/* Connection Handles - flow: gateway (left) → agent (right) */}
      <Handle
        type="target"
        position={Position.Left}
        className={cn(
          '!w-2.5 !h-2.5 !border-2 !border-background !rounded-full',
          '!bg-tertiary',
          'transition-all duration-200 hover:!scale-125'
        )}
        id="input"
      />
      <Handle
        type="source"
        position={Position.Right}
        className={cn(
          '!w-2.5 !h-2.5 !border-2 !border-background !rounded-full',
          '!bg-tertiary',
          'transition-all duration-200 hover:!scale-125'
        )}
        id="output"
      />
    </div>
  );
});

AgentNode.displayName = 'AgentNode';

export default AgentNode;
