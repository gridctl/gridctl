import { memo } from 'react';
import { Handle, Position } from '@xyflow/react';
import { Wrench } from 'lucide-react';
import { cn } from '../../lib/cn';
import { LAYOUT } from '../../lib/constants';
import type { ToolNodeData } from '../../types';

interface ToolNodeProps {
  data: ToolNodeData;
}

/**
 * A single tool fanned out from an expanded MCP server. Renders as a compact
 * neutral pill that matches the linked-client theme (surface gradient, neutral
 * border, monochrome accents) rather than the violet server theme, so "tools"
 * read as a distinct layer from the MCP servers they belong to. Slides in from
 * the left when mounted. Not selectable - it is a read-only affordance for PR 2.
 */
const ToolNode = memo(({ data }: ToolNodeProps) => {
  return (
    <div
      className={cn(
        'animate-slide-in-right',
        'flex items-center gap-2 px-2.5 rounded-lg relative',
        'border border-border bg-gradient-to-b from-surface/95 via-surface/90 to-surface/80',
        'backdrop-blur-xl shadow-bevel',
        'transition-colors duration-200 hover:shadow-node-hover hover:border-text-secondary/40'
      )}
      style={{ width: LAYOUT.TOOL_WIDTH, height: LAYOUT.TOOL_HEIGHT }}
      title={`${data.serverName} · ${data.name}`}
    >
      {/* Top accent line, matching the client nodes. */}
      <div className="absolute top-0 left-0 right-0 h-px bg-gradient-to-r from-transparent via-white/20 to-transparent" />

      <Wrench size={11} className="text-text-secondary flex-shrink-0" />
      {/* min-w-0 lets the flex item shrink; tool-label re-asserts
          overflow:hidden (see index.css) so truncate clips long tool names
          instead of overflowing the pill. */}
      <span className="tool-label min-w-0 flex-1 font-mono text-[11px] text-text-secondary truncate tracking-tight">
        {data.name}
      </span>

      <Handle
        type="target"
        position={Position.Left}
        className={cn(
          '!w-2 !h-2 !bg-text-secondary !border-2 !border-background !rounded-full',
          'transition-all duration-200'
        )}
        id="input"
      />
    </div>
  );
});

ToolNode.displayName = 'ToolNode';

export default ToolNode;
