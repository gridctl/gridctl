import { memo } from 'react';
import { Handle, Position } from '@xyflow/react';
import { Bot, Globe, Server, Hash, Zap } from 'lucide-react';
import { cn } from '../../lib/cn';
import { StatusDot } from '../ui/StatusDot';
import type { AgentNodeData } from '../../types';

interface AgentNodeProps {
  data: AgentNodeData;
  selected?: boolean;
}

const AgentNode = memo(({ data, selected }: AgentNodeProps) => {
  const isRemote = data.variant === 'remote';
  const hasA2A = data.hasA2A;

  // Determine primary color based on variant and A2A capability
  // Local without A2A: purple (tertiary)
  // Local with A2A: purple base + teal accent ("Cartridge" pattern)
  // Remote (always has A2A): teal (secondary)
  const primaryColor = isRemote ? 'secondary' : 'tertiary';

  return (
    <div
      className={cn(
        'w-40 h-36 rounded-lg',
        'backdrop-blur-xl border-2 transition-all duration-300 ease-out',
        // Gradient background - purple for local, teal tint for remote
        isRemote
          ? 'bg-gradient-to-br from-surface/95 to-secondary/[0.08]'
          : hasA2A
            ? 'bg-gradient-to-br from-surface/95 via-tertiary/[0.03] to-secondary/[0.06]'
            : 'bg-gradient-to-br from-surface/95 to-tertiary/[0.05]',
        'flex flex-col items-center justify-center text-center',
        // Border and selection styling
        selected && `border-${primaryColor} shadow-glow-${primaryColor} ring-2 ring-${primaryColor}/30`,
        !selected && `border-${primaryColor}/30 hover:shadow-node-hover hover:border-${primaryColor}/50`
      )}
      style={{
        borderColor: selected
          ? `var(--color-${primaryColor})`
          : undefined,
      }}
    >
      {/* Pulse ring for running agents */}
      {data.status === 'running' && (
        <div
          className={cn(
            'absolute inset-0 rounded-lg border-2 animate-ping',
            isRemote ? 'border-secondary/30' : 'border-tertiary/30'
          )}
          style={{ animationDuration: '2.5s' }}
        />
      )}

      {/* A2A capability badge (top-right corner) */}
      {hasA2A && (
        <div className="absolute -top-2 -right-2 px-2 py-0.5 rounded-full bg-surface border border-secondary/30 flex items-center gap-1">
          {/* Teal overlay for glass effect */}
          <div className="absolute inset-0 rounded-full bg-secondary/20" />
          <Zap size={8} className="text-secondary relative" />
          <span className="text-[9px] text-secondary font-medium relative">
            A2A
          </span>
        </div>
      )}

      {/* Variant badge (top-left corner) */}
      <div className={cn(
        'absolute -top-2 -left-2 px-1.5 py-0.5 rounded-full border bg-surface',
        isRemote
          ? 'border-secondary/30'
          : 'border-tertiary/30'
      )}>
        {/* Color overlay for glass effect */}
        <div className={cn(
          'absolute inset-0 rounded-full',
          isRemote ? 'bg-secondary/10' : 'bg-tertiary/10'
        )} />
        {isRemote ? (
          <Globe size={10} className="text-secondary relative" />
        ) : (
          <Server size={10} className="text-tertiary relative" />
        )}
      </div>

      {/* Icon - purple base, teal accent if A2A */}
      <div className={cn(
        'p-2.5 rounded-lg border mb-1 relative',
        isRemote
          ? 'bg-secondary/10 border-secondary/30'
          : 'bg-tertiary/10 border-tertiary/30'
      )}>
        <Bot size={22} className={isRemote ? 'text-secondary' : 'text-tertiary'} />
        {/* A2A indicator on icon */}
        {hasA2A && !isRemote && (
          <div className="absolute -bottom-1 -right-1 p-0.5 rounded-full bg-surface border border-secondary/40">
            <div className="absolute inset-0 rounded-full bg-secondary/20" />
            <Zap size={8} className="text-secondary relative" />
          </div>
        )}
      </div>

      {/* Name */}
      <span className="font-semibold text-xs text-text-primary truncate max-w-[130px] px-1">
        {data.name}
      </span>

      {/* Skills count (if A2A enabled) */}
      {hasA2A && (data.skillCount ?? 0) > 0 && (
        <span className="text-[10px] text-secondary mt-0.5">
          {data.skillCount} skill{data.skillCount !== 1 ? 's' : ''}
        </span>
      )}

      {/* Status */}
      <div className="flex items-center gap-1 mt-1">
        <StatusDot status={data.status} size="sm" />
        <span className="text-[10px] text-text-muted capitalize">
          {data.status}
        </span>
      </div>

      {/* Container ID hint (only for local agents) */}
      {!isRemote && data.containerId && (
        <div className="flex items-center gap-0.5 mt-0.5 opacity-60">
          <Hash size={8} className="text-tertiary" />
          <span className="text-[8px] text-text-muted font-mono">
            {data.containerId.slice(0, 8)}
          </span>
        </div>
      )}

      {/* Connection Handles */}
      <Handle
        type="target"
        position={Position.Left}
        className={cn(
          '!w-2.5 !h-2.5 !border-2 !border-background !rounded-full',
          isRemote ? '!bg-secondary' : '!bg-tertiary',
          'transition-all duration-200 hover:!scale-125'
        )}
        id="input"
      />
      <Handle
        type="source"
        position={Position.Right}
        className={cn(
          '!w-2.5 !h-2.5 !border-2 !border-background !rounded-full',
          hasA2A ? '!bg-secondary' : '!bg-tertiary',
          'transition-all duration-200 hover:!scale-125'
        )}
        id="output"
      />
    </div>
  );
});

AgentNode.displayName = 'AgentNode';

export default AgentNode;
