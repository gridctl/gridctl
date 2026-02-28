import { memo } from 'react';
import { Handle, Position } from '@xyflow/react';
import { Library, Wrench } from 'lucide-react';
import { cn } from '../../lib/cn';
import type { RegistryNodeData } from '../../types';

interface RegistryNodeProps {
  data: RegistryNodeData;
  selected?: boolean;
}

const RegistryNode = memo(({ data, selected }: RegistryNodeProps) => {
  return (
    <div
      className={cn(
        'w-[200px] rounded-xl relative',
        'backdrop-blur-xl border transition-all duration-300 ease-out',
        'bg-gradient-to-br from-surface/95 to-primary/[0.03] border-primary/30',
        selected && 'border-primary shadow-glow-primary ring-1 ring-primary/30',
        !selected && 'hover:shadow-node-hover hover:border-primary/50'
      )}
    >
      {/* Top accent gradient */}
      <div className="absolute top-0 left-0 right-0 h-px bg-gradient-to-r from-transparent via-primary/40 to-transparent" />

      {/* Header */}
      <div className="px-3 py-2.5 flex items-center justify-between border-b border-primary/10 bg-primary/[0.03]">
        <div className="flex items-center gap-2.5 min-w-0">
          <div className="p-1.5 rounded-lg bg-primary/10 border border-primary/20">
            <Library size={14} className="text-primary" />
          </div>
          <div className="flex-1 min-w-0">
            <span className="font-semibold text-sm text-text-primary block truncate tracking-tight">
              {data.name}
            </span>
            <span className="text-[9px] text-text-muted">Agent Skills</span>
          </div>
        </div>
      </div>

      {/* Body */}
      <div className="p-3 space-y-2">
        {/* Counts */}
        <div className="flex items-center gap-3 text-[11px] text-text-muted font-mono">
          <div className="flex items-center gap-1.5">
            <Wrench size={10} className="text-primary/70" />
            <span>
              <span className="text-status-running">{data.activeSkills ?? 0}</span>
              <span className="text-text-muted/60">/{data.totalSkills ?? 0}</span>
              <span className="ml-1 text-text-muted/50">skills</span>
            </span>
          </div>
        </div>

        {/* Type badge */}
        <div className="flex items-center gap-1.5">
          <span className="text-[9px] px-1.5 py-0.5 rounded bg-primary/10 text-primary font-mono uppercase tracking-wider">
            Internal
          </span>
          {(data.activeSkills ?? 0) > 0 && (
            <span className="text-[9px] px-1.5 py-0.5 rounded bg-status-running/10 text-status-running font-mono">
              executable
            </span>
          )}
        </div>
      </div>

      {/* Connection Handle */}
      <Handle
        type="target"
        position={Position.Left}
        className={cn(
          '!w-2.5 !h-2.5 !border-2 !border-background !rounded-full !bg-primary',
          'transition-all duration-200 hover:!scale-125'
        )}
        id="input"
      />
    </div>
  );
});

RegistryNode.displayName = 'RegistryNode';

export default RegistryNode;
