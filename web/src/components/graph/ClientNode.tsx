import { memo } from 'react';
import { Handle, Position } from '@xyflow/react';
import { Monitor } from 'lucide-react';
import { cn } from '../../lib/cn';
import { StatusDot } from '../ui/StatusDot';
import type { ClientNodeData } from '../../types';

interface ClientNodeProps {
  data: ClientNodeData;
  selected?: boolean;
}

const ClientNode = memo(({ data, selected }: ClientNodeProps) => {
  return (
    <div
      className={cn(
        'w-40 rounded-xl relative',
        'backdrop-blur-xl border transition-all duration-300 ease-out',
        'bg-gradient-to-b from-surface/95 via-surface/90 to-primary/[0.03]',
        'flex flex-col items-center justify-center text-center p-3 gap-1',
        selected && 'border-primary shadow-glow-primary ring-2 ring-primary/20',
        !selected && 'border-primary/25 hover:shadow-node-hover hover:border-primary/40'
      )}
      style={{ height: 120 }}
    >
      {/* Top accent */}
      <div className="absolute top-0 left-0 right-0 h-px bg-gradient-to-r from-transparent via-primary/40 to-transparent" />

      {/* Icon */}
      <div className="p-2 rounded-lg border bg-primary/10 border-primary/25">
        <Monitor size={20} className="text-primary" />
      </div>

      {/* Name */}
      <span className="font-semibold text-xs text-text-primary truncate max-w-[130px] px-1">
        {data.name}
      </span>

      {/* Transport badge */}
      <span className="text-[9px] text-text-muted font-mono uppercase tracking-wider">
        {data.transport}
      </span>

      {/* Status */}
      <div className="flex items-center gap-1">
        <StatusDot status={data.status} />
        <span className="text-[10px] text-status-running font-medium">Linked</span>
      </div>

      {/* Source handle (connects to gateway on the right) */}
      <Handle
        type="source"
        position={Position.Right}
        className={cn(
          '!w-2.5 !h-2.5 !bg-primary !border-2 !border-background !rounded-full',
          'transition-all duration-200 hover:!scale-125 hover:!shadow-glow-primary'
        )}
        id="output"
      />
    </div>
  );
});

ClientNode.displayName = 'ClientNode';

export default ClientNode;
