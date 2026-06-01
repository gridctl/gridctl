import { memo, useCallback, useState } from 'react';
import { Handle, Position } from '@xyflow/react';
import { Wrench } from 'lucide-react';
import { cn } from '../../lib/cn';
import { LAYOUT } from '../../lib/constants';
import { useDismiss } from '../../hooks/useDismiss';
import ToolDetailPopover from './ToolDetailPopover';
import type { ToolNodeData } from '../../types';

interface ToolNodeProps {
  data: ToolNodeData;
}

/**
 * A single tool fanned out from an expanded MCP server. Renders as a compact
 * neutral pill that matches the linked-client theme (surface gradient, neutral
 * border, monochrome accents) rather than the violet server theme, so "tools"
 * read as a distinct layer from the MCP servers they belong to. Slides in from
 * the left when mounted.
 *
 * Clicking (or keyboard-activating) the pill opens a canvas-anchored detail
 * popover with the tool's description, input schema, and quick actions. The
 * pill is `nodrag` so a click is a clean activation rather than a drag start,
 * matching the "+N more" overflow node.
 */
const ToolNode = memo(({ data }: ToolNodeProps) => {
  const [open, setOpen] = useState(false);
  const close = useCallback(() => setOpen(false), []);
  const wrapperRef = useDismiss<HTMLDivElement>(open, close);

  return (
    <div
      ref={wrapperRef}
      className="nodrag animate-slide-in-right relative"
      style={{ width: LAYOUT.TOOL_WIDTH }}
    >
      <button
        type="button"
        onClick={(e) => {
          // Keep the click off the canvas node/pane handlers; the popover is
          // self-contained, so the node must never select or open the sidebar.
          e.stopPropagation();
          setOpen((v) => !v);
        }}
        aria-expanded={open}
        aria-label={`Show details for ${data.serverName} tool ${data.name}`}
        title={`${data.serverName} · ${data.name}`}
        className={cn(
          'w-full flex items-center gap-2 px-2.5 rounded-lg relative text-left',
          'border border-border bg-gradient-to-b from-surface/95 via-surface/90 to-surface/80',
          'backdrop-blur-xl shadow-bevel',
          'transition-colors duration-200 hover:shadow-node-hover hover:border-text-secondary/40',
        )}
        style={{ height: LAYOUT.TOOL_HEIGHT }}
      >
        {/* Top accent line, matching the client nodes. */}
        <div className="absolute top-0 left-0 right-0 h-px bg-gradient-to-r from-transparent via-white/20 to-transparent" />

        <Wrench size={11} className="text-text-secondary flex-shrink-0" aria-hidden="true" />
        {/* min-w-0 lets the flex item shrink; tool-label re-asserts
            overflow:hidden (see index.css) so truncate clips long tool names
            instead of overflowing the pill. */}
        <span className="tool-label min-w-0 flex-1 font-mono text-[11px] text-text-secondary truncate tracking-tight">
          {data.name}
        </span>
      </button>

      <Handle
        type="target"
        position={Position.Left}
        className={cn(
          '!w-2 !h-2 !bg-text-secondary !border-2 !border-background !rounded-full',
          'transition-all duration-200',
        )}
        id="input"
      />

      {open && (
        <ToolDetailPopover
          serverName={data.serverName}
          toolName={data.name}
          onClose={close}
        />
      )}
    </div>
  );
});

ToolNode.displayName = 'ToolNode';

export default ToolNode;
